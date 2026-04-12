package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// ReviewRequest configures a code review run.
type ReviewRequest struct {
	RepoSlug string
	RepoPath string
	DiffRef  string // git ref to diff against (e.g. "HEAD~1", "main", commit hash)
}

// ReviewIssue is a single issue found during code review.
type ReviewIssue struct {
	Severity    string `json:"severity"`    // "high", "medium", "low"
	Category    string `json:"category"`    // "bug", "security", "style", "missing-test"
	File        string `json:"file"`
	Line        int    `json:"line"`
	Function    string `json:"function"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
}

// ReviewResult is the output of a code review.
type ReviewResult struct {
	Issues       []ReviewIssue `json:"issues"`
	GoodPatterns []string      `json:"good_patterns"`
	BlastRadius  int           `json:"blast_radius"`   // total transitive dependents affected
	MissingTests []string      `json:"missing_tests"`  // functions without test coverage
	Timing       types.TimingInfo
}

// ReviewService analyzes git diffs using the code graph + LLM.
type ReviewService struct {
	blastSvc  *BlastService
	graph     domain.OpenCodeGraph
	gitWalker domain.GitWalker
	explainer domain.LLMExplainer
	log       *slog.Logger
}

// NewReviewService creates a code review service.
func NewReviewService(
	blastSvc *BlastService,
	graph domain.OpenCodeGraph,
	gitWalker domain.GitWalker,
	explainer domain.LLMExplainer,
) *ReviewService {
	return &ReviewService{
		blastSvc:  blastSvc,
		graph:     graph,
		gitWalker: gitWalker,
		explainer: explainer,
		log:       slog.Default().With("service", "review"),
	}
}

// Review analyzes changes since the given ref.
func (s *ReviewService) Review(ctx context.Context, req ReviewRequest) (*ReviewResult, error) {
	startTime := time.Now()

	diffRef := req.DiffRef
	if diffRef == "" {
		diffRef = "HEAD~1"
	}

	// Get changed files
	commits, err := s.gitWalker.ListCommits(ctx, req.RepoPath, diffRef, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("list commits: %w", err)
	}

	// Collect all changed files across commits
	var allDiffs []domain.GitFileDiff
	for _, commit := range commits {
		diffs, err := s.gitWalker.DiffCommit(ctx, req.RepoPath, commit.Hash)
		if err != nil {
			continue
		}
		allDiffs = append(allDiffs, diffs...)
	}

	s.log.Info("reviewing changes", "commits", len(commits), "files", len(allDiffs))

	// Look up changed functions in the graph
	var changedFunctions []types.CodeNode
	totalBlastRadius := 0
	var missingTests []string

	for _, diff := range allDiffs {
		if diff.Status == "deleted" {
			continue
		}
		nodes, err := s.graph.ListNodes(ctx, req.RepoSlug, domain.ListOpts{FilePath: diff.Path})
		if err != nil {
			continue
		}
		changedFunctions = append(changedFunctions, nodes...)

		// Compute blast radius for each changed function
		for _, node := range nodes {
			if node.Kind != types.NodeFunction {
				continue
			}
			blastResult, err := s.blastSvc.Blast(ctx, BlastRequest{
				Symbol:   node.Qualified,
				RepoSlug: req.RepoSlug,
				MaxDepth: 2,
			})
			if err == nil {
				totalBlastRadius += len(blastResult.Affected)
			}

			// Check if function has tests (naive: look for Test* in callers)
			nb, err := s.graph.Neighbors(ctx, node.ID)
			if err == nil && nb != nil {
				hasTest := false
				for _, caller := range nb.Callers {
					if strings.HasPrefix(caller.Qualified, "Test") || strings.Contains(caller.Qualified, "_test.") {
						hasTest = true
						break
					}
				}
				if !hasTest {
					missingTests = append(missingTests, node.Qualified)
				}
			}
		}
	}

	// Build diff summary for LLM review
	var diffSummary strings.Builder
	for _, diff := range allDiffs {
		fmt.Fprintf(&diffSummary, "%s %s (+%d -%d)\n", diff.Status, diff.Path, diff.Additions, diff.Deletions)
		if diff.Patch != "" && len(diff.Patch) < 3000 {
			diffSummary.WriteString(diff.Patch)
			diffSummary.WriteByte('\n')
		}
	}

	// Ask LLM to review
	var issues []ReviewIssue
	var goodPatterns []string

	if s.explainer != nil {
		excerpts := make([]domain.CodeExcerpt, 0, len(changedFunctions))
		for _, fn := range changedFunctions[:min(10, len(changedFunctions))] {
			excerpts = append(excerpts, domain.CodeExcerpt{
				Qualified: fn.Qualified,
				FilePath:  fn.FilePath,
				Snippet:   fn.Body,
			})
		}

		raw, err := s.explainer.ExplainStructured(ctx, domain.ExplainRequest{
			QueryType: "search",
			UserQuery: fmt.Sprintf(
				"Review this code change for bugs, security issues, and missing error handling.\n\nDiff:\n%s\n\nBlast radius: %d affected functions\nMissing tests: %s",
				diffSummary.String(), totalBlastRadius, strings.Join(missingTests, ", "),
			),
			CodeContext:    excerpts,
			ResponseSchema: domain.SchemaForQueryType("search"),
		})
		if err == nil {
			var result struct {
				Overview string   `json:"overview"`
				Evidence []struct {
					Function    string `json:"function"`
					File        string `json:"file"`
					Description string `json:"description"`
					Relevance   string `json:"relevance"`
				} `json:"evidence"`
				Insights []string `json:"insights"`
			}
			if json.Unmarshal(raw, &result) == nil {
				for _, e := range result.Evidence {
					issues = append(issues, ReviewIssue{
						Severity:    "medium",
						Category:    "review",
						File:        e.File,
						Function:    e.Function,
						Description: e.Description,
						Suggestion:  e.Relevance,
					})
				}
				goodPatterns = result.Insights
			}
		}
	}

	return &ReviewResult{
		Issues:       issues,
		GoodPatterns: goodPatterns,
		BlastRadius:  totalBlastRadius,
		MissingTests: missingTests,
		Timing: types.TimingInfo{
			TotalMS: time.Since(startTime).Milliseconds(),
		},
	}, nil
}
