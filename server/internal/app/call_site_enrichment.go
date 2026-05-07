package app

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// contextLines is the number of lines to show above and below the call line.
const contextLines = 3

// callExpressionRegexp matches a function or method call expression on a single line.
// It captures the callee (dotted name) followed by a parenthesised argument list.
// The argument list is greedy-but-single-line: we stop at the first unmatched ')'.
var callExpressionRegexp = regexp.MustCompile(`\b(\w+(?:\.\w+)*(?:\[[\w,\s*]+\])?\s*\([^)]*\))`)

// EnrichAffectedWithCallSites post-processes a flat affected-node list in place,
// populating CallSiteExcerpt, CallExpression, and CallLine on each entry where a
// matching "calls" edge exists. Errors are best-effort and non-fatal.
func EnrichAffectedWithCallSites(
	ctx context.Context,
	graph domain.OpenCodeGraph,
	repoSlug string,
	affected []types.AffectedNode,
) error {
	if len(affected) == 0 {
		return nil
	}

	// Fetch all "calls" edges for the repository in one round trip.
	edges, err := graph.ListEdges(ctx, repoSlug, []string{"calls"})
	if err != nil {
		slog.Warn("EnrichAffectedWithCallSites: failed to list edges", "repo", repoSlug, "err", err)
		return nil // non-fatal
	}

	// Index edges by FromID for O(1) lookup per affected node.
	edgesByFrom := indexEdgesByFrom(edges)

	for i := range affected {
		nodeID := affected[i].Node.ID
		edge, ok := pickRepresentativeEdge(edgesByFrom, nodeID)
		if !ok {
			continue
		}

		_, callerBody, absoluteLine, relativeLine, ok := resolveCallSite(ctx, graph, edge)
		if !ok {
			continue
		}

		excerpt := sliceLines(callerBody, relativeLine, contextLines)
		callLineStr := callLineText(callerBody, relativeLine)

		affected[i].CallSiteExcerpt = excerpt
		affected[i].CallLine = absoluteLine
		affected[i].CallExpression = extractCallExpression(callLineStr)
	}

	return nil
}

// EnrichHopsWithCallSites post-processes a flat (or recursive) trace-hop tree in
// place, populating CallSiteExcerpt and CallExpression on each hop where a matching
// "calls" edge exists. Errors are best-effort and non-fatal.
func EnrichHopsWithCallSites(
	ctx context.Context,
	graph domain.OpenCodeGraph,
	repoSlug string,
	hops []types.TraceHop,
) error {
	if len(hops) == 0 {
		return nil
	}

	// Fetch all "calls" edges for the repository in one round trip.
	edges, err := graph.ListEdges(ctx, repoSlug, []string{"calls"})
	if err != nil {
		slog.Warn("EnrichHopsWithCallSites: failed to list edges", "repo", repoSlug, "err", err)
		return nil // non-fatal
	}

	edgesByFrom := indexEdgesByFrom(edges)
	enrichHopsRecursive(ctx, graph, edgesByFrom, hops)
	return nil
}

// enrichHopsRecursive walks the hop tree depth-first and enriches each hop.
func enrichHopsRecursive(
	ctx context.Context,
	graph domain.OpenCodeGraph,
	edgesByFrom map[string][]types.CodeEdge,
	hops []types.TraceHop,
) {
	for i := range hops {
		nodeID := hops[i].Node.ID
		edge, ok := pickRepresentativeEdge(edgesByFrom, nodeID)
		if !ok {
			goto children
		}
		{
			_, callerBody, _, relativeLine, ok := resolveCallSite(ctx, graph, edge)
			if ok {
				hops[i].CallSiteExcerpt = sliceLines(callerBody, relativeLine, contextLines)
				hops[i].CallExpression = extractCallExpression(callLineText(callerBody, relativeLine))
			}
		}
	children:
		if len(hops[i].Children) > 0 {
			enrichHopsRecursive(ctx, graph, edgesByFrom, hops[i].Children)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// indexEdgesByFrom builds a map from FromID to the list of edges originating there.
func indexEdgesByFrom(edges []types.CodeEdge) map[string][]types.CodeEdge {
	index := make(map[string][]types.CodeEdge, len(edges))
	for _, e := range edges {
		index[e.FromID] = append(index[e.FromID], e)
	}
	return index
}

// pickRepresentativeEdge returns the first edge where FromID matches nodeID and
// CallSite is non-empty.
func pickRepresentativeEdge(edgesByFrom map[string][]types.CodeEdge, nodeID string) (types.CodeEdge, bool) {
	for _, e := range edgesByFrom[nodeID] {
		if e.CallSite != "" {
			return e, true
		}
	}
	return types.CodeEdge{}, false
}

// resolveCallSite parses a "file.go:lineno" CallSite, fetches the caller node's
// body, and returns (callerNode, body, absoluteLine, relativeBodyLine, ok).
// absoluteLine is the 1-based line in the file (stored as CallLine for the user).
// relativeBodyLine is the 1-based line within caller.Body (used for slicing).
func resolveCallSite(
	ctx context.Context,
	graph domain.OpenCodeGraph,
	edge types.CodeEdge,
) (*types.CodeNode, string, int, int, bool) {
	// CallSite format: "path/to/file.go:42"
	callSite := edge.CallSite
	colonIdx := strings.LastIndexByte(callSite, ':')
	if colonIdx < 0 {
		return nil, "", 0, 0, false
	}
	lineStr := callSite[colonIdx+1:]
	absoluteLine, err := strconv.Atoi(lineStr)
	if err != nil || absoluteLine <= 0 {
		return nil, "", 0, 0, false
	}

	// The caller is the FROM node of the "calls" edge.
	caller, err := graph.GetNode(ctx, edge.FromID)
	if err != nil || caller == nil {
		return nil, "", 0, 0, false
	}

	// Convert the absolute file line to a 1-based line within the function body.
	// CodeNode.StartLine is 1-based; body starts at that line in the file.
	relativeLine := absoluteLine
	if caller.StartLine > 0 {
		relativeLine = absoluteLine - caller.StartLine + 1
	}
	if relativeLine <= 0 {
		relativeLine = 1
	}

	return caller, caller.Body, absoluteLine, relativeLine, true
}

// sliceLines returns a ±contextLines slice from body around the 1-based target line.
// The body is the full function source; target is an absolute file line number.
// We try to relativise it: if the node StartLine is set, target is adjusted.
// If the body has fewer lines than expected, we clamp gracefully.
func sliceLines(body string, targetLine int, context int) string {
	if body == "" {
		return ""
	}
	lines := strings.Split(body, "\n")
	if len(lines) == 0 {
		return ""
	}
	// targetLine is 1-based within the body (already adjusted by caller if needed,
	// but the body itself starts at line 1 relative to itself).
	idx := targetLine - 1 // 0-based
	if idx < 0 {
		idx = 0
	}
	if idx >= len(lines) {
		idx = len(lines) - 1
	}
	from := idx - context
	if from < 0 {
		from = 0
	}
	to := idx + context + 1
	if to > len(lines) {
		to = len(lines)
	}
	return strings.Join(lines[from:to], "\n")
}

// callLineText returns the single line at the 1-based target index from body,
// or empty string if out of range.
func callLineText(body string, targetLine int) string {
	if body == "" || targetLine <= 0 {
		return ""
	}
	lines := strings.Split(body, "\n")
	idx := targetLine - 1
	if idx >= len(lines) {
		return ""
	}
	return lines[idx]
}

// extractCallExpression attempts to extract the first function/method call
// expression from a single line of source text. Returns empty string if no
// call is found or the line is malformed.
func extractCallExpression(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	matches := callExpressionRegexp.FindStringSubmatch(line)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

// callSiteFileAndLine splits a CallSite "path:line" string and returns its
// components. Exported for tests.
func callSiteFileAndLine(callSite string) (string, int, error) {
	colonIdx := strings.LastIndexByte(callSite, ':')
	if colonIdx < 0 {
		return "", 0, fmt.Errorf("no colon in call site %q", callSite)
	}
	line, err := strconv.Atoi(callSite[colonIdx+1:])
	if err != nil {
		return "", 0, fmt.Errorf("non-numeric line in call site %q: %w", callSite, err)
	}
	return callSite[:colonIdx], line, nil
}
