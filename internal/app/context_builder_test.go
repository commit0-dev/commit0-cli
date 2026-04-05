package app

import (
	"strings"
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
)

func TestContextBuilderForFunction(t *testing.T) {
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind:       types.NodeFunction,
		Qualified:  "pkg.Handler",
		Language:   "go",
		FilePath:   "main.go",
		StartLine:  10,
		EndLine:    20,
		Signature:  "func(w http.ResponseWriter, r *http.Request)",
		Docstring:  "Handle HTTP requests",
		Body:       "func Handler(w http.ResponseWriter, r *http.Request) { }",
	}

	result := cb.ForNode(node)

	if !strings.Contains(result, "task: search result | query: [FUNCTION]") {
		t.Errorf("result missing task prefix")
	}

	if !strings.Contains(result, "pkg.Handler") {
		t.Errorf("result missing qualified name")
	}

	if !strings.Contains(result, "go") {
		t.Errorf("result missing language")
	}

	if !strings.Contains(result, "main.go:10-20") {
		t.Errorf("result missing file path and line numbers")
	}

	if !strings.Contains(result, "Signature:") {
		t.Errorf("result missing signature")
	}

	if !strings.Contains(result, "Handle HTTP requests") {
		t.Errorf("result missing docstring")
	}
}

func TestContextBuilderForClass(t *testing.T) {
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind:      types.NodeClass,
		Qualified: "pkg.User",
		Language:  "python",
		FilePath:  "models.py",
		Docstring: "User class",
		Body:      "class User: pass",
	}

	result := cb.ForNode(node)

	if !strings.Contains(result, "[CLASS]") {
		t.Errorf("result missing [CLASS] prefix")
	}

	if !strings.Contains(result, "pkg.User") {
		t.Errorf("result missing qualified name")
	}
}

func TestContextBuilderForFile(t *testing.T) {
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind:     types.NodeFile,
		FilePath: "main.go",
		Language: "go",
		Body:     "package main",
	}

	result := cb.ForNode(node)

	if !strings.Contains(result, "[FILE]") {
		t.Errorf("result missing [FILE] prefix")
	}

	if !strings.Contains(result, "main.go") {
		t.Errorf("result missing file path")
	}
}

func TestContextBuilderForModule(t *testing.T) {
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind:     types.NodeModule,
		Name:     "utils",
		FilePath: "utils/",
		Body:     "module content",
	}

	result := cb.ForNode(node)

	if !strings.Contains(result, "[MODULE]") {
		t.Errorf("result missing [MODULE] prefix")
	}

	if !strings.Contains(result, "utils") {
		t.Errorf("result missing module name")
	}
}

func TestContextBuilderForQuery(t *testing.T) {
	cb := NewContextBuilder(1000)
	result := cb.ForQuery("where is the error handler?")

	if !strings.Contains(result, "task: search query | query:") {
		t.Errorf("result missing query task prefix")
	}

	if !strings.Contains(result, "where is the error handler?") {
		t.Errorf("result missing query text")
	}
}

func TestContextBuilderTruncate(t *testing.T) {
	cb := NewContextBuilder(10)

	longText := "This is a very long text that should be truncated"
	result := cb.truncate(longText, 10)

	// Count runes
	runeCount := len([]rune(result))
	if runeCount > 10 {
		t.Errorf("truncate(%d) returned %d runes", 10, runeCount)
	}
}

func TestContextBuilderTruncateUnicode(t *testing.T) {
	cb := NewContextBuilder(1000)

	// Unicode string with emojis
	unicodeText := "Hello 世界 🌍 こんにちは"
	result := cb.truncate(unicodeText, 5)

	runeCount := len([]rune(result))
	if runeCount > 5 {
		t.Errorf("truncate should count runes, not bytes")
	}
}

func TestContextBuilderEmptyDocstring(t *testing.T) {
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind:      types.NodeFunction,
		Qualified: "pkg.Func",
		Language:  "go",
		FilePath:  "main.go",
		Docstring: "", // empty
		Body:      "func Func() {}",
	}

	result := cb.ForNode(node)

	// Should not have "Doc:" line if docstring is empty
	if strings.Contains(result, "Doc:") && !strings.Contains(result, "Doc: \n") {
		// Allow "Doc:" only if followed by empty value
		t.Errorf("result should not have Doc: line for empty docstring")
	}
}

func TestContextBuilderNilNode(t *testing.T) {
	cb := NewContextBuilder(1000)
	result := cb.ForNode(nil)

	if result != "" {
		t.Errorf("ForNode(nil) should return empty string, got %q", result)
	}
}
