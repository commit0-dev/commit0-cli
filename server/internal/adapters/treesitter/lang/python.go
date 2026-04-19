package lang

import (
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	pysitter "github.com/smacker/go-tree-sitter/python"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
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

		// Extract route decorators before unwrapping.
		if n.Type() == "decorated_definition" {
			edges = append(edges, extractPyRoutes(n, src, file.Path, scope)...)
		}

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
				edges = append(edges, extractPyReturnFlows(inner, src, file.Path, fn.Qualified)...)
				edges = append(edges, extractPyCFG(inner, src, file.Path, fn.Qualified)...)
				edges = append(edges, extractPyDataDep(inner, src, file.Path, fn.Qualified)...)
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

// pyHTTPMethods maps decorator method names to HTTP methods for Flask/FastAPI.
var pyHTTPMethods = map[string]string{
	"route": "", // method from methods= kwarg
	"get":   "GET", "post": "POST", "put": "PUT",
	"delete": "DELETE", "patch": "PATCH", "head": "HEAD", "options": "OPTIONS",
}

// extractPyRoutes detects HTTP route registrations from Flask/FastAPI decorators.
// Patterns detected:
//   - @app.route("/path", methods=["GET"])
//   - @app.get("/path"), @app.post("/path")
//   - @bp.get("/path"), @router.post("/path")
//
// The decorated_definition node must be passed (before unwrapping).
// Returns route edges and the handler qualified name (if detected).
func extractPyRoutes(n *sitter.Node, src []byte, filePath, scopeQual string) []types.CodeEdge {
	if n.Type() != "decorated_definition" {
		return nil
	}

	fileID := makeNodeID(string(types.NodeFile), filePath)
	var edges []types.CodeEdge

	// Walk children looking for decorator nodes
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		if child.Type() != "decorator" {
			continue
		}

		// The decorator contains an expression — typically a call or attribute
		// @app.get("/path") is: decorator → call → attribute (app.get) + arguments ("/path")
		// @app.route("/path", methods=["GET"]) is similar
		for j := range int(child.ChildCount()) {
			expr := child.Child(j)
			if expr.Type() != "call" {
				continue
			}

			fnNode := expr.ChildByFieldName("function")
			if fnNode == nil || fnNode.Type() != "attribute" {
				continue
			}

			methodNode := fnNode.ChildByFieldName("attribute")
			if methodNode == nil {
				continue
			}

			methodName := strings.ToLower(nodeText(methodNode, src))
			httpMethod, isRoute := pyHTTPMethods[methodName]
			if !isRoute {
				continue
			}

			// Extract path from first argument
			argsNode := expr.ChildByFieldName("arguments")
			path := ""
			if argsNode != nil {
				for k := range int(argsNode.ChildCount()) {
					arg := argsNode.Child(k)
					if t := arg.Type(); t == "," || t == "(" || t == ")" {
						continue
					}
					// First positional string arg is the path
					if arg.Type() == "string" || arg.Type() == "concatenated_string" {
						path = strings.Trim(nodeText(arg, src), `"'`)
						break
					}
				}
			}

			if path == "" {
				continue
			}

			// For @app.route, try to extract methods= kwarg
			if methodName == "route" && argsNode != nil {
				httpMethod = extractPyMethodsKwarg(argsNode, src)
				if httpMethod == "" {
					httpMethod = "ANY"
				}
			}

			// Find the handler function name
			inner := unwrapDecorated(n)
			handlerName := ""
			if nameNode := inner.ChildByFieldName("name"); nameNode != nil {
				handlerName = scopeQual + "." + nodeText(nameNode, src)
			}
			if handlerName == "" {
				continue
			}

			meta := map[string]string{
				"http_method": httpMethod,
				"http_path":   path,
			}

			edges = append(edges, types.CodeEdge{
				Kind:     types.EdgeRoute,
				FromID:   fileID,
				ToID:     handlerName,
				CallSite: fmt.Sprintf("%s:%d", filePath, child.StartPoint().Row+1),
				Metadata: meta,
			})
		}
	}

	return edges
}

// extractPyMethodsKwarg extracts the HTTP method from a methods= keyword argument.
// e.g., methods=["GET", "POST"] → "GET,POST" or methods=["GET"] → "GET".
func extractPyMethodsKwarg(argsNode *sitter.Node, src []byte) string {
	for i := range int(argsNode.ChildCount()) {
		arg := argsNode.Child(i)
		if arg.Type() == "keyword_argument" {
			nameNode := arg.ChildByFieldName("name")
			if nameNode != nil && nodeText(nameNode, src) == "methods" {
				valueNode := arg.ChildByFieldName("value")
				if valueNode != nil {
					// Extract method names from list: ["GET", "POST"]
					var methods []string
					for j := range int(valueNode.ChildCount()) {
						item := valueNode.Child(j)
						if item.Type() == "string" {
							m := strings.Trim(nodeText(item, src), `"'`)
							if m != "" {
								methods = append(methods, strings.ToUpper(m))
							}
						}
					}
					return strings.Join(methods, ",")
				}
			}
		}
	}
	return ""
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

// ── Control Flow Graph (CFG) extraction ──────────────────────────────────────

// extractPyCFG builds intra-procedural control flow edges for a Python function.
func extractPyCFG(n *sitter.Node, src []byte, filePath, scopeQual string) []types.CodeEdge {
	body := n.ChildByFieldName("body")
	if body == nil {
		return nil
	}

	fnID := makeNodeID(string(types.NodeFunction), scopeQual)
	var edges []types.CodeEdge

	cfgEdge := func(fromLine, toLine int, branchType, condition string) {
		meta := map[string]string{
			"from_line":   fmt.Sprintf("%d", fromLine),
			"to_line":     fmt.Sprintf("%d", toLine),
			"branch_type": branchType,
		}
		if condition != "" {
			meta["condition"] = condition
		}
		edges = append(edges, types.CodeEdge{
			Kind:     types.EdgeControlFlow,
			FromID:   fnID,
			ToID:     fnID,
			CallSite: fmt.Sprintf("%s:%d", filePath, fromLine),
			Metadata: meta,
		})
	}

	var walkBlock func(block *sitter.Node)
	walkBlock = func(block *sitter.Node) {
		if block == nil {
			return
		}
		stmts := pyDirectStatements(block)
		for i, stmt := range stmts {
			line := int(stmt.StartPoint().Row) + 1

			if i > 0 {
				prevLine := int(stmts[i-1].StartPoint().Row) + 1
				if stmts[i-1].Type() != "return_statement" {
					cfgEdge(prevLine, line, "sequential", "")
				}
			}

			switch stmt.Type() {
			case "if_statement":
				cond := stmt.ChildByFieldName("condition")
				condText := ""
				if cond != nil {
					condText = strings.TrimSpace(nodeText(cond, src))
				}

				consequence := stmt.ChildByFieldName("consequence")
				if consequence != nil {
					firstTrue := pyFirstStmtLine(consequence)
					if firstTrue > 0 {
						cfgEdge(line, firstTrue, "if_true", condText)
					}
					walkBlock(consequence)
				}

				// Look for elif/else clauses.
				for j := range int(stmt.ChildCount()) {
					child := stmt.Child(j)
					if child.Type() == "elif_clause" || child.Type() == "else_clause" {
						firstFalse := pyFirstStmtLine(child)
						if firstFalse > 0 {
							cfgEdge(line, firstFalse, "if_false", condText)
						}
						walkBlock(child)
					}
				}

			case "for_statement", "while_statement":
				bodyNode := stmt.ChildByFieldName("body")
				if bodyNode != nil {
					firstBody := pyFirstStmtLine(bodyNode)
					if firstBody > 0 {
						cfgEdge(line, firstBody, "loop_entry", "")
					}
					lastBody := pyLastStmtLine(bodyNode)
					if lastBody > 0 {
						cfgEdge(lastBody, line, "loop_back", "")
					}
					walkBlock(bodyNode)
				}

			case "return_statement":
				cfgEdge(line, line, "return", "")

			case "try_statement":
				for j := range int(stmt.ChildCount()) {
					child := stmt.Child(j)
					if child.Type() == "block" || child.Type() == "except_clause" || child.Type() == "finally_clause" {
						walkBlock(child)
					}
				}
			}
		}
	}

	walkBlock(body)
	return edges
}

// extractPyDataDep builds intra-procedural data dependence edges for a Python function.
func extractPyDataDep(n *sitter.Node, src []byte, filePath, scopeQual string) []types.CodeEdge {
	body := n.ChildByFieldName("body")
	if body == nil {
		return nil
	}

	fnID := makeNodeID(string(types.NodeFunction), scopeQual)

	type varDef struct {
		name    string
		line    int
		defType string
	}

	var defs []varDef

	// Collect parameter definitions.
	params := n.ChildByFieldName("parameters")
	if params != nil {
		for i := range int(params.ChildCount()) {
			child := params.Child(i)
			if child.Type() == "identifier" {
				name := nodeText(child, src)
				if name != "self" && name != "cls" && name != "_" {
					defs = append(defs, varDef{name: name, line: int(child.StartPoint().Row) + 1, defType: "parameter"})
				}
			}
		}
	}

	// Collect assignment definitions.
	var collectDefs func(node *sitter.Node)
	collectDefs = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if node.Type() == "assignment" {
			lhs := node.ChildByFieldName("left")
			rhs := node.ChildByFieldName("right")
			defType := "assignment"
			if rhs != nil && rhs.Type() == "call" {
				defType = "return_value"
			}
			if lhs != nil && lhs.Type() == "identifier" {
				name := nodeText(lhs, src)
				if name != "_" {
					defs = append(defs, varDef{name: name, line: int(node.StartPoint().Row) + 1, defType: defType})
				}
			}
		}
		if node.Type() == "for_statement" {
			left := node.ChildByFieldName("left")
			if left != nil && left.Type() == "identifier" {
				name := nodeText(left, src)
				if name != "_" {
					defs = append(defs, varDef{name: name, line: int(node.StartPoint().Row) + 1, defType: "for_range"})
				}
			}
		}
		for i := range int(node.ChildCount()) {
			collectDefs(node.Child(i))
		}
	}
	collectDefs(body)

	if len(defs) == 0 {
		return nil
	}

	defMap := make(map[string]varDef)
	for _, d := range defs {
		defMap[d.name] = d
	}

	var edges []types.CodeEdge
	var collectUses func(node *sitter.Node)
	collectUses = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if node.Type() == "call" {
			argsNode := node.ChildByFieldName("arguments")
			if argsNode != nil {
				for i := range int(argsNode.ChildCount()) {
					arg := argsNode.Child(i)
					if t := arg.Type(); t == "," || t == "(" || t == ")" {
						continue
					}
					varName := ""
					if arg.Type() == "identifier" {
						varName = nodeText(arg, src)
					} else if arg.Type() == "attribute" {
						obj := arg.ChildByFieldName("object")
						if obj != nil && obj.Type() == "identifier" {
							varName = nodeText(obj, src)
						}
					}
					if varName == "" {
						continue
					}
					if def, ok := defMap[varName]; ok {
						useLine := int(arg.StartPoint().Row) + 1
						edges = append(edges, types.CodeEdge{
							Kind:     types.EdgeDataDep,
							FromID:   fnID,
							ToID:     fnID,
							CallSite: fmt.Sprintf("%s:%d", filePath, useLine),
							Metadata: map[string]string{
								"var_name": varName,
								"def_line": fmt.Sprintf("%d", def.line),
								"use_line": fmt.Sprintf("%d", useLine),
								"def_type": def.defType,
							},
						})
					}
				}
			}
		}
		for i := range int(node.ChildCount()) {
			collectUses(node.Child(i))
		}
	}
	collectUses(body)

	return edges
}

// pyDirectStatements returns direct statement children of a Python block,
// filtering out punctuation and comments.
func pyDirectStatements(block *sitter.Node) []*sitter.Node {
	var stmts []*sitter.Node
	for i := range int(block.ChildCount()) {
		child := block.Child(i)
		t := child.Type()
		if t == ":" || t == "comment" || t == "INDENT" || t == "DEDENT" || t == "NEWLINE" {
			continue
		}
		stmts = append(stmts, child)
	}
	return stmts
}

func pyFirstStmtLine(block *sitter.Node) int {
	stmts := pyDirectStatements(block)
	if len(stmts) > 0 {
		return int(stmts[0].StartPoint().Row) + 1
	}
	return 0
}

func pyLastStmtLine(block *sitter.Node) int {
	stmts := pyDirectStatements(block)
	if len(stmts) > 0 {
		return int(stmts[len(stmts)-1].StartPoint().Row) + 1
	}
	return 0
}

// extractPyReturnFlows detects return-value taint propagation within a Python
// function body. When a call's return value is assigned to a variable and that
// variable is later passed as an argument to another call, a data_flow edge is
// emitted connecting the producing call to the consuming call.
func extractPyReturnFlows(n *sitter.Node, src []byte, filePath, scopeQual string) []types.CodeEdge {
	body := n.ChildByFieldName("body")
	if body == nil {
		return nil
	}

	fromID := makeNodeID(string(types.NodeFunction), scopeQual)

	// Pass 1: collect variables assigned from call return values.
	type callOrigin struct {
		callee   string
		callSite string
	}
	varMap := make(map[string]callOrigin)

	var collectVars func(node *sitter.Node)
	collectVars = func(node *sitter.Node) {
		if node == nil {
			return
		}
		// Python assignment: lhs = rhs
		if node.Type() == "assignment" {
			lhs := node.ChildByFieldName("left")
			rhs := node.ChildByFieldName("right")
			if lhs != nil && rhs != nil && rhs.Type() == "call" {
				calleeNode := rhs.ChildByFieldName("function")
				if calleeNode != nil {
					callee := strings.TrimSpace(nodeText(calleeNode, src))
					site := fmt.Sprintf("%s:%d", filePath, node.StartPoint().Row+1)
					if lhs.Type() == "identifier" {
						name := nodeText(lhs, src)
						if name != "_" {
							varMap[name] = callOrigin{callee: callee, callSite: site}
						}
					}
					// Tuple unpacking: a, b = func()
					if lhs.Type() == "pattern_list" || lhs.Type() == "tuple_pattern" {
						for i := range int(lhs.ChildCount()) {
							child := lhs.Child(i)
							if child.Type() == "identifier" {
								name := nodeText(child, src)
								if name != "_" {
									varMap[name] = callOrigin{callee: callee, callSite: site}
								}
							}
						}
					}
				}
			}
		}
		for i := range int(node.ChildCount()) {
			collectVars(node.Child(i))
		}
	}
	collectVars(body)

	if len(varMap) == 0 {
		return nil
	}

	// Pass 2: find call nodes where an argument references a tracked variable.
	var edges []types.CodeEdge

	var findConsumers func(node *sitter.Node)
	findConsumers = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if node.Type() == "call" {
			fnNode := node.ChildByFieldName("function")
			argsNode := node.ChildByFieldName("arguments")
			if fnNode != nil && argsNode != nil {
				consumerCallee := strings.TrimSpace(nodeText(fnNode, src))
				for i := range int(argsNode.ChildCount()) {
					arg := argsNode.Child(i)
					if t := arg.Type(); t == "," || t == "(" || t == ")" {
						continue
					}
					argText := strings.TrimSpace(nodeText(arg, src))
					if origin, ok := varMap[argText]; ok {
						edges = append(edges, types.CodeEdge{
							Kind:     types.EdgeDataFlow,
							FromID:   fromID,
							ToID:     consumerCallee,
							CallSite: fmt.Sprintf("%s:%d", filePath, node.StartPoint().Row+1),
							Metadata: map[string]string{
								"flow_type": "return_value",
								"via_var":   argText,
								"from_call": origin.callee,
								"arg_expr":  argText,
							},
						})
					}
					// attribute access on tracked var: result.field
					if arg.Type() == "attribute" {
						obj := arg.ChildByFieldName("object")
						if obj != nil {
							opText := strings.TrimSpace(nodeText(obj, src))
							if origin, ok := varMap[opText]; ok {
								attr := arg.ChildByFieldName("attribute")
								fieldPath := ""
								if attr != nil {
									fieldPath = opText + "." + nodeText(attr, src)
								}
								edges = append(edges, types.CodeEdge{
									Kind:     types.EdgeDataFlow,
									FromID:   fromID,
									ToID:     consumerCallee,
									CallSite: fmt.Sprintf("%s:%d", filePath, node.StartPoint().Row+1),
									Metadata: map[string]string{
										"flow_type":  "return_value",
										"via_var":    opText,
										"from_call":  origin.callee,
										"arg_expr":   nodeText(arg, src),
										"field_path": fieldPath,
									},
								})
							}
						}
					}
				}
			}
		}
		for i := range int(node.ChildCount()) {
			findConsumers(node.Child(i))
		}
	}
	findConsumers(body)

	return edges
}

// e.g. "myapp/utils/helpers.py" → "myapp.utils.helpers".
func pyModuleFromPath(path string) string {
	path = strings.TrimSuffix(path, ".py")
	return strings.ReplaceAll(path, "/", ".")
}
