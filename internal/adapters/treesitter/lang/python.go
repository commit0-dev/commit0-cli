package lang

import (
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	pysitter "github.com/smacker/go-tree-sitter/python"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// PythonExtractor extracts code structure from Python source files.
type PythonExtractor struct{}

// Language returns the tree-sitter grammar for Python.
func (e *PythonExtractor) Language() *sitter.Language { return pysitter.GetLanguage() }

// Extract walks the AST and returns CodeNode/CodeEdge values.
//
// Node types:
//   - function_definition → NodeFunction
//   - class_definition    → NodeClass
//   - decorated_definition wrapping a function/class → unwrapped
//
// Edge types:
//   - import_statement / import_from_statement → EdgeImports
//   - call (inside function/class) → EdgeCalls
func (e *PythonExtractor) Extract(root *sitter.Node, file domain.FileEntry) ([]types.CodeNode, []types.CodeEdge) {
	src := file.Content
	moduleName := pyModuleFromPath(file.Path)

	var nodes []types.CodeNode
	var edges []types.CodeEdge

	type frame struct {
		node      *sitter.Node
		scopeQual string // qualified name of enclosing function or class
	}

	queue := []frame{{node: root, scopeQual: moduleName}}

	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		n := f.node
		scope := f.scopeQual

		// Unwrap decorated_definition to reach the inner function or class.
		inner := n
		if n.Type() == "decorated_definition" {
			inner = unwrapDecorated(n)
		}

		switch inner.Type() {

		// ── function_definition ───────────────────────────────────────────
		case "function_definition":
			fn := extractPyFunction(inner, src, file.Path, scope)
			if fn != nil {
				nodes = append(nodes, *fn)
				edges = append(edges, extractPyMutations(inner, src, file.Path, fn.Qualified)...)
				newScope := fn.Qualified
				for i := 0; i < int(inner.ChildCount()); i++ {
					queue = append(queue, frame{node: inner.Child(i), scopeQual: newScope})
				}
				continue
			}

		// ── class_definition ──────────────────────────────────────────────
		case "class_definition":
			cls, inheritEdges := extractPyClass(inner, src, file.Path, scope)
			if cls != nil {
				nodes = append(nodes, *cls)
				edges = append(edges, inheritEdges...)
				newScope := cls.Qualified
				for i := 0; i < int(inner.ChildCount()); i++ {
					queue = append(queue, frame{node: inner.Child(i), scopeQual: newScope})
				}
				continue
			}

		// ── import_statement ──────────────────────────────────────────────
		case "import_statement":
			edges = append(edges, extractPyImport(inner, src, file.Path)...)

		// ── import_from_statement ─────────────────────────────────────────
		case "import_from_statement":
			edges = append(edges, extractPyImportFrom(inner, src, file.Path)...)

		// ── call (function call expression) ───────────────────────────────
		case "call":
			if scope != moduleName { // only inside a function/class scope
				e := extractPyCall(inner, src, file.Path, scope)
				if e != nil {
					edges = append(edges, *e)
				}
			}
		}

		// Enqueue children unless we already did so above.
		for i := 0; i < int(n.ChildCount()); i++ {
			queue = append(queue, frame{node: n.Child(i), scopeQual: scope})
		}
	}

	return nodes, edges
}

// ── helpers ───────────────────────────────────────────────────────────────────

// unwrapDecorated returns the inner function_definition or class_definition
// inside a decorated_definition node.
func unwrapDecorated(n *sitter.Node) *sitter.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if c.Type() == "function_definition" || c.Type() == "class_definition" {
			return c
		}
	}
	return n
}

func extractPyFunction(n *sitter.Node, src []byte, filePath, scopeQual string) *types.CodeNode {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nodeText(nameNode, src)
	qualified := scopeQual + "." + name
	sig := extractPySignature(n, src)
	doc := extractPyDocstring(n, src)

	return &types.CodeNode{
		ID:         makeNodeID(string(types.NodeFunction), qualified),
		Kind:       types.NodeFunction,
		Name:       name,
		Qualified:  qualified,
		FilePath:   filePath,
		Language:   "python",
		StartLine:  int(n.StartPoint().Row) + 1,
		EndLine:    int(n.EndPoint().Row) + 1,
		Signature:  sig,
		Docstring:  doc,
		Body:       nodeText(n, src),
		Visibility: pyVisibility(name),
	}
}

func extractPyClass(n *sitter.Node, src []byte, filePath, scopeQual string) (*types.CodeNode, []types.CodeEdge) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return nil, nil
	}
	name := nodeText(nameNode, src)
	qualified := scopeQual + "." + name
	doc := extractPyDocstring(n, src)

	cls := &types.CodeNode{
		ID:         makeNodeID(string(types.NodeClass), qualified),
		Kind:       types.NodeClass,
		Name:       name,
		Qualified:  qualified,
		FilePath:   filePath,
		Language:   "python",
		StartLine:  int(n.StartPoint().Row) + 1,
		EndLine:    int(n.EndPoint().Row) + 1,
		Docstring:  doc,
		Body:       nodeText(n, src),
		Visibility: pyVisibility(name),
	}

	// Extract base classes → EdgeInherits
	var edges []types.CodeEdge
	argsNode := n.ChildByFieldName("superclasses")
	if argsNode != nil {
		for i := 0; i < int(argsNode.ChildCount()); i++ {
			base := argsNode.Child(i)
			if base.Type() == "identifier" || base.Type() == "attribute" {
				baseName := nodeText(base, src)
				edges = append(edges, types.CodeEdge{
					Kind:   types.EdgeInherits,
					FromID: cls.ID,
					ToID:   baseName, // resolver will attempt to qualify
				})
			}
		}
	}

	return cls, edges
}

func extractPyImport(n *sitter.Node, src []byte, filePath string) []types.CodeEdge {
	fileID := makeNodeID(string(types.NodeFile), filePath)
	var edges []types.CodeEdge
	// import a, b, c  — names are dotted_name or aliased_import children
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if c.Type() == "dotted_name" || c.Type() == "aliased_import" {
			modName := strings.TrimSpace(nodeText(c, src))
			if c.Type() == "aliased_import" {
				// aliased_import: <name> as <alias> — use the first child
				if c.ChildCount() > 0 {
					modName = nodeText(c.Child(0), src)
				}
			}
			edges = append(edges, types.CodeEdge{
				Kind:     types.EdgeImports,
				FromID:   fileID,
				ToID:     makeNodeID(string(types.NodeModule), modName),
				CallSite: fmt.Sprintf("%s:%d", filePath, n.StartPoint().Row+1),
			})
		}
	}
	return edges
}

func extractPyImportFrom(n *sitter.Node, src []byte, filePath string) []types.CodeEdge {
	fileID := makeNodeID(string(types.NodeFile), filePath)
	var edges []types.CodeEdge

	// from <module> import <names>
	moduleNode := n.ChildByFieldName("module_name")
	if moduleNode == nil {
		return nil
	}
	modName := nodeText(moduleNode, src)
	edges = append(edges, types.CodeEdge{
		Kind:     types.EdgeImports,
		FromID:   fileID,
		ToID:     makeNodeID(string(types.NodeModule), modName),
		CallSite: fmt.Sprintf("%s:%d", filePath, n.StartPoint().Row+1),
	})
	return edges
}

func extractPyCall(n *sitter.Node, src []byte, filePath, scopeQual string) *types.CodeEdge {
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

func extractPySignature(n *sitter.Node, src []byte) string {
	nameNode := n.ChildByFieldName("name")
	paramsNode := n.ChildByFieldName("parameters")
	returnNode := n.ChildByFieldName("return_type")

	var parts []string
	if nameNode != nil {
		parts = append(parts, nodeText(nameNode, src))
	}
	if paramsNode != nil {
		parts = append(parts, nodeText(paramsNode, src))
	}
	if returnNode != nil {
		parts = append(parts, "->", nodeText(returnNode, src))
	}
	return strings.Join(parts, " ")
}

// extractPyDocstring returns the first string literal in the function/class body.
func extractPyDocstring(n *sitter.Node, src []byte) string {
	bodyNode := n.ChildByFieldName("body")
	if bodyNode == nil {
		return ""
	}
	if bodyNode.ChildCount() == 0 {
		return ""
	}
	first := bodyNode.Child(0)
	if first == nil {
		return ""
	}
	// expression_statement containing a string
	if first.Type() == "expression_statement" && first.ChildCount() > 0 {
		s := first.Child(0)
		if s != nil && (s.Type() == "string" || s.Type() == "concatenated_string") {
			return strings.Trim(nodeText(s, src), `"'`)
		}
	}
	return ""
}

// pyVisibility: dunder/private start with _, otherwise public.
func pyVisibility(name string) string {
	if strings.HasPrefix(name, "_") {
		return "private"
	}
	return "public"
}

// extractPyMutations detects data mutations inside a Python function body.
// A mutation is `x = func(x)` or `self.field = transform(self.field)`.
func extractPyMutations(n *sitter.Node, src []byte, filePath, scopeQual string) []types.CodeEdge {
	body := n.ChildByFieldName("body")
	if body == nil {
		return nil
	}

	fromID := makeNodeID(string(types.NodeFunction), scopeQual)
	var edges []types.CodeEdge

	var walk func(node *sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}

		// Python assignment: target = value (type "assignment")
		if node.Type() == "assignment" {
			lhs := node.ChildByFieldName("left")
			rhs := node.ChildByFieldName("right")
			if lhs != nil && rhs != nil && rhs.Type() == "call" {
				lhsText := strings.TrimSpace(nodeText(lhs, src))
				rhsText := strings.TrimSpace(nodeText(rhs, src))
				calleeNode := rhs.ChildByFieldName("function")
				if calleeNode != nil && lhsText != "" {
					calleeText := strings.TrimSpace(nodeText(calleeNode, src))
					meta := map[string]string{
						"arg_expr":      lhsText,
						"mutation_type": string(types.MutationTransform),
						"mutation_expr": rhsText,
						"mutation_line": fmt.Sprintf("%d", node.StartPoint().Row+1),
					}
					// self.field → field_path
					if lhs.Type() == "attribute" {
						obj := lhs.ChildByFieldName("object")
						attr := lhs.ChildByFieldName("attribute")
						if obj != nil && attr != nil {
							meta["field_path"] = nodeText(obj, src) + "." + nodeText(attr, src)
						}
					}
					edges = append(edges, types.CodeEdge{
						Kind:     types.EdgeDataFlow,
						FromID:   fromID,
						ToID:     calleeText,
						CallSite: fmt.Sprintf("%s:%d", filePath, node.StartPoint().Row+1),
						Metadata: meta,
					})
				}
			}
		}

		for i := range int(node.ChildCount()) {
			walk(node.Child(i))
		}
	}

	walk(body)
	return edges
}

// e.g. "myapp/utils/helpers.py" → "myapp.utils.helpers".
func pyModuleFromPath(path string) string {
	path = strings.TrimSuffix(path, ".py")
	return strings.ReplaceAll(path, "/", ".")
}
