package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// DataFlowService enriches query results with interprocedural data-flow context.
// It is designed to be used as an optional dependency of QueryService: when
// present, it fetches the graph neighborhood for each top-K result and builds
// a structured GraphContext string that the LLM explainer uses to understand
// how data flows through the codebase.
type DataFlowService struct {
	graph domain.OpenCodeGraph
	log   *slog.Logger
}

// NewDataFlowService creates a new DataFlowService.
func NewDataFlowService(graph domain.OpenCodeGraph) *DataFlowService {
	return &DataFlowService{
		graph: graph,
		log:   slog.Default().With("service", "dataflow"),
	}
}

// BuildFlowContext returns a formatted string describing the data-flow
// neighborhood of each function node in results. The string is intended to be
// passed as ExplainRequest.GraphContext so the LLM explanation includes
// data-flow paths alongside the code excerpts.
func (df *DataFlowService) BuildFlowContext(ctx context.Context, results []types.ScoredNode) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Data-flow context for top results:\n\n")

	written := 0
	for _, scored := range results {
		n := scored.Node
		if n.ID == "" {
			continue
		}

		nb, err := df.graph.Neighbors(ctx, n.ID)
		if err != nil || nb == nil || nb.IsEmpty() {
			continue
		}

		sb.WriteString(fmt.Sprintf("%s (%s:%d):\n", n.Qualified, n.FilePath, n.StartLine))

		if len(nb.Callees) > 0 {
			sb.WriteString(fmt.Sprintf("  Calls: %s\n", strings.Join(neighborNames(nb.Callees), ", ")))
		}
		if len(nb.Callers) > 0 {
			sb.WriteString(fmt.Sprintf("  Called by: %s\n", strings.Join(neighborNames(nb.Callers), ", ")))
		}
		if len(nb.DataSinks) > 0 {
			parts := make([]string, 0, len(nb.DataSinks))
			for _, s := range nb.DataSinks {
				part := s.Qualified
				if s.ParamName != "" {
					part += fmt.Sprintf(" (param %q)", s.ParamName)
				} else if s.ArgExpr != "" {
					part += fmt.Sprintf(" (arg %q)", s.ArgExpr)
				}
				parts = append(parts, part)
			}
			sb.WriteString(fmt.Sprintf("  Data flows to: %s\n", strings.Join(parts, ", ")))
		}
		if len(nb.DataSources) > 0 {
			parts := make([]string, 0, len(nb.DataSources))
			for _, s := range nb.DataSources {
				part := s.Qualified
				if s.ArgExpr != "" {
					part += fmt.Sprintf(" via %q", s.ArgExpr)
				}
				parts = append(parts, part)
			}
			sb.WriteString(fmt.Sprintf("  Data flows from: %s\n", strings.Join(parts, ", ")))
		}
		if len(nb.Reads) > 0 {
			sb.WriteString(fmt.Sprintf("  Reads: %s\n", strings.Join(nb.Reads, ", ")))
		}
		if len(nb.Writes) > 0 {
			sb.WriteString(fmt.Sprintf("  Writes: %s\n", strings.Join(nb.Writes, ", ")))
		}
		sb.WriteString("\n")
		written++
	}

	if written == 0 {
		return ""
	}
	return sb.String()
}
