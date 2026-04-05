package lang

import (
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	tssitter "github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// TypeScriptExtractor extracts code structure from TypeScript source files.
type TypeScriptExtractor struct{}

// Language returns the tree-sitter grammar for TypeScript.
func (e *TypeScriptExtractor) Language() *sitter.Language { return tssitter.GetLanguage() }

// Extract walks the AST and returns CodeNode/CodeEdge values.
//
// Node types:
//   - function_declaration         → NodeFunction
//   - method_definition            → NodeFunction
//   - arrow_function (named, assigned to variable) → NodeFunction
//   - class_declaration            → NodeClass
//   - interface_declaration        → NodeClass (visibility: "interface")
//
// Edge types:
//   - import_statement             → EdgeImports
//   - call_expression              → EdgeCalls
//   - new_expression               → EdgeUses (instantiation)
func (e *TypeScriptExtractor) Extract(root *sitter.Node, file domain.FileEntry) ([]types.CodeNode, []types.CodeEdge) {
	return extractTS(root, file, "typescript")
}

// extractTS is the shared implementation used by both TypeScriptExtractor and
// JavaScriptExtractor, parameterised on the language tag.
func extractTS(root *sitter.Node, file domain.FileEntry, language string) ([]types.CodeNode, []types.CodeEdge) {
	src := file.Content
	moduleName := tsModuleFromPath(file.Path)

	var nodes []types.CodeNode
	var edges []types.CodeEdge

	type frame struct {
		node      *sitter.Node
		scopeQual string // enclosing function / class qualified name
		classQual string // enclosing class qualified name (for methods)
	}

	queue := []frame{{node: root, scopeQual: moduleName, classQual: ""}}

	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		n := f.node
		scope := f.scopeQual
		clsQual := f.classQual

		switch n.Type() {

		// ── function_declaration ───────────────────────────────────────────
		case "function_declaration":
			fn := extractTSFunction(n, src, file.Path, scope, language)
			if fn != nil {
				nodes = append(nodes, *fn)
				newScope := fn.Qualified
				for i := 0; i < int(n.ChildCount()); i++ {
					queue = append(queue, frame{n.Child(i), newScope, clsQual})
				}
				continue
			}

		// ── method_definition ─────────────────────────────────────────────
		case "method_definition":
			fn := extractTSMethod(n, src, file.Path, clsQual, language)
			if fn != nil {
				nodes = append(nodes, *fn)
				newScope := fn.Qualified
				for i := 0; i < int(n.ChildCount()); i++ {
					queue = append(queue, frame{n.Child(i), newScope, clsQual})
				}
				continue
			}

		// ── lexical_declaration / variable_declaration (named arrow fn) ───
		case "lexical_declaration", "variable_declaration":
			fn := extractTSArrowFunction(n, src, file.Path, scope, language)
			if fn != nil {
				nodes = append(nodes, *fn)
				// Don't recurse into the declarator body here; it was captured.
				continue
			}

		// ── class_declaration ─────────────────────────────────────────────
		case "class_declaration":
			cls, inheritEdges := extractTSClass(n, src, file.Path, scope, language, false)
			if cls != nil {
				nodes = append(nodes, *cls)
				edges = append(edges, inheritEdges...)
				for i := 0; i < int(n.ChildCount()); i++ {
					queue = append(queue, frame{n.Child(i), scope, cls.Qualified})
				}
				continue
			}

		// ── interface_declaration ─────────────────────────────────────────
		case "interface_declaration":
			cls, inheritEdges := extractTSClass(n, src, file.Path, scope, language, true)
			if cls != nil {
				nodes = append(nodes, *cls)
				edges = append(edges, inheritEdges...)
				for i := 0; i < int(n.ChildCount()); i++ {
					queue = append(queue, frame{n.Child(i), scope, cls.Qualified})
				}
				continue
			}

		// ── import_statement ──────────────────────────────────────────────
		case "import_statement":
			edges = append(edges, extractTSImport(n, src, file.Path)...)

		// ── call_expression ───────────────────────────────────────────────
		case "call_expression":
			e := extractTSCall(n, src, file.Path, scope)
			if e != nil {
				edges = append(edges, *e)
			}

		// ── new_expression ────────────────────────────────────────────────
		case "new_expression":
			e := extractTSNew(n, src, file.Path, scope)
			if e != nil {
				edges = append(edges, *e)
			}
		}

		// Default: enqueue all children.
		for i := 0; i < int(n.ChildCount()); i++ {
			queue = append(queue, frame{n.Child(i), scope, clsQual})
		}
	}

	return nodes, edges
}

// ── node extractors ───────────────────────────────────────────────────────────

func extractTSFunction(n *sitter.Node, src []byte, filePath, scopeQual, language string) *types.CodeNode {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nodeText(nameNode, src)
	qualified := scopeQual + "." + name
	return &types.CodeNode{
		ID:         makeNodeID(string(types.NodeFunction), qualified),
		Kind:       types.NodeFunction,
		Name:       name,
		Qualified:  qualified,
		FilePath:   filePath,
		Language:   language,
		StartLine:  int(n.StartPoint().Row) + 1,
		EndLine:    int(n.EndPoint().Row) + 1,
		Signature:  extractTSSignature(n, src),
		Body:       nodeText(n, src),
		Visibility: tsVisibility(n, src),
	}
}

func extractTSMethod(n *sitter.Node, src []byte, filePath, classQual, language string) *types.CodeNode {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nodeText(nameNode, src)
	parent := classQual
	if parent == "" {
		parent = "unknown"
	}
	qualified := parent + "." + name
	return &types.CodeNode{
		ID:         makeNodeID(string(types.NodeFunction), qualified),
		Kind:       types.NodeFunction,
		Name:       name,
		Qualified:  qualified,
		FilePath:   filePath,
		Language:   language,
		StartLine:  int(n.StartPoint().Row) + 1,
		EndLine:    int(n.EndPoint().Row) + 1,
		Signature:  extractTSSignature(n, src),
		Body:       nodeText(n, src),
		Visibility: tsVisibility(n, src),
	}
}

// extractTSArrowFunction detects patterns like:
//
//	const myFn = (x) => { ... }
//	let myFn = function(x) { ... }
func extractTSArrowFunction(n *sitter.Node, src []byte, filePath, scopeQual, language string) *types.CodeNode {
	// n is lexical_declaration or variable_declaration.
	// Look for a variable_declarator child whose value is arrow_function or function.
	for i := 0; i < int(n.ChildCount()); i++ {
		decl := n.Child(i)
		if decl.Type() != "variable_declarator" {
			continue
		}
		nameNode := decl.ChildByFieldName("name")
		valNode := decl.ChildByFieldName("value")
		if nameNode == nil || valNode == nil {
			continue
		}
		vt := valNode.Type()
		if vt != "arrow_function" && vt != "function" && vt != "function_expression" {
			continue
		}
		name := nodeText(nameNode, src)
		qualified := scopeQual + "." + name
		return &types.CodeNode{
			ID:         makeNodeID(string(types.NodeFunction), qualified),
			Kind:       types.NodeFunction,
			Name:       name,
			Qualified:  qualified,
			FilePath:   filePath,
			Language:   language,
			StartLine:  int(n.StartPoint().Row) + 1,
			EndLine:    int(n.EndPoint().Row) + 1,
			Signature:  name + extractTSSignature(valNode, src),
			Body:       nodeText(valNode, src),
			Visibility: tsVisibility(n, src),
		}
	}
	return nil
}

func extractTSClass(n *sitter.Node, src []byte, filePath, scopeQual, language string, isInterface bool) (*types.CodeNode, []types.CodeEdge) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return nil, nil
	}
	name := nodeText(nameNode, src)
	qualified := scopeQual + "." + name
	visibility := tsVisibility(n, src)
	if isInterface {
		visibility = "interface"
	}

	cls := &types.CodeNode{
		ID:         makeNodeID(string(types.NodeClass), qualified),
		Kind:       types.NodeClass,
		Name:       name,
		Qualified:  qualified,
		FilePath:   filePath,
		Language:   language,
		StartLine:  int(n.StartPoint().Row) + 1,
		EndLine:    int(n.EndPoint().Row) + 1,
		Body:       nodeText(n, src),
		Visibility: visibility,
	}

	// Extract extends / implements clauses → EdgeInherits.
	// The grammar nests: class_heritage → extends_clause/implements_clause → identifier.
	var edges []types.CodeEdge
	var collectBases func(node *sitter.Node)
	collectBases = func(node *sitter.Node) {
		for i := 0; i < int(node.ChildCount()); i++ {
			c := node.Child(i)
			switch c.Type() {
			case "class_heritage", "extends_clause", "implements_clause":
				collectBases(c)
			case "identifier", "member_expression", "type_identifier":
				baseName := nodeText(c, src)
				edges = append(edges, types.CodeEdge{
					Kind:   types.EdgeInherits,
					FromID: cls.ID,
					ToID:   baseName,
				})
			}
		}
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if c.Type() == "class_heritage" {
			collectBases(c)
		}
	}

	return cls, edges
}

func extractTSImport(n *sitter.Node, src []byte, filePath string) []types.CodeEdge {
	fileID := makeNodeID(string(types.NodeFile), filePath)
	// import ... from '<source>'
	sourceNode := n.ChildByFieldName("source")
	if sourceNode == nil {
		return nil
	}
	importPath := strings.Trim(nodeText(sourceNode, src), `"'`)
	return []types.CodeEdge{{
		Kind:     types.EdgeImports,
		FromID:   fileID,
		ToID:     makeNodeID(string(types.NodeModule), importPath),
		CallSite: fmt.Sprintf("%s:%d", filePath, n.StartPoint().Row+1),
	}}
}

func extractTSCall(n *sitter.Node, src []byte, filePath, scopeQual string) *types.CodeEdge {
	fnNode := n.ChildByFieldName("function")
	if fnNode == nil {
		return nil
	}
	callee := strings.TrimSpace(nodeText(fnNode, src))
	if callee == "" {
		return nil
	}
	return &types.CodeEdge{
		Kind:     types.EdgeCalls,
		FromID:   makeNodeID(string(types.NodeFunction), scopeQual),
		ToID:     callee,
		CallSite: fmt.Sprintf("%s:%d", filePath, n.StartPoint().Row+1),
		CallType: "direct",
	}
}

func extractTSNew(n *sitter.Node, src []byte, filePath, scopeQual string) *types.CodeEdge {
	constructorNode := n.ChildByFieldName("constructor")
	if constructorNode == nil {
		return nil
	}
	target := strings.TrimSpace(nodeText(constructorNode, src))
	if target == "" {
		return nil
	}
	return &types.CodeEdge{
		Kind:     types.EdgeUses,
		FromID:   makeNodeID(string(types.NodeFunction), scopeQual),
		ToID:     target,
		CallSite: fmt.Sprintf("%s:%d", filePath, n.StartPoint().Row+1),
		CallType: "instantiation",
	}
}

// extractTSSignature builds a compact signature from a function-like node.
func extractTSSignature(n *sitter.Node, src []byte) string {
	paramsNode := n.ChildByFieldName("parameters")
	returnNode := n.ChildByFieldName("return_type")
	var parts []string
	if paramsNode != nil {
		parts = append(parts, nodeText(paramsNode, src))
	}
	if returnNode != nil {
		parts = append(parts, ":", nodeText(returnNode, src))
	}
	return strings.Join(parts, " ")
}

// tsVisibility checks for export keyword or accessibility modifiers.
func tsVisibility(n *sitter.Node, src []byte) string {
	// Check if the node itself or an ancestor lexical_declaration has "export".
	cur := n
	for cur != nil {
		for i := 0; i < int(cur.ChildCount()); i++ {
			c := cur.Child(i)
			switch c.Type() {
			case "export":
				return "public"
			case "accessibility_modifier":
				// e.g. public, private, protected
				return nodeText(c, src)
			}
			if nodeText(c, src) == "export" {
				return "public"
			}
		}
		cur = cur.Parent()
		if cur == nil || cur.Type() == "program" || cur.Type() == "class_body" {
			break
		}
	}
	return "private"
}

// e.g. "src/services/auth.ts" → "src.services.auth".
func tsModuleFromPath(path string) string {
	path = strings.TrimSuffix(path, ".ts")
	path = strings.TrimSuffix(path, ".tsx")
	path = strings.TrimSuffix(path, ".js")
	path = strings.TrimSuffix(path, ".jsx")
	return strings.ReplaceAll(path, "/", ".")
}
