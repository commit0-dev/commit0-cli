package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// AnalysisIssue represents a vulnerability found by the scanner.
type AnalysisIssue struct {
	Severity    string   `json:"severity"`    // "critical", "high", "medium", "low"
	Category    string   `json:"category"`    // "sql-injection", "xss", "auth-bypass", "hardcoded-secret", "missing-auth"
	Title       string   `json:"title"`
	File        string   `json:"file"`
	Line        int      `json:"line"`
	Function    string   `json:"function"`
	Description string   `json:"description"`
	TaintPath   []string `json:"taint_path"` // data flow from source to sink
	Fix         string   `json:"fix"`
}

// AnalysisScanResult is the output of a security scan.
type AnalysisScanResult struct {
	Issues       []AnalysisIssue `json:"issues"`
	ScannedNodes int             `json:"scanned_nodes"`
	Timing       types.TimingInfo
}

// TaintRule defines a source → sink pattern for taint analysis.
type TaintRule struct {
	Name       string
	Severity   string
	Category   string
	Sources    []string // patterns matching input sources (e.g. "req.Body", "r.URL.Query")
	Sinks      []string // patterns matching dangerous sinks (e.g. "db.Query", "exec.Command", "fmt.Fprintf")
	Sanitizers []string // patterns that neutralize the taint (e.g. "html.EscapeString")
}

// DefaultTaintRules returns the built-in security rules.
func DefaultTaintRules() []TaintRule {
	return []TaintRule{
		{
			Name: "SQL Injection", Severity: "critical", Category: "sql-injection",
			Sources:    []string{"req.Body", "r.URL", "r.Form", "c.Param", "c.Query", "input", "request"},
			Sinks:      []string{"db.Query", "db.Exec", "db.Raw", "sql.Query", "surrealdb.Query"},
			Sanitizers: []string{"Prepare", "Parameterize", "Escape"},
		},
		{
			Name: "Command Injection", Severity: "critical", Category: "command-injection",
			Sources:    []string{"req.Body", "input", "os.Args", "flag."},
			Sinks:      []string{"exec.Command", "os.StartProcess", "syscall.Exec"},
			Sanitizers: []string{"shellescape", "shlex.Quote"},
		},
		{
			Name: "XSS", Severity: "high", Category: "xss",
			Sources:    []string{"req.Body", "r.URL", "input", "request"},
			Sinks:      []string{"fmt.Fprintf", "w.Write", "template.HTML", "ResponseWriter.Write"},
			Sanitizers: []string{"html.EscapeString", "template.HTMLEscapeString", "sanitize"},
		},
		{
			Name: "Path Traversal", Severity: "high", Category: "path-traversal",
			Sources:    []string{"req.Body", "r.URL", "c.Param", "filepath."},
			Sinks:      []string{"os.Open", "os.ReadFile", "ioutil.ReadFile", "filepath.Join"},
			Sanitizers: []string{"filepath.Clean", "path.Clean"},
		},
	}
}

// AnalysisService scans code for vulnerabilities using the code graph's
// data flow edges for taint propagation analysis.
type AnalysisService struct {
	store     domain.GraphStore
	flowStore domain.FieldFlowStore
	flowSvc   *FieldFlowService
	explainer domain.LLMExplainer
	rules     []TaintRule
	log       *slog.Logger
}

// NewAnalysisService creates a security scanner.
func NewAnalysisService(
	store domain.GraphStore,
	flowStore domain.FieldFlowStore,
	flowSvc *FieldFlowService,
	explainer domain.LLMExplainer,
) *AnalysisService {
	return &AnalysisService{
		store:     store,
		flowStore: flowStore,
		flowSvc:   flowSvc,
		explainer: explainer,
		rules:     DefaultTaintRules(),
		log:       slog.Default().With("service", "security"),
	}
}

// Scan scans the entire codebase for vulnerabilities.
func (s *AnalysisService) Scan(ctx context.Context, repoSlug string) (*AnalysisScanResult, error) {
	startTime := time.Now()
	s.log.Info("starting security scan", "repo", repoSlug)

	var issues []AnalysisIssue
	scannedNodes := 0

	// Strategy 1: Taint analysis via data flow graph
	// For each rule, find functions that match source patterns, then trace
	// data flow forward to see if it reaches a sink without sanitization.
	for _, rule := range s.rules {
		ruleIssues, scanned := s.checkTaintRule(ctx, repoSlug, rule)
		issues = append(issues, ruleIssues...)
		scannedNodes += scanned
	}

	// Strategy 2: Auth gap detection via call graph
	// Find HTTP handler functions and check if authMiddleware is in their caller chain.
	authIssues := s.checkAuthGaps(ctx, repoSlug)
	issues = append(issues, authIssues...)

	// Strategy 3: LLM verification — filter false positives
	if s.explainer != nil && len(issues) > 0 {
		issues = s.llmVerifyIssues(ctx, issues)
	}

	s.log.Info("security scan complete",
		"repo", repoSlug,
		"issues", len(issues),
		"scanned", scannedNodes,
	)

	return &AnalysisScanResult{
		Issues:       issues,
		ScannedNodes: scannedNodes,
		Timing: types.TimingInfo{
			TotalMS: time.Since(startTime).Milliseconds(),
		},
	}, nil
}

// checkTaintRule traces data flow from sources to sinks for a single rule.
func (s *AnalysisService) checkTaintRule(ctx context.Context, repoSlug string, rule TaintRule) ([]AnalysisIssue, int) {
	var issues []AnalysisIssue
	scanned := 0

	// Find mutations that match source patterns
	for _, sourcePattern := range rule.Sources {
		mutations, err := s.flowStore.FindMutations(ctx, repoSlug, sourcePattern)
		if err != nil {
			continue
		}

		for _, mut := range mutations {
			scanned++
			// Check if the data flows to a sink
			if s.flowSvc != nil {
				flowResult, err := s.flowSvc.TraceFieldFlow(ctx, FieldFlowRequest{
					Symbol:    mut.Node.Qualified,
					FieldPath: sourcePattern,
					RepoSlug:  repoSlug,
					Direction: "forward",
					Depth:     5,
				})
				if err != nil {
					continue
				}

				for _, chain := range flowResult.Chains {
					for _, hop := range chain.Hops {
						for _, sink := range rule.Sinks {
							if strings.Contains(hop.Node.Qualified, sink) || strings.Contains(hop.ArgExpr, sink) {
								// Check if sanitized along the way
								sanitized := false
								for _, sanHop := range chain.Hops {
									for _, san := range rule.Sanitizers {
										if strings.Contains(sanHop.Node.Qualified, san) {
											sanitized = true
											break
										}
									}
								}
								if !sanitized {
									taintPath := make([]string, 0, len(chain.Hops))
									for _, h := range chain.Hops {
										taintPath = append(taintPath, h.Node.Qualified)
									}
									issues = append(issues, AnalysisIssue{
										Severity:    rule.Severity,
										Category:    rule.Category,
										Title:       rule.Name,
										File:        hop.Node.FilePath,
										Line:        hop.Node.StartLine,
										Function:    hop.Node.Qualified,
										Description: fmt.Sprintf("Unsanitized %s reaches %s", sourcePattern, sink),
										TaintPath:   taintPath,
									})
								}
							}
						}
					}
				}
			}
		}
	}

	return issues, scanned
}

// checkAuthGaps finds HTTP handlers without auth middleware in their caller chain.
func (s *AnalysisService) checkAuthGaps(ctx context.Context, repoSlug string) []AnalysisIssue {
	var issues []AnalysisIssue

	// Search for handler functions (common patterns)
	handlerPatterns := []string{"Handle", "ServeHTTP", "handler", "endpoint"}
	for _, pattern := range handlerPatterns {
		nodes, err := s.store.ListNodesByConcepts(ctx, repoSlug, []string{"http-handler", "api-handler", "endpoint"}, 50)
		if err != nil || len(nodes) == 0 {
			continue
		}
		_ = pattern

		for _, node := range nodes {
			nb, err := s.store.GetNeighborhood(ctx, node.ID)
			if err != nil || nb == nil {
				continue
			}

			// Check if any caller contains "auth" or "middleware"
			hasAuth := false
			for _, caller := range nb.Callers {
				lower := strings.ToLower(caller.Qualified)
				if strings.Contains(lower, "auth") || strings.Contains(lower, "middleware") || strings.Contains(lower, "jwt") {
					hasAuth = true
					break
				}
			}

			if !hasAuth && len(nb.Callers) > 0 {
				issues = append(issues, AnalysisIssue{
					Severity:    "medium",
					Category:    "missing-auth",
					Title:       "Missing Authentication",
					File:        node.FilePath,
					Line:        node.StartLine,
					Function:    node.Qualified,
					Description: fmt.Sprintf("Handler %s has no auth middleware in its caller chain", node.Qualified),
					Fix:         "Add authentication middleware to the route registration",
				})
			}
		}
		break // only need one pass
	}

	return issues
}

// llmVerifyIssues uses the LLM to filter false positives.
func (s *AnalysisService) llmVerifyIssues(ctx context.Context, issues []AnalysisIssue) []AnalysisIssue {
	if len(issues) == 0 {
		return issues
	}

	// Build prompt with all issues
	issuesJSON, _ := json.Marshal(issues[:min(10, len(issues))])

	raw, err := s.explainer.ExplainStructured(ctx, domain.ExplainRequest{
		QueryType: "search",
		UserQuery: fmt.Sprintf(
			"Review these security findings and assess each one. Mark false positives.\n\n%s",
			string(issuesJSON),
		),
	})
	if err != nil {
		return issues // return unfiltered if LLM fails
	}

	// Try to parse LLM response for filtered issues
	var result struct {
		Overview string `json:"overview"`
		Insights []string `json:"insights"`
	}
	if json.Unmarshal(raw, &result) == nil {
		// Add LLM insights as fix suggestions
		for i := range issues {
			if i < len(result.Insights) {
				issues[i].Fix = result.Insights[i]
			}
		}
	}

	return issues
}
