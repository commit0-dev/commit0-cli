package surreal

import (
	"context"
	"fmt"
	"strings"

	surrealdb "github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// Field-level data flow traversal methods on SurrealAdapter.
// Used by openCodeGraphAdapter for OpenCodeGraph.TraverseGraph with data_flow labels.

// TraceFieldFlow follows field-level data_flow edges from a start node,
// optionally filtering by field_path. Returns FieldFlowHop entries with
// mutation metadata extracted from edge properties.
func (a *SurrealAdapter) TraceFieldFlow(ctx context.Context, startID string, fieldPath string, depth int, direction string) ([]types.FieldFlowHop, error) {
	if depth <= 0 {
		depth = 10
	}

	table, localID := splitRecordID(startID)
	if table == "" || localID == "" {
		return nil, domain.Validation(fmt.Sprintf("invalid start node ID: %q", startID))
	}

	// Build the direction-specific traversal query.
	// Forward: follow ->data_flow-> edges (where data goes TO)
	// Reverse: follow <-data_flow<- edges (where data comes FROM)
	var traversal string
	switch direction {
	case "reverse":
		traversal = "<-data_flow<-"
	default: // forward
		traversal = "->data_flow->"
	}

	// SurrealDB graph traversal with depth
	q := fmt.Sprintf(`
		SELECT
			out.* AS node,
			call_site, is_dynamic, call_type,
			meta::field_path AS field_path,
			meta::mutation_type AS mutation_type,
			meta::mutation_expr AS mutation_expr,
			meta::mutation_line AS mutation_line,
			meta::arg_expr AS arg_expr,
			meta::param_name AS param_name
		FROM type::record($start)%s(data_flow WHERE true)
		LIMIT $depth;`, traversal)

	// Simpler approach: just query data_flow edges connected to this node
	// and recursively follow them up to depth.
	var hops []types.FieldFlowHop
	visited := make(map[string]bool)

	err := a.traceFieldFlowRecursive(ctx, startID, fieldPath, direction, depth, 1, visited, &hops)
	if err != nil {
		return nil, err
	}

	_ = q // complex graph traversal reserved for future optimization
	return hops, nil
}

// traceFieldFlowRecursive follows data_flow edges recursively.
func (a *SurrealAdapter) traceFieldFlowRecursive(
	ctx context.Context,
	nodeID, fieldPath, direction string,
	maxDepth, currentDepth int,
	visited map[string]bool,
	hops *[]types.FieldFlowHop,
) error {
	if currentDepth > maxDepth || visited[nodeID] {
		return nil
	}
	visited[nodeID] = true

	// Query data_flow edges from/to this node
	var q string
	table, localID := splitRecordID(nodeID)
	if table == "" {
		return nil
	}

	params := map[string]any{
		"node": models.NewRecordID(table, localID),
	}

	if direction == "reverse" {
		q = `SELECT *, in AS source_node, out AS target_node FROM data_flow WHERE out = $node`
	} else {
		q = `SELECT *, in AS source_node, out AS target_node FROM data_flow WHERE in = $node`
	}

	// Filter by field_path if specified
	if fieldPath != "" {
		q += ` AND (meta.field_path = $field_path OR meta.field_path IS NONE)`
		params["field_path"] = fieldPath
	}
	q += ` LIMIT 50;`

	type flowEdgeRow struct {
		In        *models.RecordID  `json:"in"`
		Out       *models.RecordID  `json:"out"`
		CallSite  string            `json:"call_site"`
		FieldPath string            `json:"field_path"`
		Metadata  map[string]string `json:"metadata"`
	}

	results, err := surrealdb.Query[[]flowEdgeRow](ctx, a.readDB(), q, params)
	if err != nil {
		return nil //nolint:nilerr // non-fatal: stop traversal at this branch
	}
	if results == nil || len(*results) == 0 {
		return nil
	}

	for _, r := range (*results)[0].Result {
		// Determine the next node to follow
		var nextNodeID string
		if direction == "reverse" && r.In != nil {
			nextNodeID = fmt.Sprintf("%s:%v", r.In.Table, r.In.ID)
		} else if r.Out != nil {
			nextNodeID = fmt.Sprintf("%s:%v", r.Out.Table, r.Out.ID)
		}
		if nextNodeID == "" {
			continue
		}

		// Fetch the target node
		nextNode, err := a.GetNode(ctx, nextNodeID)
		if err != nil || nextNode == nil {
			continue
		}

		// Build the hop with mutation metadata
		hop := types.FieldFlowHop{
			Node:      *nextNode,
			FieldPath: r.FieldPath,
			Depth:     currentDepth,
			Edge: types.CodeEdge{
				Kind:     types.EdgeDataFlow,
				CallSite: r.CallSite,
				Metadata: r.Metadata,
			},
		}

		if r.Metadata != nil {
			hop.ParamName = r.Metadata["param_name"]
			hop.ArgExpr = r.Metadata["arg_expr"]
			hop.FieldPath = r.Metadata["field_path"]
			if mt := r.Metadata["mutation_type"]; mt != "" {
				hop.MutationType = types.MutationKind(mt)
				hop.MutationExpr = r.Metadata["mutation_expr"]
				if ml := r.Metadata["mutation_line"]; ml != "" {
					for _, c := range ml {
						if c >= '0' && c <= '9' {
							hop.MutationLine = hop.MutationLine*10 + int(c-'0')
						}
					}
				}
			}
		}

		*hops = append(*hops, hop)

		// Recurse
		if err := a.traceFieldFlowRecursive(ctx, nextNodeID, fieldPath, direction, maxDepth, currentDepth+1, visited, hops); err != nil {
			return err
		}
	}

	return nil
}

// FindMutations returns all data_flow hops where the specified field is mutated.
func (a *SurrealAdapter) FindMutations(ctx context.Context, repoSlug string, fieldPath string) ([]types.FieldFlowHop, error) {
	params := map[string]any{}

	q := `SELECT *, in AS source, out AS target FROM data_flow WHERE meta.mutation_type IS NOT NONE`
	if fieldPath != "" {
		q += ` AND meta.field_path = $field_path`
		params["field_path"] = fieldPath
	}
	q += ` LIMIT 100;`

	type mutRow struct {
		In       *models.RecordID  `json:"in"`
		Out      *models.RecordID  `json:"out"`
		CallSite string            `json:"call_site"`
		Metadata map[string]string `json:"metadata"`
	}

	results, err := surrealdb.Query[[]mutRow](ctx, a.readDB(), q, params)
	if err != nil {
		return nil, fmt.Errorf("find mutations: %w", err)
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}

	var hops []types.FieldFlowHop
	for _, r := range (*results)[0].Result {
		nodeID := ""
		if r.Out != nil {
			nodeID = fmt.Sprintf("%s:%v", r.Out.Table, r.Out.ID)
		}
		node, err := a.GetNode(ctx, nodeID)
		if err != nil || node == nil {
			continue
		}

		hop := types.FieldFlowHop{
			Node: *node,
			Edge: types.CodeEdge{Kind: types.EdgeDataFlow, CallSite: r.CallSite, Metadata: r.Metadata},
		}
		if r.Metadata != nil {
			hop.FieldPath = r.Metadata["field_path"]
			hop.MutationType = types.MutationKind(r.Metadata["mutation_type"])
			hop.MutationExpr = r.Metadata["mutation_expr"]
		}
		hops = append(hops, hop)
	}

	_ = strings.TrimSpace // suppress import warning
	return hops, nil
}
