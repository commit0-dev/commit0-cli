// CGO_ENABLED=1 required — tree-sitter uses C libraries.
package treesitter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/commit0-dev/commit0/internal/adapters/treesitter/lang"
	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// Extractor extracts code nodes and edges from a parsed tree-sitter AST.
type Extractor interface {
	// Language returns the sitter.Language grammar for this extractor.
	Language() *sitter.Language
	// Extract walks root and returns all code nodes and raw edges found.
	Extract(root *sitter.Node, file domain.FileEntry) ([]types.CodeNode, []types.CodeEdge)
}

// TreeSitterParser implements domain.Parser using tree-sitter grammars.
type TreeSitterParser struct {
	extractors map[string]Extractor // keyed by language name
	resolver   *Resolver
	log        *slog.Logger
}

// Compile-time interface check.
var _ domain.Parser = (*TreeSitterParser)(nil)

// NewParser creates a TreeSitterParser with all supported language extractors
// registered (go, python, typescript, javascript).
func NewParser(log *slog.Logger) *TreeSitterParser {
	if log == nil {
		log = slog.Default()
	}
	p := &TreeSitterParser{
		extractors: make(map[string]Extractor, 4),
		resolver:   &Resolver{},
		log:        log,
	}
	p.extractors["go"] = &lang.GoExtractor{}
	p.extractors["python"] = &lang.PythonExtractor{}
	p.extractors["typescript"] = &lang.TypeScriptExtractor{}
	p.extractors["javascript"] = &lang.JavaScriptExtractor{}
	return p
}

// SupportedLanguages returns the list of language names this parser handles.
func (p *TreeSitterParser) SupportedLanguages() []string {
	langs := make([]string, 0, len(p.extractors)+1)
	langs = append(langs, "gomod")
	for l := range p.extractors {
		langs = append(langs, l)
	}
	return langs
}

// Parse parses a single FileEntry and returns structured code nodes and edges.
//
// Pipeline:
//  1. Look up the extractor for file.Language.
//  2. Build a tree-sitter parser, set the grammar, and parse file.Content.
//  3. Run the language extractor over the AST root.
//  4. Prepend a synthetic NodeFile node for the file itself.
//  5. Run the resolver pass to add EdgeDefines and resolve call targets.
//  6. Return a ParsedFile with SHA-256 content hash.
func (p *TreeSitterParser) Parse(ctx context.Context, file domain.FileEntry) (*domain.ParsedFile, error) {
	if file.Path == "" {
		return nil, domain.Validation("file path must not be empty")
	}
	if len(file.Content) == 0 {
		return nil, domain.Validation(fmt.Sprintf("file %q has empty content", file.Path))
	}

	// go.mod uses a dedicated text parser (no tree-sitter grammar).
	if file.Language == "gomod" {
		return ParseGoMod(ctx, file)
	}

	ext, ok := p.extractors[file.Language]
	if !ok {
		return nil, domain.Validation(fmt.Sprintf("unsupported language %q for file %q", file.Language, file.Path))
	}

	// Check context before the CGO call.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Build a tree-sitter parser for this grammar.
	parser := sitter.NewParser()
	parser.SetLanguage(ext.Language())
	tree, err := parser.ParseCtx(ctx, nil, file.Content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse %q: %w", file.Path, err)
	}
	defer tree.Close()

	root := tree.RootNode()

	// Extract raw nodes and edges from the AST.
	nodes, edges := ext.Extract(root, file)

	// Prepend the file node.
	fileNode := makeFileNode(file)
	nodes = append([]types.CodeNode{fileNode}, nodes...)

	// Resolver pass: add EdgeDefines + attempt to resolve call targets.
	nodes, edges = p.resolver.Resolve(nodes, edges)

	return &domain.ParsedFile{
		Path:        file.Path,
		Language:    file.Language,
		ContentHash: sha256Hex(file.Content),
		Nodes:       nodes,
		Edges:       edges,
		LineCount:   countLines(file.Content),
		SizeBytes:   len(file.Content),
	}, nil
}

// makeFileNode constructs a synthetic CodeNode representing the file itself.
func makeFileNode(file domain.FileEntry) types.CodeNode {
	qualified := file.Path
	return types.CodeNode{
		ID:         makeNodeID(string(types.NodeFile), qualified),
		Kind:       types.NodeFile,
		Name:       lastPathSegment(file.Path),
		Qualified:  qualified,
		FilePath:   file.Path,
		Language:   file.Language,
		StartLine:  1,
		EndLine:    countLines(file.Content),
		Visibility: "public",
	}
}

// makeNodeID returns the canonical record ID for a node.
func makeNodeID(kind string, qualified string) string {
	safe := strings.ReplaceAll(qualified, "/", "⋅")
	safe = strings.ReplaceAll(safe, ".", "⋅")
	return fmt.Sprintf("%s:%s", kind, safe)
}

// sha256Hex returns the lower-case hex SHA-256 of data.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// countLines counts the number of lines in data.
func countLines(data []byte) int {
	return bytes.Count(data, []byte("\n")) + 1
}

// lastPathSegment returns the last component of a slash-separated path.
func lastPathSegment(path string) string {
	idx := strings.LastIndexByte(path, '/')
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}
