package types

// GraphNode is any entity in the code knowledge graph.
// Label classifies it ("function", "class", "file", "module", and future labels).
// Props carries all label-specific data as key-value pairs.
type GraphNode struct {
	ID        string         `json:"id"`
	Label     string         `json:"label"`
	Qualified string         `json:"qualified"`
	Name      string         `json:"name"`
	FilePath  string         `json:"file_path"`
	RepoSlug  string         `json:"repo_slug"`
	Props     map[string]any `json:"props,omitempty"`
	Embedding []float32      `json:"embedding,omitempty"`
}

// GraphEdge is a directed labeled relationship between two nodes.
// Label classifies it ("calls", "data_flow", "reads", and future labels).
// Props carries all label-specific metadata.
type GraphEdge struct {
	Label  string         `json:"label"`
	FromID string         `json:"from_id"`
	ToID   string         `json:"to_id"`
	Props  map[string]any `json:"props,omitempty"`
}

// ── Convention label constants ────────────────────────────────────────────
// These are NOT enums — just discoverable strings. New labels don't need
// to be added here. They exist for IDE autocomplete and grep-ability.

// Node labels
const (
	LabelFunction = "function"
	LabelClass    = "class"
	LabelFile     = "file"
	LabelModule   = "module"
)

// Edge labels
const (
	LabelCalls       = "calls"
	LabelImports     = "imports"
	LabelDefines     = "defines"
	LabelInherits    = "inherits"
	LabelUses        = "uses"
	LabelDataFlow    = "data_flow"
	LabelReads       = "reads"
	LabelWrites      = "writes"
	LabelRoute       = "route"
	LabelControlFlow = "control_flow"
	LabelDataDep     = "data_dep"
)

// ── Conversion helpers (CodeNode ↔ GraphNode) ────────────────────────────

// CodeNodeToGraph converts a legacy CodeNode to a GraphNode.
func CodeNodeToGraph(n *CodeNode) GraphNode {
	props := make(map[string]any, 16)
	setNonEmpty(props, "language", n.Language)
	setNonEmpty(props, "signature", n.Signature)
	setNonEmpty(props, "docstring", n.Docstring)
	setNonEmpty(props, "body", n.Body)
	setNonEmpty(props, "visibility", n.Visibility)
	setNonEmpty(props, "content_hash", n.ContentHash)
	setNonEmpty(props, "summary", n.Summary)
	setNonEmpty(props, "introduced_commit", n.IntroducedCommit)
	setNonEmpty(props, "last_modified_commit", n.LastModifiedCommit)
	if n.StartLine != 0 {
		props["start_line"] = n.StartLine
	}
	if n.EndLine != 0 {
		props["end_line"] = n.EndLine
	}
	if len(n.Concepts) > 0 {
		props["concepts"] = n.Concepts
	}
	if n.IntroducedAt != nil {
		props["introduced_at"] = n.IntroducedAt
	}
	if n.LastModifiedAt != nil {
		props["last_modified_at"] = n.LastModifiedAt
	}

	return GraphNode{
		ID: n.ID, Label: string(n.Kind), Qualified: n.Qualified,
		Name: n.Name, FilePath: n.FilePath, RepoSlug: n.RepoSlug,
		Props: props, Embedding: n.Embedding,
	}
}

// GraphNodeToCode converts a GraphNode back to a legacy CodeNode.
func GraphNodeToCode(g *GraphNode) CodeNode {
	return CodeNode{
		ID: g.ID, Kind: NodeKind(g.Label), Qualified: g.Qualified,
		Name: g.Name, FilePath: g.FilePath, RepoSlug: g.RepoSlug,
		Language:           stringProp(g.Props, "language"),
		Signature:          stringProp(g.Props, "signature"),
		Docstring:          stringProp(g.Props, "docstring"),
		Body:               stringProp(g.Props, "body"),
		Visibility:         stringProp(g.Props, "visibility"),
		ContentHash:        stringProp(g.Props, "content_hash"),
		Summary:            stringProp(g.Props, "summary"),
		Concepts:           stringSliceProp(g.Props, "concepts"),
		StartLine:          intProp(g.Props, "start_line"),
		EndLine:            intProp(g.Props, "end_line"),
		IntroducedCommit:   stringProp(g.Props, "introduced_commit"),
		LastModifiedCommit: stringProp(g.Props, "last_modified_commit"),
		Embedding:          g.Embedding,
	}
}

// CodeEdgeToGraph converts a legacy CodeEdge to a GraphEdge.
func CodeEdgeToGraph(e *CodeEdge) GraphEdge {
	props := make(map[string]any, len(e.Metadata)+4)
	if e.CallSite != "" {
		props["call_site"] = e.CallSite
	}
	if e.CallType != "" {
		props["call_type"] = e.CallType
	}
	if e.IsDynamic {
		props["is_dynamic"] = true
	}
	for k, v := range e.Metadata {
		props[k] = v
	}
	if e.IntroducedCommit != "" {
		props["introduced_commit"] = e.IntroducedCommit
	}
	if e.RemovedCommit != "" {
		props["removed_commit"] = e.RemovedCommit
	}
	return GraphEdge{
		Label: string(e.Kind), FromID: e.FromID, ToID: e.ToID, Props: props,
	}
}

// GraphEdgeToCode converts a GraphEdge back to a legacy CodeEdge.
func GraphEdgeToCode(g *GraphEdge) CodeEdge {
	meta := make(map[string]string, len(g.Props))
	for k, v := range g.Props {
		switch k {
		case "call_site", "call_type", "is_dynamic",
			"introduced_commit", "introduced_at", "removed_commit":
			continue // handled as named fields
		default:
			if s, ok := v.(string); ok {
				meta[k] = s
			}
		}
	}
	return CodeEdge{
		Kind:             EdgeKind(g.Label),
		FromID:           g.FromID,
		ToID:             g.ToID,
		CallSite:         stringProp(g.Props, "call_site"),
		CallType:         stringProp(g.Props, "call_type"),
		IsDynamic:        boolProp(g.Props, "is_dynamic"),
		IntroducedCommit: stringProp(g.Props, "introduced_commit"),
		RemovedCommit:    stringProp(g.Props, "removed_commit"),
		Metadata:         meta,
	}
}

// ── Typed property accessors ─────────────────────────────────────────────

func setNonEmpty(m map[string]any, key, val string) {
	if val != "" {
		m[key] = val
	}
}

func stringProp(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func intProp(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	}
	return 0
}

func boolProp(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	b, _ := m[key].(bool)
	return b
}

func stringSliceProp(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	switch v := m[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
