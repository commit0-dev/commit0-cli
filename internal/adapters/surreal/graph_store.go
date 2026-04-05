package surreal

import (
	"context"
	"fmt"
	"strings"
	"time"

	surrealdb "github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// ---------------------------------------------------------------------------
// Raw DB row structs — used for unmarshalling SurrealDB query results.
// ---------------------------------------------------------------------------

type nodeRow struct {
	ID          *models.RecordID `json:"id"`
	Docstring   string           `json:"docstring"`
	Signature   string           `json:"signature"`
	FilePath    string           `json:"file_path"`
	RepoSlug    string           `json:"repo_slug"`
	Language    string           `json:"language"`
	Visibility  string           `json:"visibility"`
	ContentHash string           `json:"content_hash"`
	Qualified   string           `json:"qualified"`
	Name        string           `json:"name"`
	Body        string           `json:"body"`
	Embedding   []float32        `json:"embedding"`
	EndLine     int              `json:"end_line"`
	StartLine   int              `json:"start_line"`
	Centrality  int              `json:"centrality"`
}

type repoRow struct {
	CreatedAt     time.Time        `json:"created_at"`
	ID            *models.RecordID `json:"id"`
	LastIndexedAt *time.Time       `json:"last_indexed_at"`
	Slug          string           `json:"slug"`
	Path          string           `json:"path"`
	RemoteURL     string           `json:"remote_url"`
	DefaultBranch string           `json:"default_branch"`
	LastCommit    string           `json:"last_commit"`
	Languages     []string         `json:"languages"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// rowToCodeNode converts a raw DB row to a domain CodeNode.
func rowToCodeNode(r nodeRow, kind types.NodeKind) types.CodeNode {
	id := ""
	if r.ID != nil {
		id = fmt.Sprintf("%s:%v", r.ID.Table, r.ID.ID)
	}
	return types.CodeNode{
		ID:          id,
		Kind:        kind,
		Name:        r.Name,
		Qualified:   r.Qualified,
		FilePath:    r.FilePath,
		RepoSlug:    r.RepoSlug,
		Language:    r.Language,
		StartLine:   r.StartLine,
		EndLine:     r.EndLine,
		Signature:   r.Signature,
		Docstring:   r.Docstring,
		Body:        r.Body,
		ContentHash: r.ContentHash,
		Embedding:   r.Embedding,
		Visibility:  r.Visibility,
	}
}

// nodeParams builds the SurrealQL variable map for upsert operations.
func nodeParams(node *types.CodeNode) map[string]any {
	table := nodeTable(string(node.Kind))
	_, localID := splitRecordID(node.ID)
	if localID == "" {
		localID = node.Qualified
	}

	// Pass repo/file as typed models.RecordID objects so the Go SDK serialises
	// them via CBOR correctly — especially when slugs or paths contain '/' which
	// type::record(string) may mis-parse as a division operator.
	repoRef := models.NewRecordID("repo", node.RepoSlug)
	fileRef := models.NewRecordID("file", node.FilePath)

	// SurrealDB 3.0 strict mode: option<T> fields need NONE (not NULL)
	// when the value is absent. The Go SDK's models.None serialises correctly.
	var embedding any = models.None
	if len(node.Embedding) > 0 {
		embedding = node.Embedding
	}

	var docstring any = models.None
	if node.Docstring != "" {
		docstring = node.Docstring
	}

	return map[string]any{
		"record_id":    models.NewRecordID(table, localID),
		"name":         node.Name,
		"qualified":    node.Qualified,
		"file_path":    node.FilePath,
		"repo_slug":    node.RepoSlug,
		"language":     node.Language,
		"start_line":   node.StartLine,
		"end_line":     node.EndLine,
		"signature":    node.Signature,
		"docstring":    docstring,
		"body":         node.Body,
		"content_hash": node.ContentHash,
		"embedding":    embedding,
		"visibility":   defaultVisibility(node.Visibility),
		"repo_ref":     repoRef,
		"file_ref":     fileRef,
	}
}

func defaultVisibility(v string) string {
	if v == "" {
		return "public"
	}
	return v
}

// upsertNodeQuery returns the UPSERT SurrealQL for a given table.
// We store file_path and repo_slug as plain string fields for easy lookup;
// the repo/file REFERENCE fields point to the proper record IDs.
func upsertNodeQuery(table string) string {
	switch table {
	case "function":
		return `
UPSERT type::record($record_id) CONTENT {
    name:         $name,
    qualified:    $qualified,
    file_path:    $file_path,
    repo_slug:    $repo_slug,
    file:         $file_ref,
    repo:         $repo_ref,
    language:     $language,
    start_line:   $start_line,
    end_line:     $end_line,
    signature:    $signature,
    docstring:    $docstring,
    body:         $body,
    content_hash: $content_hash,
    embedding:    $embedding,
    visibility:   $visibility
};`
	case "class":
		return `
UPSERT type::record($record_id) CONTENT {
    name:         $name,
    qualified:    $qualified,
    file_path:    $file_path,
    repo_slug:    $repo_slug,
    file:         $file_ref,
    repo:         $repo_ref,
    language:     $language,
    start_line:   $start_line,
    end_line:     $end_line,
    docstring:    $docstring,
    content_hash: $content_hash,
    embedding:    $embedding
};`
	case "file":
		return `
UPSERT type::record($record_id) CONTENT {
    path:         $file_path,
    file_path:    $file_path,
    repo_slug:    $repo_slug,
    repo:         $repo_ref,
    language:     $language,
    content_hash: $content_hash,
    embedding:    $embedding
};`
	case "module":
		return `
UPSERT type::record($record_id) CONTENT {
    name:      $name,
    path:      $file_path,
    file_path: $file_path,
    repo_slug: $repo_slug,
    repo:      $repo_ref,
    language:  $language,
    docstring: $docstring,
    embedding: $embedding
};`
	}
	return ""
}

// relateEdgeQuery returns a RELATE SurrealQL statement for a given EdgeKind.
func relateEdgeQuery(kind types.EdgeKind) string {
	switch kind {
	case types.EdgeCalls:
		return `
RELATE $from->calls->$to CONTENT {
    call_site:  $call_site,
    is_dynamic: $is_dynamic,
    call_type:  $call_type,
    repo:       $repo_ref
};`
	case types.EdgeImports:
		return `
RELATE $from->imports->$to;`
	case types.EdgeDefines:
		return `
RELATE $from->defines->$to;`
	case types.EdgeInherits:
		return `
RELATE $from->inherits->$to CONTENT {
    kind: $inherit_kind
};`
	case types.EdgeUses:
		return `
RELATE $from->uses->$to CONTENT {
    usage_type: $usage_type
};`
	case types.EdgeDataFlow:
		return `
RELATE $from->data_flow->$to CONTENT {
    call_site:  $call_site,
    param_name: $param_name,
    arg_expr:   $arg_expr,
    arg_type:   $arg_type,
    repo:       $repo_ref
};`
	case types.EdgeReads:
		return `
RELATE $from->reads->$to CONTENT {
    field_name: $field_name,
    repo:       $repo_ref
};`
	case types.EdgeWrites:
		return `
RELATE $from->writes->$to CONTENT {
    field_name: $field_name,
    repo:       $repo_ref
};`
	}
	return ""
}

// edgeParams builds the variable map for a RELATE statement.
func edgeParams(edge *types.CodeEdge, repoSlug string) map[string]any {
	callType := edge.CallType
	if callType == "" {
		callType = "direct"
	}
	inheritKind := "extends"
	if v, ok := edge.Metadata["kind"]; ok {
		inheritKind = v
	}
	usageType := "reference"
	if v, ok := edge.Metadata["usage_type"]; ok {
		usageType = v
	}

	fromTable, fromID := splitRecordID(edge.FromID)
	toTable, toID := splitRecordID(edge.ToID)

	// Default missing table to "function" — unresolved call targets from the
	// parser arrive without a table prefix.
	if fromTable == "" {
		fromTable = "function"
	}
	if toTable == "" {
		toTable = "function"
	}

	// Data-flow metadata
	paramName := optionalMeta(edge.Metadata, "param_name")
	argExpr := optionalMeta(edge.Metadata, "arg_expr")
	argType := optionalMeta(edge.Metadata, "arg_type")
	fieldName := optionalMeta(edge.Metadata, "field")

	return map[string]any{
		"from":         models.NewRecordID(fromTable, fromID),
		"to":           models.NewRecordID(toTable, toID),
		"call_site":    edge.CallSite,
		"is_dynamic":   edge.IsDynamic,
		"call_type":    callType,
		"repo_ref":     models.NewRecordID("repo", repoSlug),
		"inherit_kind": inheritKind,
		"usage_type":   usageType,
		"param_name":   paramName,
		"arg_expr":     argExpr,
		"arg_type":     argType,
		"field_name":   fieldName,
	}
}

// optionalMeta returns models.None when a metadata key is absent, so SurrealDB
// strict mode receives NONE for option<T> fields instead of an empty string.
func optionalMeta(meta map[string]string, key string) any {
	if v, ok := meta[key]; ok && v != "" {
		return v
	}
	return models.None
}

// kindFromTable converts a DB table name back to a NodeKind.
func kindFromTable(table string) types.NodeKind {
	switch table {
	case "function":
		return types.NodeFunction
	case "class":
		return types.NodeClass
	case "file":
		return types.NodeFile
	case "module":
		return types.NodeModule
	}
	return types.NodeFunction
}

// ---------------------------------------------------------------------------
// GraphStore — Node CRUD
// ---------------------------------------------------------------------------

// UpsertNode creates or replaces a single code node.
func (a *SurrealAdapter) UpsertNode(ctx context.Context, node *types.CodeNode) error {
	if node == nil {
		return domain.Validation("node is nil")
	}
	table := nodeTable(string(node.Kind))
	q := upsertNodeQuery(table)
	if q == "" {
		return domain.Validation(fmt.Sprintf("unknown node kind: %s", node.Kind))
	}

	results, err := surrealdb.Query[any](ctx, a.db, q, nodeParams(node))
	if err != nil {
		return fmt.Errorf("upsert node %s: %w", node.ID, err)
	}
	for i, r := range *results {
		if r.Status == "ERR" {
			return fmt.Errorf("upsert node %s stmt %d: %v", node.ID, i, r.Error)
		}
	}
	return nil
}

// GetNode fetches a node by its full record ID (e.g. "function:pkg.Handler").
func (a *SurrealAdapter) GetNode(ctx context.Context, id string) (*types.CodeNode, error) {
	table, localID := splitRecordID(id)
	if table == "" || localID == "" {
		return nil, domain.Validation(fmt.Sprintf("invalid node id: %q", id))
	}

	const q = `SELECT * FROM type::record($id);`
	results, err := surrealdb.Query[[]nodeRow](ctx, a.db, q, map[string]any{
		"id": models.NewRecordID(table, localID),
	})
	if err != nil {
		return nil, fmt.Errorf("get node %s: %w", id, err)
	}

	if results == nil || len(*results) == 0 || len((*results)[0].Result) == 0 {
		return nil, domain.NotFound(fmt.Sprintf("node %s not found", id))
	}

	row := (*results)[0].Result[0]
	node := rowToCodeNode(row, kindFromTable(table))
	return &node, nil
}

// GetNodeByQualified looks up a node by repo slug + fully qualified name.
// If the input contains no dot (i.e. it is an unqualified short name), a
// second pass searches the `name` field so that users can type bare function
// names like "ParseGoMod" instead of "treesitter.ParseGoMod".
func (a *SurrealAdapter) GetNodeByQualified(ctx context.Context, repo, qualified string) (*types.CodeNode, error) {
	// Try each node table in priority order.
	tables := []string{"function", "class", "module", "file"}
	const q = `SELECT * FROM $table WHERE repo = $repo_ref AND qualified = $qualified LIMIT 1;`

	for _, table := range tables {
		results, err := surrealdb.Query[[]nodeRow](ctx, a.db, q, map[string]any{
			"table":     models.Table(table),
			"repo_ref":  models.NewRecordID("repo", repo),
			"qualified": qualified,
		})
		if err != nil {
			return nil, fmt.Errorf("get node by qualified %s/%s: %w", repo, qualified, err)
		}
		if results != nil && len(*results) > 0 && len((*results)[0].Result) > 0 {
			row := (*results)[0].Result[0]
			node := rowToCodeNode(row, kindFromTable(table))
			return &node, nil
		}
	}

	// Fallback: bare name lookup (no dot means the caller supplied a short name).
	if !strings.Contains(qualified, ".") {
		const nameQ = `SELECT * FROM $table WHERE repo = $repo_ref AND name = $name LIMIT 1;`
		for _, table := range tables {
			results, err := surrealdb.Query[[]nodeRow](ctx, a.db, nameQ, map[string]any{
				"table":    models.Table(table),
				"repo_ref": models.NewRecordID("repo", repo),
				"name":     qualified,
			})
			if err != nil {
				return nil, fmt.Errorf("get node by name %s/%s: %w", repo, qualified, err)
			}
			if results != nil && len(*results) > 0 && len((*results)[0].Result) > 0 {
				row := (*results)[0].Result[0]
				node := rowToCodeNode(row, kindFromTable(table))
				return &node, nil
			}
		}
	}

	return nil, domain.NotFound(fmt.Sprintf("node %s/%s not found", repo, qualified))
}

// DeleteNode removes a single record by its full ID.
func (a *SurrealAdapter) DeleteNode(ctx context.Context, id string) error {
	table, localID := splitRecordID(id)
	if table == "" || localID == "" {
		return domain.Validation(fmt.Sprintf("invalid node id: %q", id))
	}

	const q = `DELETE type::record($id);`
	if _, err := surrealdb.Query[any](ctx, a.db, q, map[string]any{
		"id": models.NewRecordID(table, localID),
	}); err != nil {
		return fmt.Errorf("delete node %s: %w", id, err)
	}
	return nil
}

// DeleteNodesByRepo removes all nodes (all tables) that belong to a repo by
// deleting the repo record — all node tables declare
// `repo TYPE record<repo> REFERENCE ON DELETE CASCADE`, so the DB cascades the
// delete to every function, class, file, and module in one round-trip. The repo
// record is then re-created by UpsertRepo on the next indexing run.
func (a *SurrealAdapter) DeleteNodesByRepo(ctx context.Context, repo string) error {
	const q = `DELETE type::record($repo_id);`
	results, err := surrealdb.Query[any](ctx, a.db, q, map[string]any{
		"repo_id": models.NewRecordID("repo", repo),
	})
	if err != nil {
		return fmt.Errorf("delete nodes for repo %s: %w", repo, err)
	}
	for i, r := range *results {
		if r.Status == "ERR" {
			return fmt.Errorf("delete nodes for repo %s stmt %d: %v", repo, i, r.Error)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// GraphStore — Edge CRUD
// ---------------------------------------------------------------------------

// UpsertEdge creates a RELATE edge between two nodes.
func (a *SurrealAdapter) UpsertEdge(ctx context.Context, edge *types.CodeEdge) error {
	if edge == nil {
		return domain.Validation("edge is nil")
	}
	q := relateEdgeQuery(edge.Kind)
	if q == "" {
		return domain.Validation(fmt.Sprintf("unknown edge kind: %s", edge.Kind))
	}

	// Derive repo slug from the FromID.
	repoSlug := ""
	results, err := surrealdb.Query[any](ctx, a.db, q, edgeParams(edge, repoSlug))
	if err != nil {
		return fmt.Errorf("upsert edge %s->%s: %w", edge.FromID, edge.ToID, err)
	}
	for i, r := range *results {
		if r.Status == "ERR" {
			return fmt.Errorf("upsert edge %s->%s stmt %d: %v", edge.FromID, edge.ToID, i, r.Error)
		}
	}
	return nil
}

// DeleteEdgesForNode removes all inbound and outbound edges for a given node.
func (a *SurrealAdapter) DeleteEdgesForNode(ctx context.Context, nodeID string) error {
	table, localID := splitRecordID(nodeID)
	if table == "" || localID == "" {
		return domain.Validation(fmt.Sprintf("invalid node id: %q", nodeID))
	}

	const q = `
DELETE FROM calls    WHERE in = type::record($id) OR out = type::record($id);
DELETE FROM imports  WHERE in = type::record($id) OR out = type::record($id);
DELETE FROM defines  WHERE in = type::record($id) OR out = type::record($id);
DELETE FROM inherits WHERE in = type::record($id) OR out = type::record($id);
DELETE FROM uses     WHERE in = type::record($id) OR out = type::record($id);`

	if _, err := surrealdb.Query[any](ctx, a.db, q, map[string]any{
		"id": models.NewRecordID(table, localID),
	}); err != nil {
		return fmt.Errorf("delete edges for node %s: %w", nodeID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// GraphStore — Graph traversal
// ---------------------------------------------------------------------------

// traceRow is used to unmarshal recursive traversal results.
type traceRow struct {
	ID        *models.RecordID `json:"id"`
	Name      string           `json:"name"`
	Qualified string           `json:"qualified"`
	Language  string           `json:"language"`
	FilePath  string           `json:"file_path"`
	RepoSlug  string           `json:"repo_slug"`
	// Edge information embedded from the traversal.
	CallSite  string `json:"call_site"`
	CallType  string `json:"call_type"`
	IsDynamic bool   `json:"is_dynamic"`
	Depth     int    `json:"depth"`
}

// TraceForward follows call-graph edges outward from startID up to depth hops.
func (a *SurrealAdapter) TraceForward(ctx context.Context, startID string, depth int) ([]types.TraceHop, error) {
	table, localID := splitRecordID(startID)
	if table == "" || localID == "" {
		return nil, domain.Validation(fmt.Sprintf("invalid start id: %q", startID))
	}
	if depth <= 0 {
		depth = 5
	}

	// Build a parameterised recursive CTE using SurrealDB path syntax.
	// We select each hop together with its depth level.
	q := fmt.Sprintf(`
SELECT
    id,
    name,
    qualified,
    language,
    <-calls.call_site  AS call_site,
    <-calls.call_type  AS call_type,
    <-calls.is_dynamic AS is_dynamic
FROM $start->calls{1..%d}->function;`, depth)

	results, err := surrealdb.Query[[]traceRow](ctx, a.db, q, map[string]any{
		"start": models.NewRecordID(table, localID),
	})
	if err != nil {
		return nil, fmt.Errorf("trace forward %s: %w", startID, err)
	}

	if results == nil || len(*results) == 0 {
		return nil, nil
	}

	return buildTraceHops((*results)[0].Result, "calls"), nil
}

// TraceReverse follows call-graph edges backward (callers of startID).
func (a *SurrealAdapter) TraceReverse(ctx context.Context, startID string, depth int) ([]types.TraceHop, error) {
	table, localID := splitRecordID(startID)
	if table == "" || localID == "" {
		return nil, domain.Validation(fmt.Sprintf("invalid start id: %q", startID))
	}
	if depth <= 0 {
		depth = 5
	}

	q := fmt.Sprintf(`
SELECT
    id,
    name,
    qualified,
    language,
    ->calls.call_site  AS call_site,
    ->calls.call_type  AS call_type,
    ->calls.is_dynamic AS is_dynamic
FROM $start<-calls{1..%d}<-function;`, depth)

	results, err := surrealdb.Query[[]traceRow](ctx, a.db, q, map[string]any{
		"start": models.NewRecordID(table, localID),
	})
	if err != nil {
		return nil, fmt.Errorf("trace reverse %s: %w", startID, err)
	}

	if results == nil || len(*results) == 0 {
		return nil, nil
	}

	return buildTraceHops((*results)[0].Result, "calls"), nil
}

// buildTraceHops converts flat traversal rows into a slice of TraceHop.
// SurrealDB path traversal returns results in BFS order; we build a flat
// list and assign depths by index since graph depth tracking in SurrealQL
// requires a PATHS query which varies by version. The flat representation
// is sufficient for the explain pipeline to consume.
func buildTraceHops(rows []traceRow, _ string) []types.TraceHop {
	hops := make([]types.TraceHop, 0, len(rows))
	for i, r := range rows {
		nodeID := ""
		if r.ID != nil {
			nodeID = fmt.Sprintf("%s:%v", r.ID.Table, r.ID.ID)
		}
		node := types.CodeNode{
			ID:        nodeID,
			Kind:      kindFromTable(r.ID.Table),
			Name:      r.Name,
			Qualified: r.Qualified,
			Language:  r.Language,
			FilePath:  r.FilePath,
			RepoSlug:  r.RepoSlug,
		}
		edge := types.CodeEdge{
			Kind:      types.EdgeCalls,
			FromID:    nodeID,
			CallSite:  r.CallSite,
			CallType:  r.CallType,
			IsDynamic: r.IsDynamic,
		}
		hops = append(hops, types.TraceHop{
			Depth: i + 1,
			Node:  node,
			Edge:  edge,
		})
	}
	return hops
}

// BlastRadius returns all nodes that transitively depend on targetID.
func (a *SurrealAdapter) BlastRadius(ctx context.Context, targetID string, maxDepth int) ([]types.AffectedNode, error) {
	table, localID := splitRecordID(targetID)
	if table == "" || localID == "" {
		return nil, domain.Validation(fmt.Sprintf("invalid target id: %q", targetID))
	}
	if maxDepth <= 0 {
		maxDepth = 10
	}

	// Reverse traversal: who calls the target (transitively)?
	q := fmt.Sprintf(`
SELECT id, name, qualified, language, file_path, repo_slug
FROM $target<-calls{1..%d}<-function;`, maxDepth)

	type affectedRow struct {
		ID        *models.RecordID `json:"id"`
		Name      string           `json:"name"`
		Qualified string           `json:"qualified"`
		Language  string           `json:"language"`
		FilePath  string           `json:"file_path"`
		RepoSlug  string           `json:"repo_slug"`
	}

	results, err := surrealdb.Query[[]affectedRow](ctx, a.db, q, map[string]any{
		"target": models.NewRecordID(table, localID),
	})
	if err != nil {
		return nil, fmt.Errorf("blast radius %s: %w", targetID, err)
	}

	if results == nil || len(*results) == 0 {
		return nil, nil
	}

	rows := (*results)[0].Result
	affected := make([]types.AffectedNode, 0, len(rows))
	for i, r := range rows {
		nodeID := ""
		if r.ID != nil {
			nodeID = fmt.Sprintf("%s:%v", r.ID.Table, r.ID.ID)
		}
		// Derive module from qualified name (everything before first dot).
		module := r.Qualified
		if idx := strings.IndexByte(r.Qualified, '.'); idx > 0 {
			module = r.Qualified[:idx]
		}
		affected = append(affected, types.AffectedNode{
			Node: types.CodeNode{
				ID:        nodeID,
				Kind:      kindFromTable(r.ID.Table),
				Name:      r.Name,
				Qualified: r.Qualified,
				Language:  r.Language,
				FilePath:  r.FilePath,
				RepoSlug:  r.RepoSlug,
			},
			HopCount: i + 1,
			Module:   module,
			Path:     r.FilePath,
		})
	}
	return affected, nil
}

// ---------------------------------------------------------------------------
// GraphStore — Neighborhood & data-flow queries
// ---------------------------------------------------------------------------

// neighborRow is used to unmarshal a callee/caller row with full node fields.
type neighborRow struct {
	ID        *models.RecordID `json:"id"`
	Qualified string           `json:"qualified"`
	Signature string           `json:"signature"`
	Docstring string           `json:"docstring"`
	FilePath  string           `json:"file_path"`
	StartLine int              `json:"start_line"`
	ParamName string           `json:"param_name"`
	ArgExpr   string           `json:"arg_expr"`
}

func neighborRowToNeighborNode(r neighborRow) domain.NeighborNode {
	return domain.NeighborNode{
		Qualified: r.Qualified,
		Signature: r.Signature,
		Docstring: r.Docstring,
		FilePath:  r.FilePath,
		StartLine: r.StartLine,
		ParamName: r.ParamName,
		ArgExpr:   r.ArgExpr,
	}
}

// GetNeighborhood returns the immediate graph context for nodeID: callers and
// callees (with signatures), data-flow sources/sinks, and accessed fields.
// Returns an empty Neighborhood (not an error) when no neighbors exist.
func (a *SurrealAdapter) GetNeighborhood(ctx context.Context, nodeID string) (*domain.Neighborhood, error) {
	table, localID := splitRecordID(nodeID)
	if table == "" || localID == "" {
		return nil, domain.Validation(fmt.Sprintf("invalid node id: %q", nodeID))
	}

	// Single multi-LET query fetches all neighbor categories in one round-trip.
	q := `
LET $node = type::record($table + ":" + $local_id);

LET $callees = (
    SELECT out.qualified AS qualified, out.signature AS signature,
           out.docstring AS docstring, out.file_path  AS file_path,
           out.start_line AS start_line
    FROM calls WHERE in = $node LIMIT 30 FETCH out
);
LET $callers = (
    SELECT in.qualified AS qualified, in.signature AS signature,
           in.docstring AS docstring, in.file_path  AS file_path,
           in.start_line AS start_line
    FROM calls WHERE out = $node LIMIT 30 FETCH in
);
LET $sinks = (
    SELECT out.qualified AS qualified, out.signature AS signature,
           out.file_path AS file_path, out.start_line AS start_line,
           param_name, arg_expr
    FROM data_flow WHERE in = $node LIMIT 20 FETCH out
);
LET $sources = (
    SELECT in.qualified AS qualified, in.signature AS signature,
           in.file_path AS file_path, in.start_line AS start_line,
           param_name, arg_expr
    FROM data_flow WHERE out = $node LIMIT 20 FETCH in
);
LET $reads_f  = (SELECT field_name FROM reads  WHERE in = $node);
LET $writes_f = (SELECT field_name FROM writes WHERE in = $node);

RETURN {
    callees:  $callees,
    callers:  $callers,
    sinks:    $sinks,
    sources:  $sources,
    reads:    $reads_f.field_name,
    writes:   $writes_f.field_name
};`

	type neighborhoodRow struct {
		Callees []neighborRow `json:"callees"`
		Callers []neighborRow `json:"callers"`
		Sinks   []neighborRow `json:"sinks"`
		Sources []neighborRow `json:"sources"`
		Reads   []string      `json:"reads"`
		Writes  []string      `json:"writes"`
	}

	results, err := surrealdb.Query[[]neighborhoodRow](ctx, a.db, q, map[string]any{
		"table":    table,
		"local_id": localID,
	})
	if err != nil {
		// Non-fatal: return an empty neighborhood so callers can proceed.
		return &domain.Neighborhood{}, nil //nolint:nilerr
	}
	if results == nil || len(*results) == 0 || len((*results)[0].Result) == 0 {
		return &domain.Neighborhood{}, nil
	}

	row := (*results)[0].Result[0]
	nb := &domain.Neighborhood{
		Reads:  row.Reads,
		Writes: row.Writes,
	}
	for _, r := range row.Callees {
		nb.Callees = append(nb.Callees, neighborRowToNeighborNode(r))
	}
	for _, r := range row.Callers {
		nb.Callers = append(nb.Callers, neighborRowToNeighborNode(r))
	}
	for _, r := range row.Sinks {
		nb.DataSinks = append(nb.DataSinks, neighborRowToNeighborNode(r))
	}
	for _, r := range row.Sources {
		nb.DataSources = append(nb.DataSources, neighborRowToNeighborNode(r))
	}
	return nb, nil
}

// TraceDataFlow follows data_flow edges from startID.
// direction: "forward" (sinks), "reverse" (sources), "both".
func (a *SurrealAdapter) TraceDataFlow(ctx context.Context, startID string, depth int, direction string) ([]types.TraceHop, error) {
	table, localID := splitRecordID(startID)
	if table == "" || localID == "" {
		return nil, domain.Validation(fmt.Sprintf("invalid start id: %q", startID))
	}
	if depth <= 0 {
		depth = 5
	}

	var q string
	switch direction {
	case "reverse":
		q = fmt.Sprintf(`
SELECT id, name, qualified, language, file_path, repo_slug,
       <-data_flow.call_site  AS call_site,
       <-data_flow.param_name AS call_type
FROM $start<-data_flow<-function{1..%d};`, depth)
	default: // "forward" or "both" — forward is the primary direction
		q = fmt.Sprintf(`
SELECT id, name, qualified, language, file_path, repo_slug,
       ->data_flow.call_site  AS call_site,
       ->data_flow.param_name AS call_type
FROM $start->data_flow->function{1..%d};`, depth)
	}

	results, err := surrealdb.Query[[]traceRow](ctx, a.db, q, map[string]any{
		"start": models.NewRecordID(table, localID),
	})
	if err != nil {
		return nil, fmt.Errorf("trace data flow %s: %w", startID, err)
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}

	hops := buildTraceHops((*results)[0].Result, "data_flow")
	for i := range hops {
		hops[i].Edge.Kind = types.EdgeDataFlow
	}

	// For "both", append the reverse hops as well.
	if direction == "both" {
		revQ := fmt.Sprintf(`
SELECT id, name, qualified, language, file_path, repo_slug,
       <-data_flow.call_site  AS call_site,
       <-data_flow.param_name AS call_type
FROM $start<-data_flow<-function{1..%d};`, depth)
		revResults, err := surrealdb.Query[[]traceRow](ctx, a.db, revQ, map[string]any{
			"start": models.NewRecordID(table, localID),
		})
		if err == nil && revResults != nil && len(*revResults) > 0 {
			revHops := buildTraceHops((*revResults)[0].Result, "data_flow")
			for i := range revHops {
				revHops[i].Edge.Kind = types.EdgeDataFlow
			}
			hops = append(hops, revHops...)
		}
	}

	return hops, nil
}

// ListNodeIDs returns the record IDs of all indexable nodes (function, class, file,
// module) for a given repo slug. Used by the neighborhood re-embedding pass.
func (a *SurrealAdapter) ListNodeIDs(ctx context.Context, repoSlug string) ([]string, error) {
	type idRow struct {
		ID *models.RecordID `json:"id"`
	}

	// "function" is a reserved keyword — backtick-quoted. Query each table
	// separately to avoid UNION ALL cross-statement parsing issues in SurrealDB.
	tables := []string{"`function`", "class", "file", "module"}
	params := map[string]any{
		"repo_ref": models.NewRecordID("repo", repoSlug),
	}

	var ids []string
	for _, table := range tables {
		q := fmt.Sprintf("SELECT id FROM %s WHERE repo = $repo_ref;", table)
		results, err := surrealdb.Query[[]idRow](ctx, a.db, q, params)
		if err != nil {
			return nil, fmt.Errorf("list node ids %s [%s]: %w", repoSlug, table, err)
		}
		if results == nil || len(*results) == 0 {
			continue
		}
		for _, r := range (*results)[0].Result {
			if r.ID != nil {
				ids = append(ids, fmt.Sprintf("%s:%v", r.ID.Table, r.ID.ID))
			}
		}
	}
	return ids, nil
}

// ---------------------------------------------------------------------------
// GraphStore — Batch upsert (transactional)
// ---------------------------------------------------------------------------

// UpsertFileBatch upserts all nodes and edges for a parsed file.
// Each UPSERT/RELATE statement is individually atomic in SurrealDB, so we
// avoid wrapping them in a transaction — transactions cause write conflicts
// under concurrent indexing because multiple goroutines may touch the same
// repo/file records.
func (a *SurrealAdapter) UpsertFileBatch(ctx context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error {
	for i := range nodes {
		node := &nodes[i]
		table := nodeTable(string(node.Kind))
		q := upsertNodeQuery(table)
		if q == "" {
			continue
		}
		results, err := surrealdb.Query[any](ctx, a.db, q, nodeParams(node))
		if err != nil {
			return fmt.Errorf("upsert node %s: %w", node.ID, err)
		}
		for _, r := range *results {
			if r.Status == "ERR" {
				return fmt.Errorf("upsert node %s: %v", node.ID, r.Error)
			}
		}
	}

	// Derive repo slug from the first node (all nodes in a batch share a repo).
	repoSlug := ""
	if len(nodes) > 0 {
		repoSlug = nodes[0].RepoSlug
	}

	for i := range edges {
		edge := &edges[i]
		q := relateEdgeQuery(edge.Kind)
		if q == "" {
			continue
		}
		results, err := surrealdb.Query[any](ctx, a.db, q, edgeParams(edge, repoSlug))
		if err != nil {
			return fmt.Errorf("relate edge %s->%s: %w", edge.FromID, edge.ToID, err)
		}
		for _, r := range *results {
			if r.Status == "ERR" {
				return fmt.Errorf("relate edge %s->%s: %v", edge.FromID, edge.ToID, r.Error)
			}
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// GraphStore — Repository CRUD
// ---------------------------------------------------------------------------

// UpsertRepo creates or updates a repository record.
func (a *SurrealAdapter) UpsertRepo(ctx context.Context, repo *types.Repo) error {
	if repo == nil {
		return domain.Validation("repo is nil")
	}
	const q = `
UPSERT type::record($id) CONTENT {
    slug:            $slug,
    path:            $path,
    remote_url:      $remote_url,
    default_branch:  $default_branch,
    languages:       $languages,
    last_commit:     $last_commit,
    last_indexed_at: $last_indexed_at
};`

	// option<datetime> fields must be NONE (not NULL) when absent.
	var lastIndexedAt any = models.None
	if repo.LastIndexedAt != nil {
		lastIndexedAt = repo.LastIndexedAt
	}
	// array<string> must not be nil — use empty slice.
	languages := repo.Languages
	if languages == nil {
		languages = []string{}
	}

	results, err := surrealdb.Query[any](ctx, a.db, q, map[string]any{
		"id":              models.NewRecordID("repo", repo.Slug),
		"slug":            repo.Slug,
		"path":            repo.Path,
		"remote_url":      repo.RemoteURL,
		"default_branch":  repo.DefaultBranch,
		"languages":       languages,
		"last_commit":     repo.LastCommit,
		"last_indexed_at": lastIndexedAt,
	})
	if err != nil {
		return fmt.Errorf("upsert repo %s: %w", repo.Slug, err)
	}
	for i, r := range *results {
		if r.Status == "ERR" {
			return fmt.Errorf("upsert repo %s stmt %d: %v", repo.Slug, i, r.Error)
		}
	}
	return nil
}

// GetRepo retrieves a repository by its slug.
func (a *SurrealAdapter) GetRepo(ctx context.Context, slug string) (*types.Repo, error) {
	const q = `SELECT * FROM type::record($id);`
	results, err := surrealdb.Query[[]repoRow](ctx, a.db, q, map[string]any{
		"id": models.NewRecordID("repo", slug),
	})
	if err != nil {
		return nil, fmt.Errorf("get repo %s: %w", slug, err)
	}
	if results == nil || len(*results) == 0 || len((*results)[0].Result) == 0 {
		return nil, domain.NotFound(fmt.Sprintf("repo %s not found", slug))
	}

	r := (*results)[0].Result[0]
	return repoRowToRepo(r), nil
}

// ListRepos returns all repositories ordered by slug.
func (a *SurrealAdapter) ListRepos(ctx context.Context) ([]types.Repo, error) {
	const q = `SELECT * FROM repo ORDER BY slug ASC;`
	results, err := surrealdb.Query[[]repoRow](ctx, a.db, q, nil)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}

	rows := (*results)[0].Result
	repos := make([]types.Repo, 0, len(rows))
	for _, r := range rows {
		repos = append(repos, *repoRowToRepo(r))
	}
	return repos, nil
}

func repoRowToRepo(r repoRow) *types.Repo {
	return &types.Repo{
		Slug:          r.Slug,
		Path:          r.Path,
		RemoteURL:     r.RemoteURL,
		DefaultBranch: r.DefaultBranch,
		Languages:     r.Languages,
		LastCommit:    r.LastCommit,
		LastIndexedAt: r.LastIndexedAt,
	}
}
