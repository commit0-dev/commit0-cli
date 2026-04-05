---
name: commit0-treesitter
description: tree-sitter parsing for commit0. TRIGGER when: writing the Parser adapter, per-language AST extractors (Go/Python/TypeScript/JavaScript), node/edge extraction, type resolution, CGO integration, or internal/adapters/treesitter/. DO NOT TRIGGER for SurrealDB or Gemini code.
---

# commit0 tree-sitter Parsing Skill

Use this skill when writing tree-sitter related code for commit0: the Parser adapter, per-language extractors, type resolution, AST node/edge extraction, and CGO integration.

---

## Overview

commit0 uses tree-sitter (via `github.com/smacker/go-tree-sitter`) to parse source files into AST nodes and edges. The parser adapter implements the `domain.Parser` interface and lives in `internal/adapters/treesitter/`.

### Supported Languages

| Language | Grammar Package | Extractor File |
|---|---|---|
| Go | `github.com/smacker/go-tree-sitter/golang` | `lang/golang.go` |
| Python | `github.com/smacker/go-tree-sitter/python` | `lang/python.go` |
| TypeScript | `github.com/smacker/go-tree-sitter/typescript/typescript` | `lang/typescript.go` |
| JavaScript | `github.com/smacker/go-tree-sitter/javascript` | `lang/javascript.go` |

---

## Architecture

```
internal/adapters/treesitter/
├── parser.go           # Main Parser implementation (implements domain.Parser)
├── resolver.go         # Type resolution pass (methods → classes, interface dispatch)
└── lang/               # Per-language AST extractors
    ├── golang.go       # Go: func_decl, method_decl, type_spec, call_expr
    ├── python.go       # Python: function_def, class_def, import, call
    ├── typescript.go   # TypeScript: function_decl, method_def, class_decl, call_expr
    └── javascript.go   # JavaScript: same as TypeScript
```

---

## Parser Interface Implementation

```go
// internal/adapters/treesitter/parser.go

type TreeSitterParser struct {
    languages map[string]*sitter.Language
    log       *slog.Logger
}

// Compile-time interface check
var _ domain.Parser = (*TreeSitterParser)(nil)

func NewParser() *TreeSitterParser {
    return &TreeSitterParser{
        languages: map[string]*sitter.Language{
            "go":         golang.GetLanguage(),
            "python":     python.GetLanguage(),
            "typescript": typescript.GetLanguage(),
            "javascript": javascript.GetLanguage(),
        },
    }
}

func (p *TreeSitterParser) Parse(ctx context.Context, file domain.FileEntry) (*domain.ParsedFile, error) {
    lang, ok := p.languages[file.Language]
    if !ok {
        return nil, &domain.DomainError{
            Code:    domain.ErrValidation,
            Message: fmt.Sprintf("unsupported language: %s", file.Language),
        }
    }

    parser := sitter.NewParser()
    parser.SetLanguage(lang)

    tree, err := parser.ParseCtx(ctx, nil, file.Content)
    if err != nil {
        return nil, fmt.Errorf("parse %s: %w", file.Path, err)
    }
    defer tree.Close()

    extractor := p.getExtractor(file.Language)
    nodes, edges := extractor.Extract(tree.RootNode(), file)

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

func (p *TreeSitterParser) SupportedLanguages() []string {
    langs := make([]string, 0, len(p.languages))
    for k := range p.languages {
        langs = append(langs, k)
    }
    return langs
}
```

---

## Per-Language Extractor Interface

Each language extractor follows a common pattern:

```go
// Extractor interface (internal, not a domain port)
type Extractor interface {
    Extract(root *sitter.Node, file domain.FileEntry) ([]types.CodeNode, []types.CodeEdge)
}
```

---

## Go Extractor Example

The Go extractor extracts the following AST node types:

| tree-sitter Node Type | commit0 NodeKind | Notes |
|---|---|---|
| `function_declaration` | `NodeFunction` | Top-level functions |
| `method_declaration` | `NodeFunction` | Methods with receiver |
| `type_spec` (struct/interface) | `NodeClass` | Structs, interfaces, type aliases |
| `call_expression` | Edge: `EdgeCalls` | Function call sites |
| `import_declaration` | Edge: `EdgeImports` | Package imports |
| `type_assertion` / embed | Edge: `EdgeInherits` | Interface embedding |

```go
// internal/adapters/treesitter/lang/golang.go

type GoExtractor struct{}

func (e *GoExtractor) Extract(root *sitter.Node, file domain.FileEntry) ([]types.CodeNode, []types.CodeEdge) {
    var nodes []types.CodeNode
    var edges []types.CodeEdge

    // Walk the AST recursively
    iter := sitter.NewIterator(root, sitter.BFSMode)
    for {
        node, err := iter.Next()
        if err != nil {
            break
        }

        switch node.Type() {
        case "function_declaration":
            fn := e.extractFunction(node, file)
            nodes = append(nodes, fn)
            edges = append(edges, e.extractCallEdges(node, fn.ID, file)...)

        case "method_declaration":
            fn := e.extractMethod(node, file)
            nodes = append(nodes, fn)
            edges = append(edges, e.extractCallEdges(node, fn.ID, file)...)

        case "type_spec":
            cls := e.extractType(node, file)
            if cls != nil {
                nodes = append(nodes, *cls)
            }

        case "import_declaration":
            edges = append(edges, e.extractImports(node, file)...)
        }
    }

    return nodes, edges
}
```

### Extracting Functions

```go
func (e *GoExtractor) extractFunction(node *sitter.Node, file domain.FileEntry) types.CodeNode {
    nameNode := node.ChildByFieldName("name")
    paramsNode := node.ChildByFieldName("parameters")
    resultNode := node.ChildByFieldName("result")

    name := nameNode.Content(file.Content)
    qualified := fmt.Sprintf("%s.%s", file.Path, name) // Will be refined by resolver

    signature := ""
    if paramsNode != nil {
        signature = paramsNode.Content(file.Content)
    }
    if resultNode != nil {
        signature += " " + resultNode.Content(file.Content)
    }

    return types.CodeNode{
        ID:        makeNodeID("function", qualified),
        Kind:      types.NodeFunction,
        Name:      name,
        Qualified: qualified,
        FilePath:  file.Path,
        Language:  "go",
        StartLine: int(node.StartPoint().Row) + 1,
        EndLine:   int(node.EndPoint().Row) + 1,
        Signature: signature,
        Body:      node.Content(file.Content),
        Visibility: goVisibility(name),
    }
}

func goVisibility(name string) string {
    if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
        return "public"
    }
    return "private"
}
```

### Extracting Methods (with Receiver)

```go
func (e *GoExtractor) extractMethod(node *sitter.Node, file domain.FileEntry) types.CodeNode {
    nameNode := node.ChildByFieldName("name")
    receiverNode := node.ChildByFieldName("receiver")

    name := nameNode.Content(file.Content)
    receiver := extractReceiverType(receiverNode, file.Content)
    qualified := fmt.Sprintf("%s.%s.%s", file.Path, receiver, name)

    return types.CodeNode{
        ID:        makeNodeID("function", qualified),
        Kind:      types.NodeFunction,
        Name:      name,
        Qualified: qualified,
        // ... rest same as function
    }
}
```

### Extracting Call Edges

```go
func (e *GoExtractor) extractCallEdges(fnNode *sitter.Node, callerID string, file domain.FileEntry) []types.CodeEdge {
    var edges []types.CodeEdge

    // Find all call_expression nodes within the function body
    body := fnNode.ChildByFieldName("body")
    if body == nil {
        return edges
    }

    iter := sitter.NewIterator(body, sitter.BFSMode)
    for {
        node, err := iter.Next()
        if err != nil {
            break
        }
        if node.Type() != "call_expression" {
            continue
        }

        funcNode := node.ChildByFieldName("function")
        if funcNode == nil {
            continue
        }

        calleeName := funcNode.Content(file.Content)
        callSite := fmt.Sprintf("%s:%d", file.Path, node.StartPoint().Row+1)

        edges = append(edges, types.CodeEdge{
            Kind:     types.EdgeCalls,
            FromID:   callerID,
            ToID:     makeNodeID("function", calleeName), // Resolved later
            CallSite: callSite,
            CallType: detectCallType(node, file.Content),
        })
    }

    return edges
}

func detectCallType(node *sitter.Node, content []byte) string {
    parent := node.Parent()
    if parent != nil {
        switch parent.Type() {
        case "go_statement":
            return "goroutine"
        case "defer_statement":
            return "deferred"
        }
    }
    // Check if calling through interface (resolved later by resolver.go)
    return "direct"
}
```

---

## Type Resolution (resolver.go)

After initial extraction, the resolver performs a second pass to:

1. **Resolve method receivers** to their class/struct nodes
2. **Link methods to classes** via `defines` edges
3. **Detect interface dispatch** (mark calls as `is_dynamic: true`)
4. **Resolve cross-file references** (qualify unqualified call targets)

```go
// internal/adapters/treesitter/resolver.go

type Resolver struct {
    nodes map[string]*types.CodeNode  // qualified name → node
    edges []types.CodeEdge
}

func (r *Resolver) Resolve(nodes []types.CodeNode, edges []types.CodeEdge) ([]types.CodeNode, []types.CodeEdge) {
    // Build lookup maps
    r.buildIndex(nodes)

    // Resolve unqualified call targets
    for i, edge := range edges {
        if edge.Kind == types.EdgeCalls {
            resolved := r.resolveCallTarget(edge.ToID)
            if resolved != "" {
                edges[i].ToID = resolved
            }
        }
    }

    // Generate defines edges (file → function, file → class)
    defines := r.generateDefinesEdges(nodes)
    edges = append(edges, defines...)

    return nodes, edges
}
```

---

## Python Extractor Node Types

| tree-sitter Node Type | commit0 NodeKind |
|---|---|
| `function_definition` | `NodeFunction` |
| `class_definition` | `NodeClass` |
| `import_statement` | Edge: `EdgeImports` |
| `import_from_statement` | Edge: `EdgeImports` |
| `call` | Edge: `EdgeCalls` |
| `decorated_definition` | Unwrap to inner function/class |

---

## TypeScript/JavaScript Extractor Node Types

| tree-sitter Node Type | commit0 NodeKind |
|---|---|
| `function_declaration` | `NodeFunction` |
| `method_definition` | `NodeFunction` |
| `arrow_function` (named) | `NodeFunction` |
| `class_declaration` | `NodeClass` |
| `interface_declaration` | `NodeClass` (kind: "interface") |
| `import_statement` | Edge: `EdgeImports` |
| `call_expression` | Edge: `EdgeCalls` |
| `new_expression` | Edge: `EdgeUses` (instantiation) |

---

## Node ID Convention

Node IDs follow the SurrealDB record ID format:

```go
func makeNodeID(kind string, qualified string) string {
    // Replace dots and slashes with safe delimiters
    safe := strings.ReplaceAll(qualified, "/", "⋅")
    safe = strings.ReplaceAll(safe, ".", "⋅")
    return fmt.Sprintf("%s:%s", kind, safe)
}

// Examples:
// "function:pkg⋅Handler⋅ServeHTTP"
// "class:internal⋅domain⋅GraphStore"
// "file:internal⋅app⋅index_service⋅go"
// "module:internal⋅app"
```

---

## CGO Build Notes

tree-sitter requires CGO enabled:

```bash
CGO_ENABLED=1 go build -o commit0 .
```

Language grammars are C libraries linked via CGO. Ensure the build environment has a C compiler (gcc/clang).

---

## Checklist for Adding a New Language

1. [ ] Add grammar dependency: `go get github.com/smacker/go-tree-sitter/<language>`
2. [ ] Create extractor: `internal/adapters/treesitter/lang/<language>.go`
3. [ ] Implement `Extractor` interface with `Extract(root, file)` method
4. [ ] Map tree-sitter node types to commit0 `NodeKind` and `EdgeKind`
5. [ ] Register in `parser.go` languages map
6. [ ] Add language detection in `walker/fs_walker.go` (by extension)
7. [ ] Write unit tests with sample source files
8. [ ] Update `SupportedLanguages()` return value
