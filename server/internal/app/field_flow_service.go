package app

import (
	"context"
	"log/slog"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// FieldFlowRequest configures a field-level data flow trace.
type FieldFlowRequest struct {
	Symbol        string // qualified function name to start from
	FieldPath     string // optional: specific field to track (e.g. "user.Email")
	RepoSlug      string
	Direction     string // "forward", "reverse", "both"
	Depth         int
	ShowMutations bool // if true, only return chains with mutations
}

// FieldFlowService traces field-level data flow through the code graph.
// Unlike the existing DataFlowService (which builds context strings for the LLM),
// this service returns structured FieldFlowResult for interactive tracing.
type FieldFlowService struct {
	graph     domain.OpenCodeGraph
	embedder  domain.Embedder
	explainer domain.LLMExplainer
	cfg       *config.Config
	log       *slog.Logger
}

// NewFieldFlowService creates a new field flow tracing service.
func NewFieldFlowService(
	graph domain.OpenCodeGraph,
	embedder domain.Embedder,
	explainer domain.LLMExplainer,
	cfg *config.Config,
) *FieldFlowService {
	return &FieldFlowService{
		graph:     graph,
		embedder:  embedder,
		explainer: explainer,
		cfg:       cfg,
		log:       slog.Default().With("service", "field_flow"),
	}
}

// TraceFieldFlow traces data flow for a specific field through the code graph.
// It resolves the symbol to a node, then follows data_flow edges that match
// the field_path, collecting mutation points along the way.
func (s *FieldFlowService) TraceFieldFlow(ctx context.Context, req FieldFlowRequest) (*types.FieldFlowResult, error) {
	if req.Symbol == "" {
		return nil, domain.Validation("symbol is required")
	}
	if req.Direction == "" {
		req.Direction = "forward"
	}
	if req.Depth <= 0 {
		req.Depth = 10
	}

	startTime := ctx.Value(struct{}{}) // unused, just for timing pattern consistency
	_ = startTime

	// Resolve symbol to node
	node, err := s.resolveSymbol(ctx, req.RepoSlug, req.Symbol)
	if err != nil {
		return nil, err
	}

	// Trace data flow edges via label-parameterized traversal (OpenCodeGraph §3).
	// Uses TraverseGraph instead of the old recursive TraceDataFlow which
	// times out on dangling edge references.
	edgeLabels := []string{"data_flow", "reads", "writes"}
	hops, err := s.graph.TraverseGraph(ctx, node.ID, edgeLabels, req.Direction, req.Depth)
	if err != nil {
		return nil, err
	}

	// Convert TraceHops to FieldFlowHops with mutation metadata
	chains := s.buildFieldFlowChains(hops, req.FieldPath)

	// Filter to mutation-only chains if requested
	if req.ShowMutations {
		var filtered []types.FieldFlowChain
		for _, chain := range chains {
			if len(chain.Mutations) > 0 {
				filtered = append(filtered, chain)
			}
		}
		chains = filtered
	}

	s.log.Info("field flow trace complete",
		"symbol", req.Symbol,
		"field", req.FieldPath,
		"direction", req.Direction,
		"chains", len(chains),
	)

	return &types.FieldFlowResult{
		Root:      *node,
		Direction: req.Direction,
		Chains:    chains,
	}, nil
}

// resolveSymbol finds a node using the shared resolver.
func (s *FieldFlowService) resolveSymbol(ctx context.Context, repoSlug, symbol string) (*types.CodeNode, error) {
	return ResolveSymbol(ctx, s.graph, s.embedder, repoSlug, symbol)
}

// buildFieldFlowChains converts TraceHops into FieldFlowChains,
// extracting mutation metadata from edge metadata.
func (s *FieldFlowService) buildFieldFlowChains(hops []types.TraceHop, filterField string) []types.FieldFlowChain {
	if len(hops) == 0 {
		return nil
	}

	// Group hops by field_path into chains
	chainMap := make(map[string]*types.FieldFlowChain)

	var walkHops func(hops []types.TraceHop, depth int)
	walkHops = func(hops []types.TraceHop, depth int) {
		for _, hop := range hops {
			fieldPath := hop.Edge.Metadata["field_path"]
			if fieldPath == "" {
				fieldPath = hop.Edge.Metadata["field"]
				if fieldPath == "" {
					fieldPath = "_default"
				}
			}

			// Skip if filtering by field and this doesn't match
			if filterField != "" && fieldPath != filterField && fieldPath != "_default" {
				continue
			}

			flowHop := types.FieldFlowHop{
				Node:      hop.Node,
				Edge:      hop.Edge,
				FieldPath: fieldPath,
				ParamName: hop.Edge.Metadata["param_name"],
				ArgExpr:   hop.Edge.Metadata["arg_expr"],
				Depth:     depth,
			}

			// Extract mutation metadata
			if mt, ok := hop.Edge.Metadata["mutation_type"]; ok && mt != "" {
				flowHop.MutationType = types.MutationKind(mt)
				flowHop.MutationExpr = hop.Edge.Metadata["mutation_expr"]
				if ml, ok := hop.Edge.Metadata["mutation_line"]; ok {
					for _, c := range ml {
						if c >= '0' && c <= '9' {
							flowHop.MutationLine = flowHop.MutationLine*10 + int(c-'0')
						}
					}
				}
			}

			chain, exists := chainMap[fieldPath]
			if !exists {
				chain = &types.FieldFlowChain{FieldPath: fieldPath}
				chainMap[fieldPath] = chain
			}
			chain.Hops = append(chain.Hops, flowHop)

			if flowHop.MutationType != "" && flowHop.MutationType != types.MutationNone {
				chain.Mutations = append(chain.Mutations, flowHop)
				if chain.TaintPoint == nil {
					hp := flowHop // copy
					chain.TaintPoint = &hp
				}
			}

			// Recurse into children
			if len(hop.Children) > 0 {
				walkHops(hop.Children, depth+1)
			}
		}
	}

	walkHops(hops, 1)

	// Convert map to slice
	var chains []types.FieldFlowChain
	for _, chain := range chainMap {
		chains = append(chains, *chain)
	}
	return chains
}
