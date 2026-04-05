package app

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/commit0-dev/commit0/pkg/types"
)

// ContextBuilder constructs embedding-ready context text from code nodes
type ContextBuilder struct {
	maxBodyRunes int
}

// NewContextBuilder creates a new context builder with a max body size in runes
func NewContextBuilder(maxBodyRunes int) *ContextBuilder {
	if maxBodyRunes <= 0 {
		maxBodyRunes = 32768
	}
	return &ContextBuilder{maxBodyRunes: maxBodyRunes}
}

// ForNode generates embedding input text from a code node
func (cb *ContextBuilder) ForNode(node *types.CodeNode) string {
	if node == nil {
		return ""
	}

	var sb strings.Builder

	switch node.Kind {
	case types.NodeFunction:
		sb.WriteString(fmt.Sprintf("task: search result | query: [FUNCTION] %s\n", node.Qualified))
		sb.WriteString(fmt.Sprintf("Language: %s  File: %s:%d-%d\n", node.Language, node.FilePath, node.StartLine, node.EndLine))
		if node.Signature != "" {
			sb.WriteString(fmt.Sprintf("Signature: %s\n", node.Signature))
		}
		if node.Docstring != "" {
			sb.WriteString(fmt.Sprintf("Doc: %s\n", node.Docstring))
		}
		sb.WriteString("---\n")
		sb.WriteString(cb.truncate(node.Body, cb.maxBodyRunes))

	case types.NodeClass:
		sb.WriteString(fmt.Sprintf("task: search result | query: [CLASS] %s\n", node.Qualified))
		sb.WriteString(fmt.Sprintf("Language: %s  File: %s\n", node.Language, node.FilePath))
		if node.Docstring != "" {
			sb.WriteString(fmt.Sprintf("Doc: %s\n", node.Docstring))
		}
		sb.WriteString("---\n")
		// Cap class bodies at 2048 runes (512 tokens equivalent)
		classBodyLimit := 2048
		if cb.maxBodyRunes < classBodyLimit {
			classBodyLimit = cb.maxBodyRunes
		}
		sb.WriteString(cb.truncate(node.Body, classBodyLimit))

	case types.NodeFile:
		sb.WriteString(fmt.Sprintf("task: search result | query: [FILE] %s\n", node.FilePath))
		sb.WriteString(fmt.Sprintf("Language: %s\n", node.Language))
		sb.WriteString("---\n")
		// Cap file bodies at 4096 runes
		fileBodyLimit := 4096
		if cb.maxBodyRunes < fileBodyLimit {
			fileBodyLimit = cb.maxBodyRunes
		}
		sb.WriteString(cb.truncate(node.Body, fileBodyLimit))

	case types.NodeModule:
		sb.WriteString(fmt.Sprintf("task: search result | query: [MODULE] %s\n", node.Name))
		sb.WriteString(fmt.Sprintf("Path: %s\n", node.FilePath))
		if node.Body != "" {
			sb.WriteString("---\n")
			sb.WriteString(cb.truncate(node.Body, cb.maxBodyRunes))
		}

	default:
		// Unknown kind - just use prefix + body
		sb.WriteString("task: search result | query: ")
		sb.WriteString(cb.truncate(node.Body, cb.maxBodyRunes))
	}

	return sb.String()
}

// ForQuery generates embedding input text for a user query
func (cb *ContextBuilder) ForQuery(question string) string {
	return fmt.Sprintf("task: search query | query: %s", question)
}

// truncate safely truncates a string to maxRunes runes, counting Unicode properly
func (cb *ContextBuilder) truncate(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}

	if utf8.RuneCountInString(s) <= maxRunes {
		return s
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
