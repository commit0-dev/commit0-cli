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

	// Extract HTTP route registrations (runs over full AST).
	edges = append(edges, extractTSRoutes(root, src, file.Path)...)

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
				edges = append(edges, extractTSMutations(n, src, file.Path, fn.Qualified)...)
				edges = append(edges, extractTSReturnFlows(n, src, file.Path, fn.Qualified)...)
				edges = append(edges, extractTSCFG(n, src, file.Path, fn.Qualified)...)
				edges = append(edges, extractTSDataDep(n, src, file.Path, fn.Qualified)...)
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
				edges = append(edges, extractTSMutations(n, src, file.Path, fn.Qualified)...)
				edges = append(edges, extractTSReturnFlows(n, src, file.Path, fn.Qualified)...)
				edges = append(edges, extractTSCFG(n, src, file.Path, fn.Qualified)...)
				edges = append(edges, extractTSDataDep(n, src, file.Path, fn.Qualified)...)
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
		Docstring:  extractJSDoc(n, src),
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
		Docstring:  extractJSDoc(n, src),
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
			Docstring:  extractJSDoc(n, src),
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
		Docstring:  extractJSDoc(n, src),
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

// extractTSMutations detects data mutations inside a function body.
// A mutation is an assignment where the RHS is a function call that transforms
// the LHS value, e.g. `email = email.toLowerCase()` or `x = transform(x)`.
func extractTSMutations(n *sitter.Node, src []byte, filePath, scopeQual string) []types.CodeEdge {
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

		// Look for assignment_expression: lhs = rhs
		if node.Type() == "assignment_expression" {
			lhs := node.ChildByFieldName("left")
			rhs := node.ChildByFieldName("right")
			if lhs != nil && rhs != nil && rhs.Type() == "call_expression" {
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
					// Extract field_path from member_expression: user.email
					if lhs.Type() == "member_expression" {
						obj := lhs.ChildByFieldName("object")
						prop := lhs.ChildByFieldName("property")
						if obj != nil && prop != nil {
							meta["field_path"] = nodeText(obj, src) + "." + nodeText(prop, src)
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

// extractJSDoc looks for a JSDoc or line comment immediately preceding a node.
// Returns the comment text (cleaned) or empty string.
func extractJSDoc(n *sitter.Node, src []byte) string {
	prev := n.PrevNamedSibling()
	if prev == nil || prev.Type() != "comment" {
		// Also check parent's previous sibling (e.g., exported functions)
		if p := n.Parent(); p != nil {
			prev = p.PrevNamedSibling()
			if prev == nil || prev.Type() != "comment" {
				return ""
			}
		} else {
			return ""
		}
	}
	text := nodeText(prev, src)
	// Strip JSDoc markers
	text = strings.TrimPrefix(text, "/**")
	text = strings.TrimSuffix(text, "*/")
	text = strings.TrimPrefix(text, "//")
	// Clean up leading * on each line
	lines := strings.Split(text, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, " ")
}

// tsHTTPMethods maps method names to HTTP methods for Express/Fastify/NestJS.
var tsHTTPMethods = map[string]string{
	"get": "GET", "post": "POST", "put": "PUT",
	"delete": "DELETE", "patch": "PATCH", "head": "HEAD", "options": "OPTIONS",
	"Get": "GET", "Post": "POST", "Put": "PUT",
	"Delete": "DELETE", "Patch": "PATCH", "Head": "HEAD",
}

// extractTSRoutes detects HTTP route registrations from Express/Fastify call patterns
// and NestJS decorators.
//
// Patterns detected:
//   - app.get("/path", handler), router.post("/path", mw, handler) — Express
//   - @Get("/path"), @Post("/path") — NestJS decorators (on method_definition)
func extractTSRoutes(root *sitter.Node, src []byte, filePath string) []types.CodeEdge {
	var edges []types.CodeEdge
	fileID := makeNodeID(string(types.NodeFile), filePath)

	var walk func(node *sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}

		// Express pattern: app.get("/path", handler) or router.post("/path", mw, handler)
		if node.Type() == "call_expression" {
			fnNode := node.ChildByFieldName("function")
			if fnNode != nil && fnNode.Type() == "member_expression" {
				prop := fnNode.ChildByFieldName("property")
				if prop != nil {
					methodName := nodeText(prop, src)
					if httpMethod, ok := tsHTTPMethods[methodName]; ok {
						argsNode := node.ChildByFieldName("arguments")
						if argsNode != nil {
							path := ""
							handler := ""
							var middleware []string
							argIdx := 0
							for i := range int(argsNode.ChildCount()) {
								arg := argsNode.Child(i)
								if t := arg.Type(); t == "," || t == "(" || t == ")" {
									continue
								}
								switch argIdx {
								case 0:
									// First arg: path string
									text := nodeText(arg, src)
									path = strings.Trim(text, `"'`+"`")
								default:
									// Last non-path arg is handler, rest are middleware
									if handler != "" {
										middleware = append(middleware, handler)
									}
									handler = strings.TrimSpace(nodeText(arg, src))
								}
								argIdx++
							}

							if path != "" && handler != "" {
								meta := map[string]string{
									"http_method": httpMethod,
									"http_path":   path,
								}
								if len(middleware) > 0 {
									meta["middleware"] = strings.Join(middleware, ",")
								}
								edges = append(edges, types.CodeEdge{
									Kind:     types.EdgeRoute,
									FromID:   fileID,
									ToID:     handler,
									CallSite: fmt.Sprintf("%s:%d", filePath, node.StartPoint().Row+1),
									Metadata: meta,
								})
							}
						}
					}
				}
			}
		}

		// NestJS pattern: decorators on method_definition
		// @Get("/path") or @Post("/path") preceding a method_definition
		if node.Type() == "method_definition" {
			// Check for decorator siblings (preceding nodes)
			prev := node.PrevNamedSibling()
			for prev != nil && prev.Type() == "decorator" {
				routeEdge := parseTSDecorator(prev, node, src, filePath, fileID)
				if routeEdge != nil {
					edges = append(edges, *routeEdge)
				}
				prev = prev.PrevNamedSibling()
			}
		}

		for i := range int(node.ChildCount()) {
			walk(node.Child(i))
		}
	}

	walk(root)
	return edges
}

// parseTSDecorator checks if a decorator matches an HTTP method pattern (NestJS)
// and returns a route edge if so.
func parseTSDecorator(dec, method *sitter.Node, src []byte, filePath, fileID string) *types.CodeEdge {
	// Decorator structure: @ call_expression(identifier("Get"), arguments("path"))
	for i := range int(dec.ChildCount()) {
		child := dec.Child(i)
		if child.Type() != "call_expression" {
			continue
		}
		fnNode := child.ChildByFieldName("function")
		if fnNode == nil {
			continue
		}
		decoratorName := nodeText(fnNode, src)
		httpMethod, ok := tsHTTPMethods[decoratorName]
		if !ok {
			continue
		}

		// Extract path from arguments
		argsNode := child.ChildByFieldName("arguments")
		path := ""
		if argsNode != nil {
			for j := range int(argsNode.ChildCount()) {
				arg := argsNode.Child(j)
				if t := arg.Type(); t == "," || t == "(" || t == ")" {
					continue
				}
				text := nodeText(arg, src)
				path = strings.Trim(text, `"'`+"`")
				break
			}
		}
		if path == "" {
			path = "/"
		}

		// Get handler name from the method_definition
		nameNode := method.ChildByFieldName("name")
		handler := ""
		if nameNode != nil {
			handler = nodeText(nameNode, src)
		}
		if handler == "" {
			continue
		}

		return &types.CodeEdge{
			Kind:     types.EdgeRoute,
			FromID:   fileID,
			ToID:     handler,
			CallSite: fmt.Sprintf("%s:%d", filePath, dec.StartPoint().Row+1),
			Metadata: map[string]string{
				"http_method": httpMethod,
				"http_path":   path,
			},
		}
	}
	return nil
}

// ── Control Flow Graph (CFG) extraction ──────────────────────────────────────

// extractTSCFG builds intra-procedural control flow edges for a TypeScript/JS function.
func extractTSCFG(n *sitter.Node, src []byte, filePath, scopeQual string) []types.CodeEdge {
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
		stmts := tsDirectStatements(block)
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
				alternative := stmt.ChildByFieldName("alternative")

				if consequence != nil {
					firstTrue := tsFirstStmtLine(consequence)
					if firstTrue > 0 {
						cfgEdge(line, firstTrue, "if_true", condText)
					}
					walkBlock(consequence)
				}

				if alternative != nil {
					firstFalse := tsFirstStmtLine(alternative)
					if firstFalse > 0 {
						cfgEdge(line, firstFalse, "if_false", condText)
					}
					walkBlock(alternative)
				}

			case "for_statement", "for_in_statement", "while_statement":
				bodyNode := stmt.ChildByFieldName("body")
				if bodyNode != nil {
					firstBody := tsFirstStmtLine(bodyNode)
					if firstBody > 0 {
						cfgEdge(line, firstBody, "loop_entry", "")
					}
					lastBody := tsLastStmtLine(bodyNode)
					if lastBody > 0 {
						cfgEdge(lastBody, line, "loop_back", "")
					}
					walkBlock(bodyNode)
				}

			case "return_statement":
				cfgEdge(line, line, "return", "")

			case "switch_statement":
				for j := range int(stmt.ChildCount()) {
					child := stmt.Child(j)
					if child.Type() == "switch_body" {
						walkBlock(child)
					}
				}

			case "try_statement":
				for j := range int(stmt.ChildCount()) {
					child := stmt.Child(j)
					if child.Type() == "statement_block" || child.Type() == "catch_clause" || child.Type() == "finally_clause" {
						walkBlock(child)
					}
				}
			}
		}
	}

	walkBlock(body)
	return edges
}

// extractTSDataDep builds intra-procedural data dependence edges for TypeScript/JS.
func extractTSDataDep(n *sitter.Node, src []byte, filePath, scopeQual string) []types.CodeEdge {
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
			switch child.Type() {
			case "required_parameter", "optional_parameter":
				nameNode := child.ChildByFieldName("pattern")
				if nameNode == nil {
					nameNode = child.ChildByFieldName("name")
				}
				if nameNode != nil && nameNode.Type() == "identifier" {
					name := nodeText(nameNode, src)
					defs = append(defs, varDef{name: name, line: int(child.StartPoint().Row) + 1, defType: "parameter"})
				}
			case "identifier":
				name := nodeText(child, src)
				if name != "(" && name != ")" && name != "," {
					defs = append(defs, varDef{name: name, line: int(child.StartPoint().Row) + 1, defType: "parameter"})
				}
			}
		}
	}

	// Collect assignment definitions from body.
	var collectDefs func(node *sitter.Node)
	collectDefs = func(node *sitter.Node) {
		if node == nil {
			return
		}
		switch node.Type() {
		case "variable_declarator":
			nameNode := node.ChildByFieldName("name")
			valNode := node.ChildByFieldName("value")
			defType := "assignment"
			if valNode != nil && valNode.Type() == "call_expression" {
				defType = "return_value"
			}
			if nameNode != nil && nameNode.Type() == "identifier" {
				name := nodeText(nameNode, src)
				defs = append(defs, varDef{name: name, line: int(node.StartPoint().Row) + 1, defType: defType})
			}

		case "assignment_expression":
			lhs := node.ChildByFieldName("left")
			rhs := node.ChildByFieldName("right")
			defType := "assignment"
			if rhs != nil && rhs.Type() == "call_expression" {
				defType = "return_value"
			}
			if lhs != nil && lhs.Type() == "identifier" {
				name := nodeText(lhs, src)
				defs = append(defs, varDef{name: name, line: int(node.StartPoint().Row) + 1, defType: defType})
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
		if node.Type() == "call_expression" {
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
					} else if arg.Type() == "member_expression" {
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

func tsDirectStatements(block *sitter.Node) []*sitter.Node {
	var stmts []*sitter.Node
	for i := range int(block.ChildCount()) {
		child := block.Child(i)
		t := child.Type()
		if t == "{" || t == "}" || t == "comment" {
			continue
		}
		stmts = append(stmts, child)
	}
	return stmts
}

func tsFirstStmtLine(block *sitter.Node) int {
	stmts := tsDirectStatements(block)
	if len(stmts) > 0 {
		return int(stmts[0].StartPoint().Row) + 1
	}
	return 0
}

func tsLastStmtLine(block *sitter.Node) int {
	stmts := tsDirectStatements(block)
	if len(stmts) > 0 {
		return int(stmts[len(stmts)-1].StartPoint().Row) + 1
	}
	return 0
}

// extractTSReturnFlows detects return-value taint propagation within a
// TypeScript/JavaScript function body. When a call's return value is assigned
// to a variable and that variable is later passed as an argument to another
// call, a data_flow edge is emitted connecting the producing call to the
// consuming call.
func extractTSReturnFlows(n *sitter.Node, src []byte, filePath, scopeQual string) []types.CodeEdge {
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

		switch node.Type() {
		case "variable_declarator":
			// const result = someCall(...)
			nameNode := node.ChildByFieldName("name")
			valNode := node.ChildByFieldName("value")
			if nameNode != nil && valNode != nil && valNode.Type() == "call_expression" {
				calleeNode := valNode.ChildByFieldName("function")
				if calleeNode != nil {
					name := strings.TrimSpace(nodeText(nameNode, src))
					callee := strings.TrimSpace(nodeText(calleeNode, src))
					site := fmt.Sprintf("%s:%d", filePath, node.StartPoint().Row+1)
					if name != "_" {
						varMap[name] = callOrigin{callee: callee, callSite: site}
					}
				}
			}

		case "assignment_expression":
			// result = someCall(...)
			lhs := node.ChildByFieldName("left")
			rhs := node.ChildByFieldName("right")
			if lhs != nil && rhs != nil && rhs.Type() == "call_expression" {
				calleeNode := rhs.ChildByFieldName("function")
				if calleeNode != nil && lhs.Type() == "identifier" {
					name := nodeText(lhs, src)
					callee := strings.TrimSpace(nodeText(calleeNode, src))
					site := fmt.Sprintf("%s:%d", filePath, node.StartPoint().Row+1)
					varMap[name] = callOrigin{callee: callee, callSite: site}
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

	// Pass 2: find call_expression nodes where an argument references a tracked variable.
	var edges []types.CodeEdge

	var findConsumers func(node *sitter.Node)
	findConsumers = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if node.Type() == "call_expression" {
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
					// member_expression on tracked var: result.field
					if arg.Type() == "member_expression" {
						obj := arg.ChildByFieldName("object")
						if obj != nil {
							opText := strings.TrimSpace(nodeText(obj, src))
							if origin, ok := varMap[opText]; ok {
								prop := arg.ChildByFieldName("property")
								fieldPath := ""
								if prop != nil {
									fieldPath = opText + "." + nodeText(prop, src)
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

// e.g. "src/services/auth.ts" → "src.services.auth".
func tsModuleFromPath(path string) string {
	path = strings.TrimSuffix(path, ".ts")
	path = strings.TrimSuffix(path, ".tsx")
	path = strings.TrimSuffix(path, ".js")
	path = strings.TrimSuffix(path, ".jsx")
	return strings.ReplaceAll(path, "/", ".")
}
