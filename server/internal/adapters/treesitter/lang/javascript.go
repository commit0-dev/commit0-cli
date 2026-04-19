package lang

import (
	sitter "github.com/smacker/go-tree-sitter"
	jssitter "github.com/smacker/go-tree-sitter/javascript"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// JavaScriptExtractor extracts code structure from JavaScript source files.
// It delegates all extraction logic to the shared extractTS function defined
// in typescript.go — JavaScript and TypeScript share the same grammar shape
// for the constructs we care about.
type JavaScriptExtractor struct{}

// Language returns the tree-sitter grammar for JavaScript.
func (e *JavaScriptExtractor) Language() *sitter.Language { return jssitter.GetLanguage() }

// Extract delegates to the shared TypeScript/JavaScript extraction logic.
func (e *JavaScriptExtractor) Extract(root *sitter.Node, file domain.FileEntry) ([]types.CodeNode, []types.CodeEdge) {
	return extractTS(root, file, "javascript")
}
