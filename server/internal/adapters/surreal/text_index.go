package surreal

import (
	"context"
	"fmt"

	surrealdb "github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"

	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// ftsRow is the raw result row returned by a BM25 full-text search query.
type ftsRow struct {
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
	FTSScore   float64          `json:"fts_score"` // from search::score()
}

// Search implements domain.TextIndex on the TextAdapter wrapper.
func (t *TextAdapter) Search(ctx context.Context, query string, opts domain.TextSearchOpts) ([]types.ScoredNode, error) {
	return t.SurrealAdapter.TextSearch(ctx, query, opts)
}

// TextSearch performs BM25 full-text search across the relevant node tables.
// opts.Fields selects which fields to search (name, qualified, docstring).
// If Fields is empty, all three are searched.
func (a *SurrealAdapter) TextSearch(ctx context.Context, query string, opts domain.TextSearchOpts) ([]types.ScoredNode, error) {
	if query == "" {
		return nil, domain.Validation("query text is empty")
	}
	topK := opts.TopK
	if topK <= 0 {
		topK = 10
	}

	tables := ftsTablesToSearch(opts.NodeKinds)
	fields := opts.Fields
	if len(fields) == 0 {
		fields = []string{"name", "qualified", "docstring"}
	}

	var allResults []types.ScoredNode

	for _, table := range tables {
		// Module table uses "path" instead of "qualified".
		tableFields := fields
		if table == "module" {
			tableFields = ftsFieldsForModule(fields)
		}
		rows, err := a.ftsSearchTable(ctx, table, query, tableFields, topK, opts.RepoSlug)
		if err != nil {
			a.log.Warn("fts search failed for table", "table", table, "err", err)
			continue
		}
		for _, r := range rows {
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
				FTSScore:   r.FTSScore,
				Centrality: r.Centrality,
			})
		}
	}

	// Sort descending by FTSScore then truncate.
	sortByFTSScore(allResults)
	if len(allResults) > topK {
		allResults = allResults[:topK]
	}
	return allResults, nil
}

// ftsSearchTable executes a BM25 search on a single table.
func (a *SurrealAdapter) ftsSearchTable(
	ctx context.Context,
	table string,
	query string,
	fields []string,
	topK int,
	repoSlug string,
) ([]ftsRow, error) {
	// Build the WHERE clause: OR across requested fields using @1@ operator.
	whereClause := buildFTSWhere(fields)

	params := map[string]any{
		"text": query,
		"topk": topK,
	}

	// "function" is a reserved keyword in SurrealDB v3 — backtick-escape all table names.
	escaped := fmt.Sprintf("`%s`", table)
	cols := selectCols(table)

	var q string
	if repoSlug != "" {
		q = fmt.Sprintf(`
SELECT %s,
    search::score(1) AS fts_score
FROM %s
WHERE (%s) AND repo = $repo_ref
ORDER BY fts_score DESC
LIMIT $topk;`, cols, escaped, whereClause)
		params["repo_ref"] = models.NewRecordID("repo", repoSlug)
	} else {
		q = fmt.Sprintf(`
SELECT %s,
    search::score(1) AS fts_score
FROM %s
WHERE %s
ORDER BY fts_score DESC
LIMIT $topk;`, cols, escaped, whereClause)
	}

	results, err := surrealdb.Query[[]ftsRow](ctx, a.db, q, params)
	if err != nil {
		return nil, fmt.Errorf("bm25 search %s: %w", table, err)
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	return (*results)[0].Result, nil
}

// buildFTSWhere constructs the BM25 predicate for the given field list.
// Example: "name @1@ $text OR qualified @1@ $text OR docstring @1@ $text"
// The @1@ operator references search index 1 (fn_name_fts / fn_doc_fts).
func buildFTSWhere(fields []string) string {
	if len(fields) == 0 {
		return "name @1@ $text"
	}
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, fmt.Sprintf("%s @1@ $text", f))
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += " OR "
		}
		result += p
	}
	return result
}

// ftsFieldsForModule maps the default FTS field list to module-compatible
// fields: "qualified" → "path" (module table has path, not qualified).
func ftsFieldsForModule(fields []string) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		switch f {
		case "qualified":
			out = append(out, "path")
		default:
			out = append(out, f)
		}
	}
	return out
}

// ftsTablesToSearch returns the tables to query for FTS.
// File table uses path field; class/module have limited FTS indexes.
func ftsTablesToSearch(kinds []types.NodeKind) []string {
	if len(kinds) == 0 {
		return []string{"function", "class", "module"}
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

// sortByFTSScore sorts descending by FTSScore using insertion sort.
func sortByFTSScore(nodes []types.ScoredNode) {
	for i := 1; i < len(nodes); i++ {
		key := nodes[i]
		j := i - 1
		for j >= 0 && nodes[j].FTSScore < key.FTSScore {
			nodes[j+1] = nodes[j]
			j--
		}
		nodes[j+1] = key
	}
}

// Ensure the unused models import doesn't cause a compile error.
var _ = models.RecordID{}
