package lang

import (
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	gositter "github.com/smacker/go-tree-sitter/golang"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// GoExtractor extracts code structure from Go source files.
type GoExtractor struct{}

// Language returns the tree-sitter grammar for Go.
func (e *GoExtractor) Language() *sitter.Language { return gositter.GetLanguage() }

// Extract performs a BFS walk over the AST root and returns all
// CodeNode and CodeEdge values found in the file.
//
// Node types extracted:
//   - function_declaration  → NodeFunction
//   - method_declaration    → NodeFunction (qualified: ReceiverType.MethodName)
//   - type_spec (struct/interface body) → NodeClass
//
// Edge types extracted:
//   - import_declaration / import_spec → EdgeImports
//   - call_expression inside functions/methods → EdgeCalls
func (e *GoExtractor) Extract(root *sitter.Node, file domain.FileEntry) ([]types.CodeNode, []types.CodeEdge) {
	src := file.Content

	// Derive the Go package name from the file path (last non-file segment).
	pkgName := goPackageFromPath(file.Path)

	var nodes []types.CodeNode
	var edges []types.CodeEdge

	// We track the "current scope" qualified name so call edges can reference it.
	// Because BFS mixes levels, we use a simple stack instead.
	type frame struct {
		node        *sitter.Node
		scopeQual   string // qualified name of the function/method we are inside
		isLHSAssign bool   // true when inside the left side of an assignment
	}

	queue := []frame{{node: root, scopeQual: ""}}

	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		n := f.node
		scope := f.scopeQual

		switch n.Type() {

		// ── function_declaration ───────────────────────────────────────────
		case "function_declaration":
			fn, fnEdges := extractGoFunction(n, src, file.Path, pkgName)
			if fn != nil {
				nodes = append(nodes, *fn)
				edges = append(edges, fnEdges...)
				// Descend into function body with updated scope.
				scope = fn.Qualified
				for i := 0; i < int(n.ChildCount()); i++ {
					queue = append(queue, frame{node: n.Child(i), scopeQual: scope})
				}
				continue
			}

		// ── method_declaration ─────────────────────────────────────────────
		case "method_declaration":
			fn, fnEdges := extractGoMethod(n, src, file.Path, pkgName)
			if fn != nil {
				nodes = append(nodes, *fn)
				edges = append(edges, fnEdges...)
				scope = fn.Qualified
				for i := 0; i < int(n.ChildCount()); i++ {
					queue = append(queue, frame{node: n.Child(i), scopeQual: scope})
				}
				continue
			}

		// ── type_spec (struct or interface) ───────────────────────────────
		case "type_spec":
			cls := extractGoTypeSpec(n, src, file.Path, pkgName)
			if cls != nil {
				nodes = append(nodes, *cls)
				// Still descend (methods may be nearby at package level).
			}

		// ── import_declaration ─────────────────────────────────────────────
		case "import_declaration":
			imports := extractGoImports(n, src, file.Path)
			nodes = append(nodes, imports.Nodes...)
			edges = append(edges, imports.Edges...)

		// ── call_expression ────────────────────────────────────────────────
		case "call_expression":
			if scope != "" {
				callEdge := extractGoCall(n, src, file.Path, scope)
				if callEdge != nil {
					edges = append(edges, *callEdge)
					// Emit data-flow edges for non-trivial arguments.
					edges = append(edges, extractGoDataFlow(n, src, file.Path, scope)...)
				}
			}

		// ── assignment / short-var — track LHS for read/write classification ─
		case "assignment_statement", "short_var_declaration":
			leftNode := n.ChildByFieldName("left")
			for i := range int(n.ChildCount()) {
				child := n.Child(i)
				isLHS := leftNode != nil && child == leftNode
				queue = append(queue, frame{node: child, scopeQual: scope, isLHSAssign: isLHS})
			}
			continue

		// ── selector_expression — field reads and writes ───────────────────
		case "selector_expression":
			if scope != "" {
				// Skip selectors that are the callee of a call_expression —
				// those represent method dispatch, not data access.
				if p := n.Parent(); p != nil && p.Type() == "call_expression" {
					if fn := p.ChildByFieldName("function"); fn == n {
						break
					}
				}
				operandN := n.ChildByFieldName("operand")
				fieldN := n.ChildByFieldName("field")
				if operandN != nil && fieldN != nil {
					operandText := nodeText(operandN, src)
					fieldText := nodeText(fieldN, src)
					qualField := operandText + "." + fieldText
					kind := types.EdgeReads
					if f.isLHSAssign {
						kind = types.EdgeWrites
					}
					edges = append(edges, types.CodeEdge{
						Kind:     kind,
						FromID:   makeNodeID(string(types.NodeFunction), scope),
						ToID:     makeNodeID(string(types.NodeClass), operandText),
						CallSite: fmt.Sprintf("%s:%d", file.Path, n.StartPoint().Row+1),
						Metadata: map[string]string{"field": qualField},
					})
				}
			}
		}

		// Default: enqueue all children, propagating both scope and LHS context.
		for i := range int(n.ChildCount()) {
			queue = append(queue, frame{node: n.Child(i), scopeQual: scope, isLHSAssign: f.isLHSAssign})
		}
	}

	return nodes, edges
}

// ── helpers ───────────────────────────────────────────────────────────────────

func extractGoFunction(n *sitter.Node, src []byte, filePath, pkgName string) (*types.CodeNode, []types.CodeEdge) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return nil, nil
	}
	name := nodeText(nameNode, src)
	qualified := pkgName + "." + name
	sig := extractGoSignature(n, src)
	doc := extractGoDocComment(n, src)

	node := &types.CodeNode{
		ID:         makeNodeID(string(types.NodeFunction), qualified),
		Kind:       types.NodeFunction,
		Name:       name,
		Qualified:  qualified,
		FilePath:   filePath,
		Language:   "go",
		StartLine:  int(n.StartPoint().Row) + 1,
		EndLine:    int(n.EndPoint().Row) + 1,
		Signature:  sig,
		Docstring:  doc,
		Body:       nodeText(n, src),
		Visibility: goVisibility(name),
	}
	return node, nil
}

func extractGoMethod(n *sitter.Node, src []byte, filePath, pkgName string) (*types.CodeNode, []types.CodeEdge) {
	nameNode := n.ChildByFieldName("name")
	receiverNode := n.ChildByFieldName("receiver")
	if nameNode == nil {
		return nil, nil
	}
	name := nodeText(nameNode, src)
	receiverType := extractGoReceiverType(receiverNode, src)
	qualified := pkgName + "." + receiverType + "." + name
	sig := extractGoSignature(n, src)
	doc := extractGoDocComment(n, src)

	node := &types.CodeNode{
		ID:         makeNodeID(string(types.NodeFunction), qualified),
		Kind:       types.NodeFunction,
		Name:       name,
		Qualified:  qualified,
		FilePath:   filePath,
		Language:   "go",
		StartLine:  int(n.StartPoint().Row) + 1,
		EndLine:    int(n.EndPoint().Row) + 1,
		Signature:  sig,
		Docstring:  doc,
		Body:       nodeText(n, src),
		Visibility: goVisibility(name),
	}
	return node, nil
}

func extractGoTypeSpec(n *sitter.Node, src []byte, filePath, pkgName string) *types.CodeNode {
	nameNode := n.ChildByFieldName("name")
	typeNode := n.ChildByFieldName("type")
	if nameNode == nil || typeNode == nil {
		return nil
	}
	t := typeNode.Type()
	if t != "struct_type" && t != "interface_type" {
		return nil
	}
	name := nodeText(nameNode, src)
	qualified := pkgName + "." + name
	return &types.CodeNode{
		ID:         makeNodeID(string(types.NodeClass), qualified),
		Kind:       types.NodeClass,
		Name:       name,
		Qualified:  qualified,
		FilePath:   filePath,
		Language:   "go",
		StartLine:  int(n.StartPoint().Row) + 1,
		EndLine:    int(n.EndPoint().Row) + 1,
		Body:       nodeText(n, src),
		Visibility: goVisibility(name),
	}
}

// goImportResult holds module nodes and import edges extracted from an import block.
type goImportResult struct {
	Nodes []types.CodeNode
	Edges []types.CodeEdge
}

func extractGoImports(n *sitter.Node, src []byte, filePath string) goImportResult {
	var result goImportResult
	fileID := makeNodeID(string(types.NodeFile), filePath)
	seen := make(map[string]bool) // deduplicate modules within one file

	// Walk children looking for import_spec nodes.
	queue := []*sitter.Node{n}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.Type() == "import_spec" {
			pathNode := cur.ChildByFieldName("path")
			if pathNode != nil {
				importPath := strings.Trim(nodeText(pathNode, src), `"`)
				moduleID := makeNodeID(string(types.NodeModule), importPath)

				result.Edges = append(result.Edges, types.CodeEdge{
					Kind:     types.EdgeImports,
					FromID:   fileID,
					ToID:     moduleID,
					CallSite: fmt.Sprintf("%s:%d", filePath, cur.StartPoint().Row+1),
				})

				// Create module node (deduplicated within this file).
				if !seen[importPath] {
					seen[importPath] = true
					result.Nodes = append(result.Nodes, types.CodeNode{
						ID:        moduleID,
						Kind:      types.NodeModule,
						Name:      goModuleName(importPath),
						Qualified: importPath,
						FilePath:  importPath,
						Language:  "go",
					})
				}
			}
		}
		for i := 0; i < int(cur.ChildCount()); i++ {
			queue = append(queue, cur.Child(i))
		}
	}
	return result
}

// goModuleName returns the last path segment of an import path.
// e.g. "golang.org/x/sync/errgroup" → "errgroup", "fmt" → "fmt".
func goModuleName(importPath string) string {
	if idx := strings.LastIndex(importPath, "/"); idx >= 0 {
		return importPath[idx+1:]
	}
	return importPath
}

func extractGoCall(n *sitter.Node, src []byte, filePath, scopeQual string) *types.CodeEdge {
	fnNode := n.ChildByFieldName("function")
	if fnNode == nil {
		return nil
	}
	callee := strings.TrimSpace(nodeText(fnNode, src))
	if callee == "" {
		return nil
	}

	callType := detectGoCallType(n)
	return &types.CodeEdge{
		Kind:      types.EdgeCalls,
		FromID:    makeNodeID(string(types.NodeFunction), scopeQual),
		ToID:      callee, // resolver will attempt to qualify this
		CallSite:  fmt.Sprintf("%s:%d", filePath, n.StartPoint().Row+1),
		CallType:  callType,
		IsDynamic: callType == "interface",
	}
}

// extractGoDataFlow emits EdgeDataFlow edges for each non-literal argument
// passed at a call site. param_name is left empty because callee parameter
// names require cross-file resolution; arg_expr carries the source expression.
func extractGoDataFlow(n *sitter.Node, src []byte, filePath, scopeQual string) []types.CodeEdge {
	fnNode := n.ChildByFieldName("function")
	argsNode := n.ChildByFieldName("arguments")
	if fnNode == nil || argsNode == nil {
		return nil
	}
	callee := strings.TrimSpace(nodeText(fnNode, src))
	if callee == "" {
		return nil
	}

	fromID := makeNodeID(string(types.NodeFunction), scopeQual)
	callSite := fmt.Sprintf("%s:%d", filePath, n.StartPoint().Row+1)

	var edges []types.CodeEdge
	for i := range int(argsNode.ChildCount()) {
		arg := argsNode.Child(i)
		// Skip punctuation tokens.
		if t := arg.Type(); t == "," || t == "(" || t == ")" {
			continue
		}
		// Skip plain literals — they carry no meaningful data-flow information.
		if isGoLiteral(arg) {
			continue
		}
		argText := strings.TrimSpace(nodeText(arg, src))
		if argText == "" {
			continue
		}
		edges = append(edges, types.CodeEdge{
			Kind:     types.EdgeDataFlow,
			FromID:   fromID,
			ToID:     callee,
			CallSite: callSite,
			Metadata: map[string]string{"arg_expr": argText},
		})
	}
	return edges
}

// isGoLiteral reports whether a node is a compile-time constant (integer,
// float, string, bool, nil). Data-flow edges for literals are not useful.
func isGoLiteral(n *sitter.Node) bool {
	switch n.Type() {
	case "int_literal", "float_literal", "imaginary_literal",
		"rune_literal", "interpreted_string_literal", "raw_string_literal",
		"true", "false", "nil":
		return true
	}
	return false
}

// detectGoCallType inspects parent nodes to classify the call.
func detectGoCallType(n *sitter.Node) string {
	p := n.Parent()
	if p == nil {
		return "direct"
	}
	switch p.Type() {
	case "go_statement":
		return "goroutine"
	case "defer_statement":
		return "deferred"
	default:
		return "direct"
	}
}

// extractGoSignature builds "funcName(params) returnTypes" from a function node.
func extractGoSignature(n *sitter.Node, src []byte) string {
	nameNode := n.ChildByFieldName("name")
	paramsNode := n.ChildByFieldName("parameters")
	resultNode := n.ChildByFieldName("result")

	var parts []string
	if nameNode != nil {
		parts = append(parts, nodeText(nameNode, src))
	}
	if paramsNode != nil {
		parts = append(parts, nodeText(paramsNode, src))
	}
	if resultNode != nil {
		parts = append(parts, nodeText(resultNode, src))
	}
	return strings.Join(parts, " ")
}

// extractGoReceiverType extracts the base type name from a receiver field list.
func extractGoReceiverType(receiverNode *sitter.Node, src []byte) string {
	if receiverNode == nil {
		return "unknown"
	}
	// receiver is a parameter_list; its first parameter_declaration has a type.
	queue := []*sitter.Node{receiverNode}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		t := cur.Type()
		if t == "pointer_type" {
			// Descend into pointer to get the named type.
			for i := 0; i < int(cur.ChildCount()); i++ {
				queue = append(queue, cur.Child(i))
			}
			continue
		}
		if t == "type_identifier" {
			return nodeText(cur, src)
		}
		for i := 0; i < int(cur.ChildCount()); i++ {
			queue = append(queue, cur.Child(i))
		}
	}
	return "unknown"
}

// extractGoDocComment looks for a comment node immediately preceding n.
func extractGoDocComment(n *sitter.Node, src []byte) string {
	prev := n.PrevNamedSibling()
	if prev != nil && prev.Type() == "comment" {
		return strings.TrimSpace(nodeText(prev, src))
	}
	return ""
}

// goVisibility returns "public" if name starts with uppercase, else "private".
func goVisibility(name string) string {
	if name == "" {
		return "private"
	}
	r := rune(name[0])
	if r >= 'A' && r <= 'Z' {
		return "public"
	}
	return "private"
}

// e.g. "internal/adapters/surreal/client.go" → "surreal".
func goPackageFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	if len(parts) == 1 {
		// root-level file: use the filename without extension
		name := parts[0]
		if idx := strings.LastIndex(name, "."); idx >= 0 {
			return name[:idx]
		}
		return name
	}
	return "main"
}

// makeNodeID is mirrored from the parent package (avoids circular dependency).
func makeNodeID(kind string, qualified string) string {
	safe := strings.ReplaceAll(qualified, "/", "⋅")
	safe = strings.ReplaceAll(safe, ".", "⋅")
	return fmt.Sprintf("%s:%s", kind, safe)
}

// nodeText returns the source text for a tree-sitter node.
func nodeText(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	start := n.StartByte()
	end := min(n.EndByte(), uint32(len(src)))
	return string(src[start:end])
}
