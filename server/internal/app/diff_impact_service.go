package app

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// DiffImpactRequest is the input for DiffImpactService.Analyze.
type DiffImpactRequest struct {
	// RepoSlug identifies the indexed repository (e.g. "commit0-dev/commit0").
	RepoSlug string
	// RepoPath is the local filesystem path used for git operations.
	RepoPath string
	// FromRef is the base git ref (default: "main").
	FromRef string
	// ToRef is the target git ref. Use "WORKING" for staged+unstaged vs HEAD.
	// Default: "HEAD".
	ToRef string
	// MaxDepth caps the blast radius traversal. Default: 5.
	MaxDepth int
	// EdgeLabels selects which edge types to follow. Default: ["calls"].
	EdgeLabels []string
	// NoExplain skips the optional LLM summary when true.
	NoExplain bool
}

// DiffImpactResult is the output of DiffImpactService.Analyze.
type DiffImpactResult struct {
	// ChangedSymbols are the indexed nodes whose line ranges overlap the diff.
	ChangedSymbols []types.CodeNode
	// Affected contains production (non-test) nodes transitively impacted.
	Affected []types.AffectedNode
	// AffectedTests contains test nodes (*_test.go) transitively impacted.
	AffectedTests []types.AffectedNode
	// Summary is an optional LLM-generated description (empty when NoExplain=true).
	Summary string
	// Timing records wall-clock millis for each phase.
	Timing types.TimingInfo
}

// DiffImpactService analyses blast radius across a git diff.
type DiffImpactService struct {
	graph     domain.OpenCodeGraph
	blast     *BlastService
	git       domain.GitWalker
	explainer domain.LLMExplainer
	cfg       *config.Config
	log       *slog.Logger
}

// NewDiffImpactService constructs a DiffImpactService.
func NewDiffImpactService(
	graph domain.OpenCodeGraph,
	blast *BlastService,
	git domain.GitWalker,
	explainer domain.LLMExplainer,
	cfg *config.Config,
) *DiffImpactService {
	return &DiffImpactService{
		graph:     graph,
		blast:     blast,
		git:       git,
		explainer: explainer,
		cfg:       cfg,
		log:       slog.Default().With("service", "diff_impact"),
	}
}

// Analyze runs the full diff-impact pipeline.
func (s *DiffImpactService) Analyze(ctx context.Context, req DiffImpactRequest) (*DiffImpactResult, error) {
	total := time.Now()

	// ── Validation ────────────────────────────────────────────────────────────
	if req.RepoSlug == "" {
		return nil, domain.Validation("repo_slug is required")
	}
	if req.RepoPath == "" {
		return nil, domain.Validation("repo_path is required")
	}

	// Apply defaults.
	if req.MaxDepth <= 0 {
		req.MaxDepth = 5
	}
	if req.MaxDepth > 5 {
		req.MaxDepth = 5
	}
	if len(req.EdgeLabels) == 0 {
		req.EdgeLabels = []string{"calls"}
	}
	if req.FromRef == "" {
		req.FromRef = "main"
	}
	if req.ToRef == "" {
		req.ToRef = "HEAD"
	}

	// ── Step 1: Resolve the diff ──────────────────────────────────────────────
	gitStart := time.Now()
	var diffs []domain.GitFileDiff
	var err error

	if req.ToRef == "WORKING" {
		diffs, err = s.git.DiffWorkingTree(ctx, req.RepoPath)
	} else {
		diffs, err = s.git.DiffRange(ctx, req.RepoPath, req.FromRef, req.ToRef)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve diff: %w", err)
	}
	gitMS := time.Since(gitStart).Milliseconds()

	if len(diffs) == 0 {
		return &DiffImpactResult{
			Timing: types.TimingInfo{
				GraphMS: gitMS,
				TotalMS: time.Since(total).Milliseconds(),
			},
		}, nil
	}

	// ── Step 2: Map files → changed symbols ───────────────────────────────────
	graphStart := time.Now()
	changedSymbols, err := s.findChangedSymbols(ctx, req.RepoSlug, diffs)
	if err != nil {
		return nil, fmt.Errorf("find changed symbols: %w", err)
	}
	graphMS := time.Since(graphStart).Milliseconds()

	if len(changedSymbols) == 0 {
		return &DiffImpactResult{
			Timing: types.TimingInfo{
				GraphMS: graphMS + gitMS,
				TotalMS: time.Since(total).Milliseconds(),
			},
		}, nil
	}

	// ── Step 3: Fan-out blast for every changed symbol ────────────────────────
	blastStart := time.Now()
	affected, err := s.fanOutBlast(ctx, req, changedSymbols)
	if err != nil {
		return nil, fmt.Errorf("blast fan-out: %w", err)
	}
	blastMS := time.Since(blastStart).Milliseconds()

	// ── Step 4: Dedupe keeping minimum hop count ──────────────────────────────
	// Build a set of changed symbol IDs for the drop step (Step 5).
	changedIDs := make(map[string]struct{}, len(changedSymbols))
	for _, n := range changedSymbols {
		changedIDs[n.ID] = struct{}{}
	}

	deduped := dedupeAffected(affected, changedIDs)

	// ── Step 6: Split prod vs test ────────────────────────────────────────────
	var prod, tests []types.AffectedNode
	for _, an := range deduped {
		if strings.Contains(an.Node.FilePath, "_test.go") {
			tests = append(tests, an)
		} else {
			prod = append(prod, an)
		}
	}

	// ── Step 7: Sort each list ────────────────────────────────────────────────
	sortAffected(prod)
	sortAffected(tests)

	result := &DiffImpactResult{
		ChangedSymbols: changedSymbols,
		Affected:       prod,
		AffectedTests:  tests,
		Timing: types.TimingInfo{
			GraphMS: graphMS + gitMS + blastMS,
			TotalMS: time.Since(total).Milliseconds(),
		},
	}

	// ── Step 8: Optional LLM summary ─────────────────────────────────────────
	if !req.NoExplain && s.explainer != nil {
		explainStart := time.Now()
		result.Summary = s.buildSummary(ctx, diffs, changedSymbols, prod)
		result.Timing.ExplainMS = time.Since(explainStart).Milliseconds()
	}

	result.Timing.TotalMS = time.Since(total).Milliseconds()
	return result, nil
}

// findChangedSymbols maps diffs to indexed graph nodes by overlapping line ranges.
func (s *DiffImpactService) findChangedSymbols(
	ctx context.Context,
	repoSlug string,
	diffs []domain.GitFileDiff,
) ([]types.CodeNode, error) {
	var symbols []types.CodeNode

	for _, diff := range diffs {
		lookupPath := diff.Path
		if diff.Status == "deleted" && diff.OldPath != "" {
			lookupPath = diff.OldPath
		} else if diff.OldPath != "" && diff.Status == "renamed" {
			// For renames, look up the new path (it should be indexed under the new name).
			lookupPath = diff.Path
		}

		nodes, err := s.graph.ListNodes(ctx, repoSlug, domain.ListOpts{
			FilePath: lookupPath,
		})
		if err != nil {
			s.log.Warn("listNodes failed", "file", lookupPath, "err", err)
			continue
		}

		if len(nodes) == 0 {
			continue
		}

		// Parse hunk line ranges from the patch.
		var ranges []LineRange
		if diff.Patch != "" {
			ranges = parseHunkRanges(diff.Patch)
		}

		for _, node := range nodes {
			if len(ranges) == 0 {
				// No patch text (e.g. binary, added with no content) — include all nodes.
				symbols = append(symbols, node)
				continue
			}
			if nodeOverlapsRanges(node, ranges) {
				symbols = append(symbols, node)
			}
		}
	}

	return symbols, nil
}

// fanOutBlast fans out BlastService.Blast across all changed symbols with bounded concurrency.
func (s *DiffImpactService) fanOutBlast(
	ctx context.Context,
	req DiffImpactRequest,
	symbols []types.CodeNode,
) ([]types.AffectedNode, error) {
	const maxConcurrency = 8

	var mu sync.Mutex
	var allAffected []types.AffectedNode

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(maxConcurrency)

	for _, sym := range symbols {
		sym := sym // capture loop var
		eg.Go(func() error {
			result, err := s.blast.Blast(egCtx, BlastRequest{
				Symbol:     sym.Qualified,
				RepoSlug:   req.RepoSlug,
				MaxDepth:   req.MaxDepth,
				NoExplain:  true,
				EdgeLabels: req.EdgeLabels,
			})
			if err != nil {
				// Non-fatal: log and continue (symbol may have been renamed/removed).
				s.log.Debug("blast failed for symbol", "symbol", sym.Qualified, "err", err)
				return nil
			}
			mu.Lock()
			allAffected = append(allAffected, result.Affected...)
			mu.Unlock()
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return allAffected, nil
}

// dedupeAffected deduplicates by node ID keeping the minimum HopCount, and
// drops any node that is itself a changed symbol (hop=0 self-references).
func dedupeAffected(affected []types.AffectedNode, changedIDs map[string]struct{}) []types.AffectedNode {
	best := make(map[string]types.AffectedNode, len(affected))
	for _, an := range affected {
		if _, isChanged := changedIDs[an.Node.ID]; isChanged {
			continue // drop the changed symbols themselves
		}
		if existing, ok := best[an.Node.ID]; !ok || an.HopCount < existing.HopCount {
			best[an.Node.ID] = an
		}
	}

	deduped := make([]types.AffectedNode, 0, len(best))
	for _, an := range best {
		deduped = append(deduped, an)
	}
	return deduped
}

// sortAffected sorts by hop count ascending, then qualified name.
func sortAffected(nodes []types.AffectedNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].HopCount != nodes[j].HopCount {
			return nodes[i].HopCount < nodes[j].HopCount
		}
		return nodes[i].Node.Qualified < nodes[j].Node.Qualified
	})
}

// buildSummary constructs an LLM prompt and calls Explain for a high-level summary.
func (s *DiffImpactService) buildSummary(
	ctx context.Context,
	diffs []domain.GitFileDiff,
	changed []types.CodeNode,
	affected []types.AffectedNode,
) string {
	// Build a concise context string.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Changed files (%d):\n", len(diffs)))
	for _, d := range diffs {
		fmt.Fprintf(&sb, "  %s (%s, +%d/-%d)\n", d.Path, d.Status, d.Additions, d.Deletions)
	}
	sb.WriteString(fmt.Sprintf("\nChanged symbols (%d):\n", len(changed)))
	for _, n := range changed {
		fmt.Fprintf(&sb, "  %s (%s)\n", n.Qualified, n.Kind)
	}
	top := affected
	if len(top) > 10 {
		top = top[:10]
	}
	fmt.Fprintf(&sb, "\nTop %d affected nodes (prod):\n", len(top))
	for _, an := range top {
		fmt.Fprintf(&sb, "  %s (hop %d)\n", an.Node.Qualified, an.HopCount)
	}

	chunks, err := s.explainer.Explain(ctx, domain.ExplainRequest{
		QueryType:    "blast",
		UserQuery:    "Summarize the blast radius of this diff in 3-5 sentences.",
		GraphContext: sb.String(),
	})
	if err != nil {
		s.log.Debug("LLM summary failed (non-fatal)", "err", err)
		return ""
	}
	var buf []byte
	for chunk := range chunks {
		if chunk.Error != nil {
			s.log.Debug("LLM summary chunk error (non-fatal)", "err", chunk.Error)
			break
		}
		buf = append(buf, []byte(chunk.Text)...)
		if chunk.Done {
			break
		}
	}
	return strings.TrimSpace(string(buf))
}

// ---------------------------------------------------------------------------
// Hunk range parsing
// ---------------------------------------------------------------------------

// LineRange is an inclusive new-file line range from a diff hunk header.
type LineRange struct {
	Start int
	End   int
}

// hunkHeaderRegexp matches unified diff hunk headers.
// Format: @@ -oldStart[,oldCount] +newStart[,newCount] @@ ...
// The new-file range is captured in group 1 (start) and optional group 2 (count).
var hunkHeaderRegexp = regexp.MustCompile(`@@\s+-\d+(?:,\d+)?\s+\+(\d+)(?:,(\d+))?\s+@@`)

// parseHunkRanges extracts the new-file line ranges from unified diff hunk headers.
// A missing count is treated as 1 (single-line change).
func parseHunkRanges(patch string) []LineRange {
	matches := hunkHeaderRegexp.FindAllStringSubmatch(patch, -1)
	if len(matches) == 0 {
		return nil
	}
	ranges := make([]LineRange, 0, len(matches))
	for _, m := range matches {
		start, _ := strconv.Atoi(m[1])
		count := 1
		if m[2] != "" {
			count, _ = strconv.Atoi(m[2])
		}
		if count == 0 {
			// A hunk with count=0 means lines were only deleted; no new-file range.
			continue
		}
		ranges = append(ranges, LineRange{
			Start: start,
			End:   start + count - 1,
		})
	}
	return ranges
}

// nodeOverlapsRanges returns true if the node's [StartLine, EndLine] overlaps
// any of the provided line ranges.
func nodeOverlapsRanges(node types.CodeNode, ranges []LineRange) bool {
	if node.StartLine == 0 && node.EndLine == 0 {
		// Node has no line information — include conservatively.
		return true
	}
	nodeEnd := node.EndLine
	if nodeEnd == 0 {
		nodeEnd = node.StartLine
	}
	for _, r := range ranges {
		// Overlap when: node.Start <= r.End AND nodeEnd >= r.Start.
		if node.StartLine <= r.End && nodeEnd >= r.Start {
			return true
		}
	}
	return false
}
