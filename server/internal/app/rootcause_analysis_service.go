package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// RootCauseRequest configures a commit zero detection run.
type RootCauseRequest struct {
	Description string // bug description or symptom
	TestName    string // optional: specific failing test
	RepoSlug    string
	RepoPath    string
	Since       string // optional: time constraint ("3 days ago", commit hash)
}

// RootCauseAnalysisService orchestrates the 6-step commit zero detection algorithm:
//
//  1. LOCATE  — find bug-related functions via semantic search
//  2. TRACE   — follow field-level data flow backward to find mutations
//  3. TIMELINE — query temporal graph for when mutations were introduced
//  4. CORRELATE — score candidate commits
//  5. VERIFY  — LLM analyzes suspect commit's diff
//  6. REPORT  — assemble RootCauseReport
type RootCauseAnalysisService struct {
	querySvc  *QueryService
	flowSvc   *FieldFlowService
	tempSvc   *TemporalService
	graph     domain.OpenCodeGraph
	gitWalker domain.GitWalker
	explainer domain.LLMExplainer
	cfg       *config.Config
	log       *slog.Logger
}

// NewRootCauseAnalysisService creates the commit zero detection service.
func NewRootCauseAnalysisService(
	querySvc *QueryService,
	flowSvc *FieldFlowService,
	tempSvc *TemporalService,
	graph domain.OpenCodeGraph,
	gitWalker domain.GitWalker,
	explainer domain.LLMExplainer,
	cfg *config.Config,
) *RootCauseAnalysisService {
	return &RootCauseAnalysisService{
		querySvc:  querySvc,
		flowSvc:   flowSvc,
		tempSvc:   tempSvc,
		graph:     graph,
		gitWalker: gitWalker,
		explainer: explainer,
		cfg:       cfg,
		log:       slog.Default().With("service", "rootcause"),
	}
}

// FindRootCause executes the 6-step algorithm to find commit zero.
func (s *RootCauseAnalysisService) FindRootCause(ctx context.Context, req RootCauseRequest) (*types.RootCauseReport, error) {
	startTime := time.Now()
	s.log.Info("starting root cause analysis", "description", req.Description, "repo", req.RepoSlug)

	// ── Step 1: LOCATE — find bug-related functions ──────────────────
	s.log.Info("step 1: LOCATE — searching for related functions")
	queryResult, err := s.querySvc.Query(ctx, QueryRequest{
		Question: req.Description,
		RepoSlug: req.RepoSlug,
		TopK:     10,
	})
	if err != nil {
		return nil, fmt.Errorf("locate: %w", err)
	}
	if len(queryResult.Nodes) == 0 {
		return nil, domain.NotFound("no functions found matching the bug description")
	}
	s.log.Info("located functions", "count", len(queryResult.Nodes))

	// ── Step 2: TRACE — follow data flow backward to find mutations ──
	s.log.Info("step 2: TRACE — following data flow for mutations")
	var allChains []types.FieldFlowChain
	for _, scored := range queryResult.Nodes[:min(5, len(queryResult.Nodes))] {
		if scored.Node.Qualified == "" {
			continue
		}
		flowResult, err := s.flowSvc.TraceFieldFlow(ctx, FieldFlowRequest{
			Symbol:        scored.Node.Qualified,
			RepoSlug:      req.RepoSlug,
			Direction:     "reverse",
			Depth:         8,
			ShowMutations: true,
		})
		if err != nil {
			s.log.Debug("flow trace failed", "symbol", scored.Node.Qualified, "err", err)
			continue
		}
		allChains = append(allChains, flowResult.Chains...)
	}
	s.log.Info("traced data flow", "chains", len(allChains), "mutations", countMutations(allChains))

	// ── Step 3: TIMELINE — find when mutations were introduced ──────
	s.log.Info("step 3: TIMELINE — querying temporal history")
	var suspects []types.SuspectCommit
	seen := make(map[string]bool)

	for _, chain := range allChains {
		for _, hop := range chain.Mutations {
			if hop.Node.IntroducedCommit != "" && !seen[hop.Node.IntroducedCommit] {
				seen[hop.Node.IntroducedCommit] = true
				commitInfo, err := s.gitWalker.CommitInfo(ctx, req.RepoPath, hop.Node.IntroducedCommit)
				if err != nil {
					continue
				}
				suspects = append(suspects, types.SuspectCommit{
					Hash:      commitInfo.Hash,
					Message:   commitInfo.Message,
					Author:    commitInfo.Author,
					Timestamp: commitInfo.Timestamp,
				})
			}
			if hop.Node.LastModifiedCommit != "" && !seen[hop.Node.LastModifiedCommit] {
				seen[hop.Node.LastModifiedCommit] = true
				commitInfo, err := s.gitWalker.CommitInfo(ctx, req.RepoPath, hop.Node.LastModifiedCommit)
				if err != nil {
					continue
				}
				suspects = append(suspects, types.SuspectCommit{
					Hash:      commitInfo.Hash,
					Message:   commitInfo.Message,
					Author:    commitInfo.Author,
					Timestamp: commitInfo.Timestamp,
				})
			}
		}
	}

	// Also check introduced_commit on the top query results themselves
	for _, scored := range queryResult.Nodes[:min(5, len(queryResult.Nodes))] {
		for _, commit := range []string{scored.Node.IntroducedCommit, scored.Node.LastModifiedCommit} {
			if commit != "" && !seen[commit] {
				seen[commit] = true
				commitInfo, err := s.gitWalker.CommitInfo(ctx, req.RepoPath, commit)
				if err != nil {
					continue
				}
				suspects = append(suspects, types.SuspectCommit{
					Hash:      commitInfo.Hash,
					Message:   commitInfo.Message,
					Author:    commitInfo.Author,
					Timestamp: commitInfo.Timestamp,
				})
			}
		}
	}
	s.log.Info("found suspect commits", "count", len(suspects))

	// ── Step 4: CORRELATE — score candidates ────────────────────────
	s.log.Info("step 4: CORRELATE — scoring suspect commits")
	now := time.Now()
	for i := range suspects {
		suspects[i].Score = s.scoreCommit(suspects[i], allChains, now)
	}
	sort.Slice(suspects, func(i, j int) bool {
		return suspects[i].Score > suspects[j].Score
	})

	// ── Step 5: VERIFY — LLM analyzes top suspect ──────────────────
	s.log.Info("step 5: VERIFY — analyzing top suspect commit")
	var explanation, suggestedFix string
	var causalChain []types.FieldFlowHop
	commitZero := ""

	if len(suspects) > 0 {
		top := suspects[0]
		commitZero = top.Hash

		// Get the diff for the top suspect
		diffs, _ := s.gitWalker.DiffCommit(ctx, req.RepoPath, top.Hash)

		// Flatten chains into causal chain
		for _, chain := range allChains {
			causalChain = append(causalChain, chain.Hops...)
		}

		// Ask LLM to verify
		if s.explainer != nil {
			var diffSummary string
			for _, d := range diffs {
				diffSummary += fmt.Sprintf("%s: +%d -%d\n", d.Path, d.Additions, d.Deletions)
			}

			explainReq := domain.ExplainRequest{
				QueryType: "search",
				UserQuery: fmt.Sprintf(
					"Analyze whether commit %s (%q by %s) is the root cause of: %s\n\nCommit diff:\n%s",
					top.Hash[:8], top.Message, top.Author, req.Description, diffSummary,
				),
				CodeContext:    buildExcerptsFromChain(causalChain),
				ResponseSchema: domain.SchemaForQueryType("search"),
			}

			raw, err := s.explainer.ExplainStructured(ctx, explainReq)
			if err == nil {
				var result struct {
					Overview string `json:"overview"`
					Evidence []struct {
						Description string `json:"description"`
					} `json:"evidence"`
					Insights []string `json:"insights"`
				}
				if json.Unmarshal(raw, &result) == nil {
					explanation = result.Overview
					if len(result.Insights) > 0 {
						suggestedFix = result.Insights[0]
					}
				}
			}

			// Update suspect reasoning
			if explanation != "" {
				suspects[0].Reasoning = explanation
			}
		}
	}

	// ── Step 6: REPORT ──────────────────────────────────────────────
	s.log.Info("step 6: REPORT — assembling root cause report", "commit_zero", commitZero)

	report := &types.RootCauseReport{
		CommitHash:     commitZero,
		Explanation:    explanation,
		SuggestedFix:   suggestedFix,
		CausalChain:    causalChain,
		SuspectCommits: suspects,
		Timing: types.TimingInfo{
			TotalMS: time.Since(startTime).Milliseconds(),
		},
	}

	if len(suspects) > 0 {
		report.CommitMessage = suspects[0].Message
		report.Author = suspects[0].Author
		report.Timestamp = suspects[0].Timestamp
		report.Confidence = suspects[0].Score
	}

	return report, nil
}

// scoreCommit computes a composite score for a suspect commit.
// Higher score = more likely to be the root cause.
func (s *RootCauseAnalysisService) scoreCommit(
	suspect types.SuspectCommit,
	chains []types.FieldFlowChain,
	now time.Time,
) float64 {
	score := 0.5 // base score

	// Temporal proximity: more recent commits get higher scores
	// (bugs are usually caused by recent changes)
	daysSince := now.Sub(suspect.Timestamp).Hours() / 24
	if daysSince < 1 {
		score += 0.3
	} else if daysSince < 7 {
		score += 0.2
	} else if daysSince < 30 {
		score += 0.1
	}

	// Data flow position: commits that introduced mutation taint points score higher
	for _, chain := range chains {
		if chain.TaintPoint != nil {
			if chain.TaintPoint.Node.IntroducedCommit == suspect.Hash ||
				chain.TaintPoint.Node.LastModifiedCommit == suspect.Hash {
				score += 0.4 // high — this commit introduced the taint
			}
		}
		for _, hop := range chain.Mutations {
			if hop.Node.LastModifiedCommit == suspect.Hash {
				score += 0.2
			}
		}
	}

	// Cap at 1.0
	return math.Min(score, 1.0)
}

func countMutations(chains []types.FieldFlowChain) int {
	total := 0
	for _, chain := range chains {
		total += len(chain.Mutations)
	}
	return total
}

func buildExcerptsFromChain(hops []types.FieldFlowHop) []domain.CodeExcerpt {
	var excerpts []domain.CodeExcerpt
	seen := make(map[string]bool)
	for _, hop := range hops {
		if seen[hop.Node.Qualified] {
			continue
		}
		seen[hop.Node.Qualified] = true
		excerpts = append(excerpts, domain.CodeExcerpt{
			Qualified: hop.Node.Qualified,
			FilePath:  hop.Node.FilePath,
			Snippet:   hop.Node.Body,
		})
		if len(excerpts) >= 5 {
			break
		}
	}
	return excerpts
}
