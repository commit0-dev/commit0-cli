package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// ContextBuilder constructs embedding-ready context text from code nodes.
//
// Output follows the Gemini Embedding 2 document format:
//
//	title: [KIND] {Qualified} | text: {structured description}\n---\n{body}
//
// The embedder does NOT prepend an additional prefix — this builder produces
// the complete text sent to the embedding API.
type ContextBuilder struct {
	store        domain.GraphStore
	maxBodyRunes int
}

// NewContextBuilder creates a new context builder with a max body size in runes.
// Graph-neighborhood enrichment is disabled (no store attached).
func NewContextBuilder(maxBodyRunes int) *ContextBuilder {
	if maxBodyRunes <= 0 {
		maxBodyRunes = 32768
	}
	return &ContextBuilder{maxBodyRunes: maxBodyRunes}
}

// NewContextBuilderWithStore creates a ContextBuilder that also injects
// graph-neighborhood context (callers/callees) into function embeddings.
func NewContextBuilderWithStore(maxBodyRunes int, store domain.GraphStore) *ContextBuilder {
	cb := NewContextBuilder(maxBodyRunes)
	cb.store = store
	return cb
}

// ForNodeCtx generates embedding input text enriched with graph-neighborhood
// data (callers and callees) when a GraphStore is attached and node.ID is set.
// Falls back to ForNode if the store is nil or the lookup fails.
func (cb *ContextBuilder) ForNodeCtx(ctx context.Context, node *types.CodeNode) string {
	if node == nil || cb.store == nil || node.ID == "" {
		return cb.ForNode(node)
	}

	switch node.Kind {
	case types.NodeFunction:
		return cb.forFunctionCtx(ctx, node)
	case types.NodeClass:
		return cb.forClassCtx(ctx, node)
	case types.NodeFile:
		return cb.forFileCtx(ctx, node)
	case types.NodeModule:
		return cb.forModuleCtx(ctx, node)
	default:
		return cb.ForNode(node)
	}
}

// forFunctionCtx enriches a function node using GetNeighborhood — a single
// round-trip that returns callers, callees (with signatures), and data-flow
// sources/sinks. Falls back to ForNode if the neighborhood is empty.
func (cb *ContextBuilder) forFunctionCtx(ctx context.Context, node *types.CodeNode) string {
	nb, err := cb.store.GetNeighborhood(ctx, node.ID)
	if err != nil || nb == nil || nb.IsEmpty() {
		return cb.ForNode(node)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "title: [FUNCTION] %s | text: ", node.Qualified)
	fmt.Fprintf(&sb, "%s function defined in %s, lines %d–%d.", node.Language, node.FilePath, node.StartLine, node.EndLine)
	if node.Signature != "" {
		fmt.Fprintf(&sb, " Signature: %s.", node.Signature)
	}
	if node.Docstring != "" {
		fmt.Fprintf(&sb, " %s.", node.Docstring)
	}
	sb.WriteByte('\n')
	if len(nb.Callees) > 0 {
		fmt.Fprintf(&sb, "Calls: %s\n", strings.Join(neighborSigs(nb.Callees), ", "))
	}
	if len(nb.Callers) > 0 {
		fmt.Fprintf(&sb, "Called by: %s\n", strings.Join(neighborSigs(nb.Callers), ", "))
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
		fmt.Fprintf(&sb, "Data flows to: %s\n", strings.Join(parts, ", "))
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
		fmt.Fprintf(&sb, "Data flows from: %s\n", strings.Join(parts, ", "))
	}
	if len(nb.Reads) > 0 {
		fmt.Fprintf(&sb, "Reads fields: %s\n", strings.Join(nb.Reads, ", "))
	}
	if len(nb.Writes) > 0 {
		fmt.Fprintf(&sb, "Writes fields: %s\n", strings.Join(nb.Writes, ", "))
	}
	sb.WriteString("---\n")
	sb.WriteString(cb.truncate(node.Body, cb.maxBodyRunes))
	return sb.String()
}

// forClassCtx enriches a class/struct node with its methods, inheritance chain,
// and which functions use it.
func (cb *ContextBuilder) forClassCtx(ctx context.Context, node *types.CodeNode) string {
	nb, err := cb.store.GetNeighborhood(ctx, node.ID)
	if err != nil || nb == nil || nb.IsEmpty() {
		return cb.ForNode(node)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "title: [CLASS] %s | text: ", node.Qualified)
	fmt.Fprintf(&sb, "%s type defined in %s.", node.Language, node.FilePath)
	if node.Docstring != "" {
		fmt.Fprintf(&sb, " %s.", node.Docstring)
	}
	sb.WriteByte('\n')
	// Callers for a class node are functions that call its methods (uses edges).
	if len(nb.Callers) > 0 {
		fmt.Fprintf(&sb, "Used by: %s\n", strings.Join(neighborNames(nb.Callers), ", "))
	}
	if len(nb.Callees) > 0 {
		fmt.Fprintf(&sb, "Calls: %s\n", strings.Join(neighborNames(nb.Callees), ", "))
	}
	sb.WriteString("---\n")
	classBodyLimit := min(2048, cb.maxBodyRunes)
	sb.WriteString(cb.truncate(node.Body, classBodyLimit))
	return sb.String()
}

// forFileCtx enriches a file node with its import modules and defined symbols.
func (cb *ContextBuilder) forFileCtx(ctx context.Context, node *types.CodeNode) string {
	nb, err := cb.store.GetNeighborhood(ctx, node.ID)
	if err != nil || nb == nil || nb.IsEmpty() {
		return cb.ForNode(node)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "title: [FILE] %s | text: ", node.FilePath)
	fmt.Fprintf(&sb, "%s source file.", node.Language)
	sb.WriteByte('\n')
	if len(nb.Callees) > 0 {
		fmt.Fprintf(&sb, "Imports: %s\n", strings.Join(neighborNames(nb.Callees), ", "))
	}
	if len(nb.Callers) > 0 {
		fmt.Fprintf(&sb, "Defines: %s\n", strings.Join(neighborNames(nb.Callers), ", "))
	}
	sb.WriteString("---\n")
	fileBodyLimit := min(4096, cb.maxBodyRunes)
	sb.WriteString(cb.truncate(node.Body, fileBodyLimit))
	return sb.String()
}

// forModuleCtx enriches a module node with its importers (reverse imports).
func (cb *ContextBuilder) forModuleCtx(ctx context.Context, node *types.CodeNode) string {
	nb, _ := cb.store.GetNeighborhood(ctx, node.ID)

	var sb strings.Builder
	fmt.Fprintf(&sb, "title: [MODULE] %s | text: ", node.Name)
	fmt.Fprintf(&sb, "External %s package imported as %q.", node.Language, node.Qualified)
	if node.Docstring != "" {
		fmt.Fprintf(&sb, " Version: %s.", node.Docstring)
	}
	sb.WriteByte('\n')
	if nb != nil && len(nb.Callers) > 0 {
		fmt.Fprintf(&sb, "Imported by: %s\n", strings.Join(neighborNames(nb.Callers), ", "))
	}
	fmt.Fprintf(&sb, "Package %s provides functionality imported via %q.\n", node.Name, node.Qualified)
	return sb.String()
}

// neighborSigs formats neighbors as "Qualified(Signature)" when a signature is
// available, falling back to qualified name only.
func neighborSigs(nodes []domain.NeighborNode) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.Signature != "" {
			out = append(out, fmt.Sprintf("%s %s", n.Qualified, n.Signature))
		} else {
			out = append(out, n.Qualified)
		}
	}
	return out
}

// neighborNames returns just the qualified names.
func neighborNames(nodes []domain.NeighborNode) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.Qualified != "" {
			out = append(out, n.Qualified)
		}
	}
	return out
}

// ForNode generates embedding input text from a code node, formatted for the
// Gemini Embedding 2 document convention: title: {title} | text: {description}.
func (cb *ContextBuilder) ForNode(node *types.CodeNode) string {
	if node == nil {
		return ""
	}

	var sb strings.Builder

	switch node.Kind {
	case types.NodeFunction:
		fmt.Fprintf(&sb, "title: [FUNCTION] %s | text: ", node.Qualified)
		fmt.Fprintf(&sb, "%s function defined in %s, lines %d–%d.", node.Language, node.FilePath, node.StartLine, node.EndLine)
		if node.Signature != "" {
			fmt.Fprintf(&sb, " Signature: %s.", node.Signature)
		}
		if node.Docstring != "" {
			fmt.Fprintf(&sb, " %s.", node.Docstring)
		}
		sb.WriteString("\n---\n")
		sb.WriteString(cb.truncate(node.Body, cb.maxBodyRunes))

	case types.NodeClass:
		fmt.Fprintf(&sb, "title: [CLASS] %s | text: ", node.Qualified)
		fmt.Fprintf(&sb, "%s type defined in %s.", node.Language, node.FilePath)
		if node.Docstring != "" {
			fmt.Fprintf(&sb, " %s.", node.Docstring)
		}
		sb.WriteString("\n---\n")
		// Cap class bodies at 2048 runes (512 tokens equivalent)
		classBodyLimit := 2048
		if cb.maxBodyRunes < classBodyLimit {
			classBodyLimit = cb.maxBodyRunes
		}
		sb.WriteString(cb.truncate(node.Body, classBodyLimit))

	case types.NodeFile:
		fmt.Fprintf(&sb, "title: [FILE] %s | text: ", node.FilePath)
		fmt.Fprintf(&sb, "%s source file.", node.Language)
		sb.WriteString("\n---\n")
		// Cap file bodies at 4096 runes
		fileBodyLimit := 4096
		if cb.maxBodyRunes < fileBodyLimit {
			fileBodyLimit = cb.maxBodyRunes
		}
		sb.WriteString(cb.truncate(node.Body, fileBodyLimit))

	case types.NodeModule:
		fmt.Fprintf(&sb, "title: [MODULE] %s | text: ", node.Name)
		fmt.Fprintf(&sb, "External %s package imported as %q.", node.Language, node.Qualified)
		if node.Docstring != "" {
			fmt.Fprintf(&sb, " Version: %s.", node.Docstring)
		}
		sb.WriteByte('\n')
		fmt.Fprintf(&sb, "Package %s provides functionality imported via %q.\n", node.Name, node.Qualified)

	default:
		sb.WriteString("title: code | text: ")
		sb.WriteString(cb.truncate(node.Body, cb.maxBodyRunes))
	}

	return sb.String()
}

// ForQuery generates embedding input text for a user query using the
// Gemini Embedding 2 code-retrieval task prefix.
func (cb *ContextBuilder) ForQuery(question string) string {
	return "task: code retrieval | query: " + question
}

// truncate safely truncates a string to maxRunes runes, counting Unicode properly.
func (cb *ContextBuilder) truncate(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == maxRunes {
			return s[:i]
		}
		count++
	}
	return s
}
