package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// defaultDocPrefix is prepended to document embeddings so they align with query
// embeddings in the same Gemini Embedding 2 task space.
// Queries use: "task: code retrieval | query: ..."
// Documents use: "task: code retrieval | document: ..."
const defaultDocPrefix = "task: code retrieval | document: "

// ContextBuilder constructs embedding-ready context text from code nodes.
//
// The output prioritizes semantic content (Summary, Concepts) over raw metadata,
// ensuring the embedding captures WHAT code does, not just WHERE it is.
type ContextBuilder struct {
	graph        domain.OpenCodeGraph
	maxBodyRunes int
	docPrefix    string // prepended to each document embedding text
}

// NewContextBuilder creates a new context builder with a max body size in runes.
// Graph-neighborhood enrichment is disabled (no store attached).
func NewContextBuilder(maxBodyRunes int) *ContextBuilder {
	if maxBodyRunes <= 0 {
		maxBodyRunes = 32768
	}
	return &ContextBuilder{maxBodyRunes: maxBodyRunes, docPrefix: defaultDocPrefix}
}

// SetDocPrefix overrides the document embedding prefix. Each embedding provider
// may use a different prefix convention (e.g. "search_document: " for nomic-embed-text).
func (cb *ContextBuilder) SetDocPrefix(prefix string) {
	cb.docPrefix = prefix
}

// NewContextBuilderWithStore creates a ContextBuilder that also injects
// graph-neighborhood context (callers/callees) into function embeddings.
func NewContextBuilderWithGraph(maxBodyRunes int, graph domain.OpenCodeGraph) *ContextBuilder {
	cb := NewContextBuilder(maxBodyRunes)
	cb.graph = graph
	return cb
}

// ForNodeCtx generates embedding input text enriched with graph-neighborhood
// data (callers and callees) when a GraphStore is attached and node.ID is set.
// Falls back to ForNode if the store is nil or the lookup fails.
func (cb *ContextBuilder) ForNodeCtx(ctx context.Context, node *types.CodeNode) string {
	if node == nil || cb.graph == nil || node.ID == "" {
		return cb.ForNode(node)
	}

	nb, err := cb.graph.Neighbors(ctx, node.ID)
	if err != nil || nb == nil || nb.IsEmpty() {
		return cb.ForNode(node)
	}

	return cb.forNodeWithNeighborhood(node, nb)
}

// forNodeWithNeighborhood builds embedding text with semantic summary + graph context.
func (cb *ContextBuilder) forNodeWithNeighborhood(node *types.CodeNode, nb *domain.Neighborhood) string {
	var sb strings.Builder

	// 1. Task prefix + Kind + Name
	sb.WriteString(cb.docPrefix)
	fmt.Fprintf(&sb, "[%s] %s", strings.ToUpper(string(node.Kind)), node.Qualified)

	// 2. Semantic summary (MOST IMPORTANT — what the code DOES)
	if node.Summary != "" {
		fmt.Fprintf(&sb, " — %s", node.Summary)
	} else if node.Docstring != "" {
		fmt.Fprintf(&sb, " — %s", node.Docstring)
	}

	// 3. Concept tags
	if len(node.Concepts) > 0 {
		fmt.Fprintf(&sb, " Concepts: %s.", strings.Join(node.Concepts, ", "))
	}

	sb.WriteByte('\n')

	// 4. Signature
	if node.Signature != "" {
		fmt.Fprintf(&sb, "Signature: %s\n", node.Signature)
	}

	// 5. Graph context
	if len(nb.Callers) > 0 {
		fmt.Fprintf(&sb, "Callers: %s\n", strings.Join(neighborSigs(nb.Callers), ", "))
	}
	if len(nb.Callees) > 0 {
		fmt.Fprintf(&sb, "Callees: %s\n", strings.Join(neighborSigs(nb.Callees), ", "))
	}
	if len(nb.DataSinks) > 0 {
		parts := make([]string, 0, len(nb.DataSinks))
		for _, s := range nb.DataSinks {
			part := s.Qualified
			if s.ParamName != "" {
				part += fmt.Sprintf(" (param %q)", s.ParamName)
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
		fmt.Fprintf(&sb, "Reads: %s\n", strings.Join(nb.Reads, ", "))
	}
	if len(nb.Writes) > 0 {
		fmt.Fprintf(&sb, "Writes: %s\n", strings.Join(nb.Writes, ", "))
	}

	// 6. Body (secondary — code body for exact matching)
	sb.WriteString("---\n")
	bodyLimit := cb.bodyLimit(node.Kind)
	sb.WriteString(cb.truncate(node.Body, bodyLimit))

	return sb.String()
}

// ForNode generates embedding input text from a code node.
// Uses semantic summary if available, falls back to metadata.
func (cb *ContextBuilder) ForNode(node *types.CodeNode) string {
	if node == nil {
		return ""
	}

	var sb strings.Builder

	// Task prefix + Kind + Name
	sb.WriteString(cb.docPrefix)
	fmt.Fprintf(&sb, "[%s] %s", strings.ToUpper(string(node.Kind)), cb.nodeLabel(node))

	// Semantic summary (priority) or docstring (fallback)
	if node.Summary != "" {
		fmt.Fprintf(&sb, " — %s", node.Summary)
	} else if node.Docstring != "" {
		fmt.Fprintf(&sb, " — %s", node.Docstring)
	} else {
		// Minimal fallback: location metadata
		fmt.Fprintf(&sb, " — %s %s defined in %s", node.Language, node.Kind, node.FilePath)
		if node.StartLine > 0 {
			fmt.Fprintf(&sb, ", lines %d–%d", node.StartLine, node.EndLine)
		}
	}

	// Concept tags
	if len(node.Concepts) > 0 {
		fmt.Fprintf(&sb, ". Concepts: %s", strings.Join(node.Concepts, ", "))
	}

	// Signature
	if node.Signature != "" {
		fmt.Fprintf(&sb, "\nSignature: %s", node.Signature)
	}

	// Body
	sb.WriteString("\n---\n")
	bodyLimit := cb.bodyLimit(node.Kind)
	sb.WriteString(cb.truncate(node.Body, bodyLimit))

	return sb.String()
}

// ForQuery generates embedding input text for a user query.
func (cb *ContextBuilder) ForQuery(question string) string {
	return "task: code retrieval | query: " + question
}

// nodeLabel returns the best label for a node (qualified name or file path).
func (cb *ContextBuilder) nodeLabel(node *types.CodeNode) string {
	switch node.Kind {
	case types.NodeFile:
		return node.FilePath
	case types.NodeModule:
		if node.Name != "" {
			return node.Name
		}
		return node.Qualified
	default:
		return node.Qualified
	}
}

// bodyLimit returns the max body runes for a given node kind.
func (cb *ContextBuilder) bodyLimit(kind types.NodeKind) int {
	switch kind {
	case types.NodeClass:
		return min(2048, cb.maxBodyRunes)
	case types.NodeFile:
		return min(2048, cb.maxBodyRunes)
	default:
		// Functions: cap at maxBodyRunes. The metadata (prefix, summary,
		// signature, graph neighbors) consumes ~500 runes, so body gets
		// the remainder of the model's context budget.
		return min(4096, cb.maxBodyRunes)
	}
}

// neighborSigs formats neighbors as "Qualified(Signature)".
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

// truncate safely truncates a string to maxRunes runes.
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
