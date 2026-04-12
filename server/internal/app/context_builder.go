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
const defaultDocPrefix = "task: code retrieval | document: "

// ContextBuilder constructs context text for LLM operations.
// Each operation has a TokenBudget that controls how much text is allocated
// to each section (summary, signature, neighbors, body).
type ContextBuilder struct {
	graph     domain.OpenCodeGraph
	budget    domain.TokenBudget
	docPrefix string
}

// NewContextBuilder creates a context builder with the given token budget.
func NewContextBuilder(budget domain.TokenBudget) *ContextBuilder {
	return &ContextBuilder{budget: budget, docPrefix: defaultDocPrefix}
}

// SetDocPrefix overrides the document embedding prefix.
func (cb *ContextBuilder) SetDocPrefix(prefix string) {
	cb.docPrefix = prefix
}

// NewContextBuilderWithGraph creates a ContextBuilder with graph-neighborhood enrichment.
func NewContextBuilderWithGraph(budget domain.TokenBudget, graph domain.OpenCodeGraph) *ContextBuilder {
	cb := NewContextBuilder(budget)
	cb.graph = graph
	return cb
}

// ForNodeCtx generates embedding input text enriched with graph-neighborhood
// data when a graph is attached. Falls back to ForNode otherwise.
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

// forNodeWithNeighborhood builds text with budget-allocated sections.
func (cb *ContextBuilder) forNodeWithNeighborhood(node *types.CodeNode, nb *domain.Neighborhood) string {
	var sb strings.Builder
	b := cb.budget

	// 1. Prefix (highest priority)
	prefix := fmt.Sprintf("%s[%s] %s", cb.docPrefix, strings.ToUpper(string(node.Kind)), node.Qualified)
	sb.WriteString(truncate(prefix, b.Prefix))

	// 2. Summary
	if b.Summary > 0 {
		if node.Summary != "" {
			sb.WriteString(" — ")
			sb.WriteString(truncate(node.Summary, b.Summary))
		} else if node.Docstring != "" {
			sb.WriteString(" — ")
			sb.WriteString(truncate(node.Docstring, b.Summary))
		}
	}

	// 3. Concepts
	if b.Concepts > 0 && len(node.Concepts) > 0 {
		sb.WriteString(" Concepts: ")
		sb.WriteString(truncate(strings.Join(node.Concepts, ", "), b.Concepts))
		sb.WriteByte('.')
	}

	sb.WriteByte('\n')

	// 4. Signature
	if b.Signature > 0 && node.Signature != "" {
		fmt.Fprintf(&sb, "Signature: %s\n", truncate(node.Signature, b.Signature))
	}

	// 5. Neighbors (within neighbor budget)
	if b.Neighbors > 0 {
		var nbText strings.Builder
		if len(nb.Callers) > 0 {
			fmt.Fprintf(&nbText, "Callers: %s\n", strings.Join(neighborSigs(nb.Callers), ", "))
		}
		if len(nb.Callees) > 0 {
			fmt.Fprintf(&nbText, "Callees: %s\n", strings.Join(neighborSigs(nb.Callees), ", "))
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
			fmt.Fprintf(&nbText, "Data flows to: %s\n", strings.Join(parts, ", "))
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
			fmt.Fprintf(&nbText, "Data flows from: %s\n", strings.Join(parts, ", "))
		}
		if len(nb.Reads) > 0 {
			fmt.Fprintf(&nbText, "Reads: %s\n", strings.Join(nb.Reads, ", "))
		}
		if len(nb.Writes) > 0 {
			fmt.Fprintf(&nbText, "Writes: %s\n", strings.Join(nb.Writes, ", "))
		}
		sb.WriteString(truncate(nbText.String(), b.Neighbors))
	}

	// 6. Body (lowest priority — gets remainder of budget)
	if b.Body > 0 && node.Body != "" {
		sb.WriteString("---\n")
		sb.WriteString(truncate(node.Body, b.Body))
	}

	// Final safety: enforce total budget
	result := sb.String()
	if len(result) > b.Total && b.Total > 0 {
		return truncate(result, b.Total)
	}
	return result
}

// ForNode generates embedding input text without graph neighborhood.
func (cb *ContextBuilder) ForNode(node *types.CodeNode) string {
	if node == nil {
		return ""
	}

	var sb strings.Builder
	b := cb.budget

	// Prefix
	prefix := fmt.Sprintf("%s[%s] %s", cb.docPrefix, strings.ToUpper(string(node.Kind)), cb.nodeLabel(node))
	sb.WriteString(truncate(prefix, b.Prefix))

	// Summary
	if b.Summary > 0 {
		if node.Summary != "" {
			sb.WriteString(" — ")
			sb.WriteString(truncate(node.Summary, b.Summary))
		} else if node.Docstring != "" {
			sb.WriteString(" — ")
			sb.WriteString(truncate(node.Docstring, b.Summary))
		} else {
			fmt.Fprintf(&sb, " — %s %s defined in %s", node.Language, node.Kind, node.FilePath)
			if node.StartLine > 0 {
				fmt.Fprintf(&sb, ", lines %d–%d", node.StartLine, node.EndLine)
			}
		}
	}

	// Concepts
	if b.Concepts > 0 && len(node.Concepts) > 0 {
		sb.WriteString(". Concepts: ")
		sb.WriteString(truncate(strings.Join(node.Concepts, ", "), b.Concepts))
	}

	// Signature
	if b.Signature > 0 && node.Signature != "" {
		fmt.Fprintf(&sb, "\nSignature: %s", truncate(node.Signature, b.Signature))
	}

	// Body
	if b.Body > 0 && node.Body != "" {
		sb.WriteString("\n---\n")
		sb.WriteString(truncate(node.Body, b.Body))
	}

	result := sb.String()
	if len(result) > b.Total && b.Total > 0 {
		return truncate(result, b.Total)
	}
	return result
}

// ForQuery generates embedding input text for a user query.
func (cb *ContextBuilder) ForQuery(question string) string {
	return "task: code retrieval | query: " + question
}

// nodeLabel returns the best label for a node.
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

// neighborSigs formats neighbors as "Qualified Signature".
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
func truncate(s string, maxRunes int) string {
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
