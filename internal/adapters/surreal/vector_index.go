package surreal

import (
	"context"
	"fmt"

	surrealdb "github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// vecRow is the raw result row from an HNSW ANN query.
type vecRow struct {
	ID         *models.RecordID `json:"id"`
	Name       string           `json:"name"`
	Qualified  string           `json:"qualified"`
	FilePath   string           `json:"file_path"`
	RepoSlug   string           `json:"repo_slug"`
	Language   string           `json:"language"`
	Signature  string           `json:"signature"`
	Docstring  string           `json:"docstring"`
	Body       string           `json:"body"`
	StartLine  int              `json:"start_line"`
	EndLine    int              `json:"end_line"`
	Centrality int              `json:"centrality"`
	VecDist    float64          `json:"vec_dist"` // raw cosine distance from vector::distance::knn()
}

// Search implements domain.VectorIndex on the VectorAdapter wrapper.
func (v *VectorAdapter) Search(ctx context.Context, query []float32, opts domain.VectorSearchOpts) ([]types.ScoredNode, error) {
	return v.SurrealAdapter.VectorSearch(ctx, query, opts)
}

// VectorSearch performs HNSW approximate nearest-neighbor vector search across
// the relevant node tables. Results are filtered by MinScore and ranked by
// VectorScore = 1 - vec_dist (cosine similarity, higher is better).
func (a *SurrealAdapter) VectorSearch(ctx context.Context, query []float32, opts domain.VectorSearchOpts) ([]types.ScoredNode, error) {
	if len(query) == 0 {
		return nil, domain.Validation("query vector is empty")
	}
	topK := opts.TopK
	if topK <= 0 {
		topK = 10
	}
	effort := opts.Effort
	if effort <= 0 {
		effort = 40
	}

	// Determine which tables to search.
	tables := vecTablesToSearch(opts.NodeKinds)

	var allResults []types.ScoredNode

	for _, table := range tables {
		rows, err := a.vecSearchTable(ctx, table, query, topK, effort, opts.RepoSlug)
		if err != nil {
			// Log and continue — a missing index on one table should not abort everything.
			a.log.Warn("vector search failed for table", "table", table, "err", err)
			continue
		}
		for _, r := range rows {
			score := 1.0 - r.VecDist // cosine similarity (distance → similarity)
			if score < opts.MinScore {
				continue
			}
			nodeID := ""
			if r.ID != nil {
				nodeID = fmt.Sprintf("%s:%v", r.ID.Table, r.ID.ID)
			}
			allResults = append(allResults, types.ScoredNode{
				Node: types.CodeNode{
					ID:        nodeID,
					Kind:      kindFromTable(table),
					Name:      r.Name,
					Qualified: r.Qualified,
					FilePath:  r.FilePath,
					RepoSlug:  r.RepoSlug,
					Language:  r.Language,
					Signature: r.Signature,
					Docstring: r.Docstring,
					Body:      r.Body,
					StartLine: r.StartLine,
					EndLine:   r.EndLine,
				},
				VectorScore: score,
				Centrality:  r.Centrality,
			})
		}
	}

	// Sort descending by VectorScore then truncate to TopK.
	sortScoredNodes(allResults)
	if len(allResults) > topK {
		allResults = allResults[:topK]
	}
	return allResults, nil
}

// vecSearchTable runs HNSW ANN against a single table.
func (a *SurrealAdapter) vecSearchTable(
	ctx context.Context,
	table string,
	query []float32,
	topK, effort int,
	repoSlug string,
) ([]vecRow, error) {
	var q string
	params := map[string]any{
		"q":      query,
		"topk":   topK,
		"effort": effort,
	}

	// SurrealDB v3 requires literal integers for HNSW <|K,EF|> parameters,
	// and "function" must be backtick-escaped (reserved keyword).
	escaped := fmt.Sprintf("`%s`", table)
	cols := selectCols(table)

	if repoSlug != "" {
		q = fmt.Sprintf(`
SELECT %s,
    vector::distance::knn() AS vec_dist
FROM %s
WHERE embedding <|%d,%d|> $q
  AND repo = $repo_ref;`, cols, escaped, topK, effort)
		params["repo_ref"] = models.NewRecordID("repo", repoSlug)
	} else {
		q = fmt.Sprintf(`
SELECT %s,
    vector::distance::knn() AS vec_dist
FROM %s
WHERE embedding <|%d,%d|> $q;`, cols, escaped, topK, effort)
	}

	results, err := surrealdb.Query[[]vecRow](ctx, a.db, q, params)
	if err != nil {
		return nil, fmt.Errorf("hnsw search %s: %w", table, err)
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	return (*results)[0].Result, nil
}

// vecTablesToSearch returns the list of SurrealDB tables to query.
// If NodeKinds is empty, all four tables are searched.
func vecTablesToSearch(kinds []types.NodeKind) []string {
	if len(kinds) == 0 {
		return []string{"function", "class", "file", "module"}
	}
	seen := make(map[string]bool, len(kinds))
	var tables []string
	for _, k := range kinds {
		t := nodeTable(string(k))
		if !seen[t] {
			seen[t] = true
			tables = append(tables, t)
		}
	}
	return tables
}

// sortScoredNodes sorts a slice of ScoredNode descending by VectorScore.
// Uses a simple insertion sort — acceptable for TopK <= 50.
func sortScoredNodes(nodes []types.ScoredNode) {
	for i := 1; i < len(nodes); i++ {
		key := nodes[i]
		j := i - 1
		for j >= 0 && nodes[j].VectorScore < key.VectorScore {
			nodes[j+1] = nodes[j]
			j--
		}
		nodes[j+1] = key
	}
}

// deduplicateVecResults removes duplicate node IDs keeping highest score.
func deduplicateVecResults(nodes []types.ScoredNode) []types.ScoredNode {
	seen := make(map[string]int, len(nodes))
	out := nodes[:0]
	for _, n := range nodes {
		if idx, ok := seen[n.Node.ID]; ok {
			if out[idx].VectorScore < n.VectorScore {
				out[idx] = n
			}
			continue
		}
		seen[n.Node.ID] = len(out)
		out = append(out, n)
	}
	return out
}
