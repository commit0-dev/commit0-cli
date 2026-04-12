package surreal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	surrealdb "github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"

	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/server/internal/infra/retry"
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
	Summary     string           `json:"summary"`
	Concepts    []string         `json:"concepts"`
	Embedding          []float32        `json:"embedding"`
	EndLine            int              `json:"end_line"`
	StartLine          int              `json:"start_line"`
	Centrality         int              `json:"centrality"`
	IntroducedCommit   string           `json:"introduced_commit"`
	IntroducedAt       *time.Time       `json:"introduced_at"`
	LastModifiedCommit string           `json:"last_modified_commit"`
	LastModifiedAt     *time.Time       `json:"last_modified_at"`
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
		Summary:            r.Summary,
		Concepts:           r.Concepts,
		ContentHash:        r.ContentHash,
		Embedding:          r.Embedding,
		Visibility:         r.Visibility,
		IntroducedCommit:   r.IntroducedCommit,
		IntroducedAt:       r.IntroducedAt,
		LastModifiedCommit: r.LastModifiedCommit,
		LastModifiedAt:     r.LastModifiedAt,
	}
}

// nodeParams builds the SurrealQL variable map for upsert operations.
func nodeParams(node *types.CodeNode) map[string]any {
	table := nodeTable(string(node.Kind))
	_, localID := splitRecordID(node.ID)
	if localID == "" {
		localID = node.Qualified
	}

	// Pass repo/file as typed models.RecordID objects so the Go SDK serializes
	// them via CBOR correctly — especially when slugs or paths contain '/' which
	// type::record(string) may mis-parse as a division operator.
	repoRef := models.NewRecordID("repo", node.RepoSlug)
	fileRef := models.NewRecordID("file", node.FilePath)

	// SurrealDB 3.0 strict mode: option<T> fields need NONE (not NULL)
	// when the value is absent. The Go SDK's models.None serializes correctly.
	var embedding any = models.None
	if len(node.Embedding) > 0 {
		embedding = node.Embedding
	}

	var docstring any = models.None
	if node.Docstring != "" {
		docstring = node.Docstring
	}

	var summary any = models.None
	if node.Summary != "" {
		summary = node.Summary
	}

	var concepts any = models.None
	if len(node.Concepts) > 0 {
		concepts = node.Concepts
	}

	// Content map uses schema field names directly (repo, file — not repo_ref, file_ref).
	// record_id is separate (used in UPSERT type::record($record_id)).
	return map[string]any{
		"record_id":    models.NewRecordID(table, localID),
		"name":         node.Name,
		"qualified":    node.Qualified,
		"path":         node.FilePath,
		"file_path":    node.FilePath,
		"repo_slug":    node.RepoSlug,
		"language":     node.Language,
		"start_line":   node.StartLine,
		"end_line":     node.EndLine,
		"signature":    node.Signature,
		"docstring":    docstring,
		"body":         node.Body,
		"summary":      summary,
		"concepts":     concepts,
		"content_hash": node.ContentHash,
		"embedding":    embedding,
		"visibility":   defaultVisibility(node.Visibility),
		"repo":         repoRef,
		"file":         fileRef,
	}
}

func defaultVisibility(v string) string {
	if v == "" {
		return "public"
	}
	return v
}

// genericUpsertNodeQuery returns a single UPSERT template for ANY node label.
// Replaces the old 4-case upsertNodeQuery switch (OpenCodeGraph §5.1).
// The adapter still routes to the correct table by label; SurrealDB schema
// on SCHEMAFULL tables ignores unknown fields, so extra props are safe.
func genericUpsertNodeQuery() string {
	return `UPSERT type::record($record_id) CONTENT $props;`
}

// genericRelateQuery builds a single RELATE statement for ANY edge label.
// Replaces the old 8-case relateEdgeQuery switch (OpenCodeGraph §7.2).
// SurrealDB auto-creates SCHEMALESS tables for unknown labels.
func genericRelateQuery(label string) string {
	return fmt.Sprintf("RELATE $from->%s->$to CONTENT $props;", label)
}

// genericEdgeParams builds the props map for a generic RELATE.
// All edge-type-specific fields go into one map — no per-type switching.
func genericEdgeParams(edge *types.CodeEdge, repoSlug string) map[string]any {
	fromTable, fromID := splitRecordID(edge.FromID)
	toTable, toID := splitRecordID(edge.ToID)
	if fromTable == "" {
		fromTable = "function"
	}
	if toTable == "" {
		toTable = "function"
	}

	// Build props from all sources: fixed fields + metadata
	props := make(map[string]any, len(edge.Metadata)+6)
	if edge.CallSite != "" {
		props["call_site"] = edge.CallSite
	}
	if edge.CallType != "" {
		props["call_type"] = edge.CallType
	} else if edge.Kind == types.EdgeCalls {
		props["call_type"] = "direct"
	}
	if edge.IsDynamic {
		props["is_dynamic"] = true
	}
	// With SCHEMALESS edge tables (OpenCodeGraph), repo is stored as plain string.
	// No record<repo> reference needed — simpler and works for all edge labels.
	if repoSlug != "" {
		props["repo"] = repoSlug
	}
	// Copy metadata keys, coercing known numeric fields from string→int
	// (Metadata is map[string]string but SCHEMAFULL tables expect typed fields)
	numericFields := map[string]bool{
		"from_line": true, "to_line": true, "def_line": true,
		"use_line": true, "mutation_line": true, "start_line": true,
	}
	for k, v := range edge.Metadata {
		if numericFields[k] && v != "" {
			var n int
			for _, c := range v {
				if c >= '0' && c <= '9' {
					n = n*10 + int(c-'0')
				}
			}
			props[k] = n
		} else {
			props[k] = v
		}
	}

	return map[string]any{
		"from":  models.NewRecordID(fromTable, fromID),
		"to":    models.NewRecordID(toTable, toID),
		"props": props,
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
	if table == "" {
		return domain.Validation(fmt.Sprintf("unknown node kind: %s", node.Kind))
	}
	q := genericUpsertNodeQuery()
	params := nodeParams(node)
	// Wrap the params in a $props + $record_id structure for the generic query
	results, err := surrealdb.Query[any](ctx, a.writeDB(), q, map[string]any{
		"record_id": params["record_id"],
		"props":     params,
	})
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
	results, err := surrealdb.Query[[]nodeRow](ctx, a.readDB(), q, map[string]any{
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
		results, err := surrealdb.Query[[]nodeRow](ctx, a.readDB(), q, map[string]any{
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
			results, err := surrealdb.Query[[]nodeRow](ctx, a.readDB(), nameQ, map[string]any{
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
	if _, err := surrealdb.Query[any](ctx, a.writeDB(), q, map[string]any{
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
// DeleteNodesByFile removes all nodes (and their edges) for a specific file in a repo.
func (a *SurrealAdapter) DeleteNodesByFile(ctx context.Context, repoSlug, filePath string) error {
	tables := []string{"`function`", "class", "file", "module"}
	params := map[string]any{
		"repo_ref": models.NewRecordID("repo", repoSlug),
		"fpath":    filePath,
	}
	for _, table := range tables {
		q := fmt.Sprintf("DELETE FROM %s WHERE repo = $repo_ref AND file_path = $fpath;", table)
		if _, err := surrealdb.Query[any](ctx, a.writeDB(), q, params); err != nil {
			return fmt.Errorf("delete nodes by file %s [%s]: %w", filePath, table, err)
		}
	}
	return nil
}

func (a *SurrealAdapter) DeleteNodesByRepo(ctx context.Context, repo string) error {
	const q = `DELETE type::record($repo_id);`
	results, err := surrealdb.Query[any](ctx, a.writeDB(), q, map[string]any{
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
	if edge.Kind == "" {
		return domain.Validation("edge kind is empty")
	}
	q := genericRelateQuery(string(edge.Kind))
	repoSlug := ""
	results, err := surrealdb.Query[any](ctx, a.writeDB(), q, genericEdgeParams(edge, repoSlug))
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

	if _, err := surrealdb.Query[any](ctx, a.writeDB(), q, map[string]any{
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

// TraverseGraph follows edges of the specified labels from startID.
// This is the label-parameterized traversal from OpenCodeGraph (OPEN_CODE_GRAPH.md §7.3).
// For multiple labels, runs parallel per-label queries and merges results.
func (a *SurrealAdapter) TraverseGraph(ctx context.Context, startID string, edgeLabels []string, direction string, maxDepth int) ([]types.TraceHop, error) {
	table, localID := splitRecordID(startID)
	if table == "" || localID == "" {
		return nil, domain.Validation(fmt.Sprintf("invalid start id: %q", startID))
	}
	if maxDepth <= 0 {
		maxDepth = 5
	}
	if len(edgeLabels) == 0 {
		edgeLabels = []string{"calls"}
	}

	startRef := models.NewRecordID(table, localID)
	startPtr := &startRef

	// Single label: one query
	if len(edgeLabels) == 1 {
		return a.traverseSingleLabel(ctx, startPtr, edgeLabels[0], direction, maxDepth)
	}

	// Multiple labels: parallel queries, merge + dedup
	g, gCtx := errgroup.WithContext(ctx)
	results := make([][]types.TraceHop, len(edgeLabels))

	for i, label := range edgeLabels {
		g.Go(func() error {
			hops, err := a.traverseSingleLabel(gCtx, startPtr, label, direction, maxDepth)
			if err != nil {
				a.log.Warn("traverse label failed", "label", label, "err", err)
				return nil // non-fatal per label
			}
			results[i] = hops
			return nil
		})
	}
	_ = g.Wait()

	// Merge and dedup by qualified name
	seen := make(map[string]bool)
	var merged []types.TraceHop
	for _, hops := range results {
		for _, h := range hops {
			if h.Node.Qualified != "" && !seen[h.Node.Qualified] {
				seen[h.Node.Qualified] = true
				merged = append(merged, h)
			}
		}
	}
	return merged, nil
}

// traverseSingleLabel runs a recursive graph traversal for one edge label.
// Uses a per-query timeout (30s) to prevent dangling edges from blocking the
// entire request — some labels (data_flow) have many unresolved targets that
// cause SurrealDB recursive traversal to hang.
func (a *SurrealAdapter) traverseSingleLabel(ctx context.Context, start *models.RecordID, label, direction string, maxDepth int) ([]types.TraceHop, error) {
	var arrow string
	switch direction {
	case "reverse":
		arrow = fmt.Sprintf("<-%s<-function", label)
	default: // "forward"
		arrow = fmt.Sprintf("->%s->function", label)
	}

	q := fmt.Sprintf(`
SELECT id, name, qualified, language, file_path, repo_slug
FROM $start.{1..%d}(%s);`, maxDepth, arrow)

	// Per-label timeout prevents one slow label from killing the whole request
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	results, err := surrealdb.Query[[]traceRow](queryCtx, a.readDB(), q, map[string]any{
		"start": start,
	})
	if err != nil {
		return nil, err
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	return buildTraceHops((*results)[0].Result, label), nil
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

// BlastRadius is deprecated — use TraverseGraph with direction="reverse".
// Kept only for test stub compat.


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
	ParamName string           `json:"param_name"`
	ArgExpr   string           `json:"arg_expr"`
	FieldName string           `json:"field_name"`
	StartLine int              `json:"start_line"`
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
	// Use separate queries to avoid FETCH/dereference on dangling record IDs.
	// Dangling edges (unresolved targets) cause SurrealDB errors when accessed
	// via out.qualified because the target record doesn't exist.
	nb := &domain.Neighborhood{}
	nodeRef := models.NewRecordID(table, localID)
	params := map[string]any{"node": nodeRef}

	// Callees: functions this node calls (outbound calls edges where target exists)
	if rows, err := a.neighborQuery(ctx, "SELECT out AS id, out.qualified AS qualified, out.signature AS signature, out.docstring AS docstring, out.file_path AS file_path, out.start_line AS start_line FROM calls WHERE in = $node AND out.qualified != NONE LIMIT 30;", params); err == nil {
		for _, r := range rows {
			nb.Callees = append(nb.Callees, neighborRowToNeighborNode(r))
		}
	}
	// Callers: functions that call this node (inbound calls edges)
	if rows, err := a.neighborQuery(ctx, "SELECT in AS id, in.qualified AS qualified, in.signature AS signature, in.docstring AS docstring, in.file_path AS file_path, in.start_line AS start_line FROM calls WHERE out = $node AND in.qualified != NONE LIMIT 30;", params); err == nil {
		for _, r := range rows {
			nb.Callers = append(nb.Callers, neighborRowToNeighborNode(r))
		}
	}
	// Data sinks: functions that receive data from this node
	if rows, err := a.neighborQuery(ctx, "SELECT out AS id, out.qualified AS qualified, out.signature AS signature, out.file_path AS file_path, out.start_line AS start_line, param_name, arg_expr FROM data_flow WHERE in = $node AND out.qualified != NONE LIMIT 20;", params); err == nil {
		for _, r := range rows {
			nb.DataSinks = append(nb.DataSinks, neighborRowToNeighborNode(r))
		}
	}
	// Data sources: functions whose data flows into this node
	if rows, err := a.neighborQuery(ctx, "SELECT in AS id, in.qualified AS qualified, in.signature AS signature, in.file_path AS file_path, in.start_line AS start_line, param_name, arg_expr FROM data_flow WHERE out = $node AND in.qualified != NONE LIMIT 20;", params); err == nil {
		for _, r := range rows {
			nb.DataSources = append(nb.DataSources, neighborRowToNeighborNode(r))
		}
	}
	// Reads/writes
	if rows, err := a.neighborQuery(ctx, "SELECT field_name FROM reads WHERE in = $node;", params); err == nil {
		for _, r := range rows {
			if r.FieldName != "" {
				nb.Reads = append(nb.Reads, r.FieldName)
			}
		}
	}
	if rows, err := a.neighborQuery(ctx, "SELECT field_name FROM writes WHERE in = $node;", params); err == nil {
		for _, r := range rows {
			if r.FieldName != "" {
				nb.Writes = append(nb.Writes, r.FieldName)
			}
		}
	}
	return nb, nil
}

// neighborQuery runs a single neighborhood sub-query and returns the result rows.
func (a *SurrealAdapter) neighborQuery(ctx context.Context, q string, params map[string]any) ([]neighborRow, error) {
	results, err := surrealdb.Query[[]neighborRow](ctx, a.readDB(), q, params)
	if err != nil {
		return nil, err
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	return (*results)[0].Result, nil
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
SELECT id, name, qualified, language, file_path, repo_slug
FROM $start.{1..%d}(<-data_flow<-function);`, depth)
	default: // "forward" or "both" — forward is the primary direction
		q = fmt.Sprintf(`
SELECT id, name, qualified, language, file_path, repo_slug
FROM $start.{1..%d}(->data_flow->function);`, depth)
	}

	results, err := surrealdb.Query[[]traceRow](ctx, a.readDB(), q, map[string]any{
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
SELECT id, name, qualified, language, file_path, repo_slug
FROM $start.{1..%d}(<-data_flow<-function);`, depth)
		revResults, err := surrealdb.Query[[]traceRow](ctx, a.readDB(), revQ, map[string]any{
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
		results, err := surrealdb.Query[[]idRow](ctx, a.readDB(), q, params)
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

// ListFilePaths returns distinct file paths for all nodes in a repo (lightweight query).
// Used by cleanupStaleNodes to check which files still exist on disk.
func (a *SurrealAdapter) ListFilePaths(ctx context.Context, repoSlug string) ([]string, error) {
	// SurrealDB: use GROUP BY for distinct values, or just SELECT file_path
	// and deduplicate client-side (simpler, avoids SQL dialect issues).
	q := `SELECT file_path FROM function WHERE repo = $repo_ref;
	SELECT file_path FROM class WHERE repo = $repo_ref;
	SELECT path AS file_path FROM file WHERE repo = $repo_ref;`

	type row struct {
		FilePath string `json:"file_path"`
	}

	results, err := surrealdb.Query[[]row](ctx, a.readDB(), q, map[string]any{
		"repo_ref": models.NewRecordID("repo", repoSlug),
	})
	if err != nil {
		return nil, fmt.Errorf("list file paths %s: %w", repoSlug, err)
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}

	seen := make(map[string]bool)
	var paths []string
	for _, res := range *results {
		for _, r := range res.Result {
			if r.FilePath != "" && !seen[r.FilePath] {
				seen[r.FilePath] = true
				paths = append(paths, r.FilePath)
			}
		}
	}
	return paths, nil
}

// ListAllNodes returns all nodes (function, class, file, module) for a repo with full data.
// Used by GraphExporter for building sync bundles.
func (a *SurrealAdapter) ListAllNodes(ctx context.Context, repoSlug string) ([]types.CodeNode, error) {
	tables := []string{"`function`", "class", "file", "module"}
	params := map[string]any{
		"repo_ref": models.NewRecordID("repo", repoSlug),
	}

	var nodes []types.CodeNode
	for _, table := range tables {
		q := fmt.Sprintf("SELECT * FROM %s WHERE repo = $repo_ref;", table)
		results, err := surrealdb.Query[[]nodeRow](ctx, a.readDB(), q, params)
		if err != nil {
			return nil, fmt.Errorf("list all nodes %s [%s]: %w", repoSlug, table, err)
		}
		if results == nil || len(*results) == 0 {
			continue
		}
		kind := kindFromTable(strings.Trim(table, "`"))
		for _, r := range (*results)[0].Result {
			nodes = append(nodes, rowToCodeNode(r, kind))
		}
	}
	return nodes, nil
}

// ListAllEdges returns all edges for a repo. Used by GraphExporter for building sync bundles.
func (a *SurrealAdapter) ListAllEdges(ctx context.Context, repoSlug string) ([]types.CodeEdge, error) {
	type edgeRow struct {
		In               *models.RecordID  `json:"in"`
		Out              *models.RecordID  `json:"out"`
		CallSite         string            `json:"call_site"`
		CallType         string            `json:"call_type"`
		IsDynamic        bool              `json:"is_dynamic"`
		ParamName        string            `json:"param_name"`
		ArgExpr          string            `json:"arg_expr"`
		ArgType          string            `json:"arg_type"`
		FieldName        string            `json:"field_name"`
		IntroducedCommit string            `json:"introduced_commit"`
		IntroducedAt     *string           `json:"introduced_at"`
		RemovedCommit    string            `json:"removed_commit"`
	}

	// With SCHEMALESS edge tables, all edges store repo as a plain string prop.
	// Tables that have repo: filter directly. Others: filter via source node.
	tablesWithRepo := map[string]bool{"calls": true, "data_flow": true, "reads": true, "writes": true}
	edgeTables := []string{"calls", "imports", "defines", "inherits", "uses", "data_flow", "reads", "writes"}
	params := map[string]any{"repo": repoSlug}

	var edges []types.CodeEdge
	for _, table := range edgeTables {
		var q string
		if tablesWithRepo[table] {
			q = fmt.Sprintf("SELECT * FROM %s WHERE repo = $repo;", table)
		} else {
			q = fmt.Sprintf("SELECT * FROM %s WHERE in.repo_slug = $repo;", table)
		}
		results, err := surrealdb.Query[[]edgeRow](ctx, a.readDB(), q, params)
		if err != nil {
			return nil, fmt.Errorf("list all edges %s [%s]: %w", repoSlug, table, err)
		}
		if results == nil || len(*results) == 0 {
			continue
		}
		kind := types.EdgeKind(table)
		for _, r := range (*results)[0].Result {
			fromID, toID := "", ""
			if r.In != nil {
				fromID = fmt.Sprintf("%s:%s", r.In.Table, r.In.ID)
			}
			if r.Out != nil {
				toID = fmt.Sprintf("%s:%s", r.Out.Table, r.Out.ID)
			}
			meta := make(map[string]string)
			if r.ParamName != "" {
				meta["param_name"] = r.ParamName
			}
			if r.ArgExpr != "" {
				meta["arg_expr"] = r.ArgExpr
			}
			if r.ArgType != "" {
				meta["arg_type"] = r.ArgType
			}
			if r.FieldName != "" {
				meta["field_name"] = r.FieldName
			}
			edges = append(edges, types.CodeEdge{
				Kind:             kind,
				FromID:           fromID,
				ToID:             toID,
				CallSite:         r.CallSite,
				CallType:         r.CallType,
				IsDynamic:        r.IsDynamic,
				Metadata:         meta,
				IntroducedCommit: r.IntroducedCommit,
				RemovedCommit:    r.RemovedCommit,
			})
		}
	}
	return edges, nil
}

// ListNodesByFile returns all function and class nodes defined in a specific file.
func (a *SurrealAdapter) ListNodesByFile(ctx context.Context, repoSlug, filePath string) ([]types.CodeNode, error) {
	params := map[string]any{
		"repo_ref": models.NewRecordID("repo", repoSlug),
		"fpath":    filePath,
	}

	var nodes []types.CodeNode
	tables := []string{"`function`", "class"}
	for _, table := range tables {
		q := fmt.Sprintf("SELECT * FROM %s WHERE repo = $repo_ref AND file_path = $fpath;", table)
		results, err := surrealdb.Query[[]nodeRow](ctx, a.readDB(), q, params)
		if err != nil {
			return nil, fmt.Errorf("list nodes by file %s [%s]: %w", filePath, table, err)
		}
		if results == nil || len(*results) == 0 {
			continue
		}
		kind := kindFromTable(strings.Trim(table, "`"))
		for _, r := range (*results)[0].Result {
			nodes = append(nodes, rowToCodeNode(r, kind))
		}
	}
	return nodes, nil
}

// ListRoutes returns all route edges for a repo.
func (a *SurrealAdapter) ListRoutes(ctx context.Context, repoSlug string) ([]types.CodeEdge, error) {
	type routeRow struct {
		In          *models.RecordID `json:"in"`
		Out         *models.RecordID `json:"out"`
		HTTPMethod  string           `json:"http_method"`
		HTTPPath    string           `json:"http_path"`
		Middleware  any              `json:"middleware"` // string or []string depending on SCHEMALESS storage
		GroupPrefix string           `json:"group_prefix"`
	}

	// With SCHEMALESS edge tables, repo is stored as a plain string, not record<repo>
	q := `SELECT * FROM route WHERE repo = $repo;`
	params := map[string]any{"repo": repoSlug}

	results, err := surrealdb.Query[[]routeRow](ctx, a.readDB(), q, params)
	if err != nil {
		return nil, fmt.Errorf("list routes: %w", err)
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}

	var edges []types.CodeEdge
	for _, r := range (*results)[0].Result {
		meta := map[string]string{
			"http_method": r.HTTPMethod,
			"http_path":   r.HTTPPath,
		}
		if r.GroupPrefix != "" {
			meta["group_prefix"] = r.GroupPrefix
		}
		switch mw := r.Middleware.(type) {
		case string:
			if mw != "" {
				meta["middleware"] = mw
			}
		case []any:
			parts := make([]string, 0, len(mw))
			for _, v := range mw {
				if s, ok := v.(string); ok {
					parts = append(parts, s)
				}
			}
			if len(parts) > 0 {
				meta["middleware"] = strings.Join(parts, ",")
			}
		}

		fromID := ""
		if r.In != nil {
			fromID = fmt.Sprintf("%s:%s", r.In.Table, r.In.ID)
		}
		toID := ""
		if r.Out != nil {
			toID = fmt.Sprintf("%s:%s", r.Out.Table, r.Out.ID)
		}

		edges = append(edges, types.CodeEdge{
			Kind:     types.EdgeRoute,
			FromID:   fromID,
			ToID:     toID,
			Metadata: meta,
		})
	}
	return edges, nil
}

// UpdateRepoIndexedAt sets last_indexed_at using MERGE to avoid wiping other fields.
func (a *SurrealAdapter) UpdateRepoIndexedAt(ctx context.Context, slug string, t time.Time) error {
	const q = `UPDATE type::record($id) MERGE { last_indexed_at: $ts };`
	_, err := surrealdb.Query[any](ctx, a.writeDB(), q, map[string]any{
		"id": models.NewRecordID("repo", slug),
		"ts": t,
	})
	if err != nil {
		return fmt.Errorf("update repo indexed_at %s: %w", slug, err)
	}
	return nil
}

// FindRepoByRemoteURL finds a repo by its normalized remote URL.
func (a *SurrealAdapter) FindRepoByRemoteURL(ctx context.Context, remoteURL string) (*types.Repo, error) {
	const q = `SELECT * FROM repo WHERE remote_url = $url LIMIT 1;`
	results, err := surrealdb.Query[[]repoRow](ctx, a.readDB(), q, map[string]any{"url": remoteURL})
	if err != nil {
		return nil, fmt.Errorf("find repo by remote url: %w", err)
	}
	if results == nil || len(*results) == 0 || len((*results)[0].Result) == 0 {
		return nil, nil // not found — not an error
	}
	r := (*results)[0].Result[0]
	return &types.Repo{
		Slug:          r.Slug,
		Path:          r.Path,
		RemoteURL:     r.RemoteURL,
		DefaultBranch: r.DefaultBranch,
		LastCommit:    r.LastCommit,
		Languages:     r.Languages,
		LastIndexedAt: r.LastIndexedAt,
		CreatedAt:     r.CreatedAt,
	}, nil
}

// ListNodesByConcepts returns nodes whose concepts array overlaps with the given tags.
func (a *SurrealAdapter) ListNodesByConcepts(ctx context.Context, repoSlug string, concepts []string, limit int) ([]types.CodeNode, error) {
	if len(concepts) == 0 || limit <= 0 {
		return nil, nil
	}
	params := map[string]any{
		"repo_ref": models.NewRecordID("repo", repoSlug),
		"concepts": concepts,
		"lim":      limit,
	}

	var nodes []types.CodeNode
	tables := []string{"`function`", "class"}
	for _, table := range tables {
		q := fmt.Sprintf("SELECT * FROM %s WHERE repo = $repo_ref AND concepts CONTAINSANY $concepts LIMIT $lim;", table)
		results, err := surrealdb.Query[[]nodeRow](ctx, a.writeDB(), q, params)
		if err != nil {
			continue // non-fatal
		}
		if results == nil || len(*results) == 0 {
			continue
		}
		kind := kindFromTable(strings.Trim(table, "`"))
		for _, r := range (*results)[0].Result {
			nodes = append(nodes, rowToCodeNode(r, kind))
		}
	}
	return nodes, nil
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
		if table == "" {
			continue
		}
		q := genericUpsertNodeQuery()
		params := nodeParams(node)
		err := retry.WithRetry(ctx, 3, func() error {
			results, err := surrealdb.Query[any](ctx, a.writeDB(), q, map[string]any{
				"record_id": params["record_id"],
				"props":     params,
			})
			if err != nil {
				return classifySurrealError(err)
			}
			for _, r := range *results {
				if r.Status == "ERR" {
					return classifySurrealError(fmt.Errorf("%v", r.Error))
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("upsert node %s: %w", node.ID, err)
		}
	}

	// Derive repo slug from the first node (all nodes in a batch share a repo).
	repoSlug := ""
	if len(nodes) > 0 {
		repoSlug = nodes[0].RepoSlug
	}

	for i := range edges {
		edge := &edges[i]
		if edge.Kind == "" {
			continue
		}
		q := genericRelateQuery(string(edge.Kind))
		results, err := surrealdb.Query[any](ctx, a.writeDB(), q, genericEdgeParams(edge, repoSlug))
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

	results, err := surrealdb.Query[any](ctx, a.writeDB(), q, map[string]any{
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
	results, err := surrealdb.Query[[]repoRow](ctx, a.readDB(), q, map[string]any{
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
	results, err := surrealdb.Query[[]repoRow](ctx, a.readDB(), q, nil)
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

// classifySurrealError wraps SurrealDB errors into domain error types so the
// retry layer can distinguish transient failures from permanent ones.
// Transaction write conflicts are common under concurrent upsert workloads and
// are safe to retry with backoff.
func classifySurrealError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "Transaction conflict") ||
		strings.Contains(msg, "write conflict") {
		return domain.Conflict(msg)
	}
	return err
}
