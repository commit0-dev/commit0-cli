package lang

import (
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	gositter "github.com/smacker/go-tree-sitter/golang"

	"github.com/commit0-dev/commit0/server/internal/domain"
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

	// Extract HTTP route registrations (runs over full AST, independent of BFS scope).
	edges = append(edges, extractGoRoutes(root, src, file.Path)...)

	// Note: CFG and DataDep are extracted per-function inside the BFS loop below.

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
				// Extract field-level mutations inside the function body.
				edges = append(edges, extractGoMutations(n, src, file.Path, fn.Qualified)...)
				// Extract return-value taint propagation edges.
				edges = append(edges, extractGoReturnFlows(n, src, file.Path, fn.Qualified)...)
				// Extract HTTP request/response bindings.
				edges = append(edges, extractGoBindings(n, src, file.Path, fn.Qualified)...)
				// Extract control flow graph and data dependence edges.
				edges = append(edges, extractGoCFG(n, src, file.Path, fn.Qualified)...)
				edges = append(edges, extractGoDataDep(n, src, file.Path, fn.Qualified)...)
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
				edges = append(edges, extractGoMutations(n, src, file.Path, fn.Qualified)...)
				edges = append(edges, extractGoReturnFlows(n, src, file.Path, fn.Qualified)...)
				edges = append(edges, extractGoBindings(n, src, file.Path, fn.Qualified)...)
				edges = append(edges, extractGoCFG(n, src, file.Path, fn.Qualified)...)
				edges = append(edges, extractGoDataDep(n, src, file.Path, fn.Qualified)...)
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
// passed at a call site. Includes field_path for selector expressions (e.g.
// user.Email) so field-level data flow can be traced.
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
		if t := arg.Type(); t == "," || t == "(" || t == ")" {
			continue
		}
		if isGoLiteral(arg) {
			continue
		}
		argText := strings.TrimSpace(nodeText(arg, src))
		if argText == "" {
			continue
		}

		meta := map[string]string{"arg_expr": argText}

		// Extract field_path from selector expressions: user.Email → "user.Email"
		if arg.Type() == "selector_expression" {
			operand := arg.ChildByFieldName("operand")
			field := arg.ChildByFieldName("field")
			if operand != nil && field != nil {
				meta["field_path"] = nodeText(operand, src) + "." + nodeText(field, src)
			}
		}

		edges = append(edges, types.CodeEdge{
			Kind:     types.EdgeDataFlow,
			FromID:   fromID,
			ToID:     callee,
			CallSite: callSite,
			Metadata: meta,
		})
	}
	return edges
}

// extractGoMutations detects data mutations inside a function body.
// A mutation is an assignment where the RHS is a function call that transforms
// the LHS value, e.g. `email = strings.ToLower(email)` or `x.Field = transform(x.Field)`.
// Returns data_flow edges with mutation metadata.
func extractGoMutations(n *sitter.Node, src []byte, filePath, scopeQual string) []types.CodeEdge {
	if n.Type() != "function_declaration" && n.Type() != "method_declaration" {
		return nil
	}
	body := n.ChildByFieldName("body")
	if body == nil {
		return nil
	}

	fromID := makeNodeID(string(types.NodeFunction), scopeQual)
	var edges []types.CodeEdge

	// Walk body looking for assignment_statement or short_var_declaration
	var walk func(node *sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}

		switch node.Type() {
		case "assignment_statement":
			// LHS = RHS — check if RHS is a call_expression
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
					// If LHS is a selector (user.Email), extract field_path
					if lhs.Type() == "selector_expression" {
						operand := lhs.ChildByFieldName("operand")
						field := lhs.ChildByFieldName("field")
						if operand != nil && field != nil {
							meta["field_path"] = nodeText(operand, src) + "." + nodeText(field, src)
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

		// Recurse into children
		for i := range int(node.ChildCount()) {
			walk(node.Child(i))
		}
	}

	walk(body)
	return edges
}

// ── HTTP route extraction ────────────────────────────────────────────────────

// httpMethods is the set of HTTP method names recognized for route extraction.
var httpMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true,
}

// extractGoRoutes detects HTTP route registrations in Go code.
// It recognizes patterns from Echo, Gin, chi, and net/http:
//   - e.GET("/path", handler, middleware...)
//   - group.POST("/path", handler)
//   - e.Group("/prefix", middleware...)
//   - http.HandleFunc("/path", handler)
//
// It returns EdgeRoute edges and tracks group variable→prefix mappings for
// nested route resolution.
func extractGoRoutes(root *sitter.Node, src []byte, filePath string) []types.CodeEdge {
	var edges []types.CodeEdge

	// Track group variable → prefix mapping: v1 := e.Group("/api/v1")
	groupPrefixes := make(map[string]string)
	// Track group variable → middleware list
	groupMiddleware := make(map[string][]string)

	fileID := makeNodeID(string(types.NodeFile), filePath)

	var walk func(node *sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}

		// Detect group assignment: v1 := e.Group("/prefix") or v1 = e.Group("/prefix")
		if node.Type() == "short_var_declaration" || node.Type() == "assignment_statement" {
			lhs := node.ChildByFieldName("left")
			rhs := node.ChildByFieldName("right")
			if lhs != nil && rhs != nil {
				call := findCallExpr(rhs)
				if call != nil {
					fnNode := call.ChildByFieldName("function")
					if fnNode != nil && fnNode.Type() == "selector_expression" {
						method := fnNode.ChildByFieldName("field")
						if method != nil && nodeText(method, src) == "Group" {
							argsNode := call.ChildByFieldName("arguments")
							if argsNode != nil {
								prefix := ""
								varName := extractFirstIdentifier(lhs, src)
								argIdx := 0
								for i := range int(argsNode.ChildCount()) {
									arg := argsNode.Child(i)
									if t := arg.Type(); t == "," || t == "(" || t == ")" {
										continue
									}
									if argIdx == 0 {
										prefix = extractStringArg(arg, src)
									} else {
										mw := strings.TrimSpace(nodeText(arg, src))
										if mw != "" && varName != "" {
											groupMiddleware[varName] = append(groupMiddleware[varName], mw)
										}
									}
									argIdx++
								}
								if prefix != "" && varName != "" {
									groupPrefixes[varName] = prefix
								}
							}
						}
					}
				}
			}
		}

		// Detect .Use(middleware) calls: v1.Use(authMiddleware)
		if node.Type() == "call_expression" {
			fnNode := node.ChildByFieldName("function")
			if fnNode != nil && fnNode.Type() == "selector_expression" {
				obj := fnNode.ChildByFieldName("operand")
				method := fnNode.ChildByFieldName("field")
				if obj != nil && method != nil && nodeText(method, src) == "Use" {
					varName := nodeText(obj, src)
					argsNode := node.ChildByFieldName("arguments")
					if argsNode != nil {
						for i := range int(argsNode.ChildCount()) {
							arg := argsNode.Child(i)
							if t := arg.Type(); t == "," || t == "(" || t == ")" {
								continue
							}
							mw := strings.TrimSpace(nodeText(arg, src))
							if mw != "" {
								groupMiddleware[varName] = append(groupMiddleware[varName], mw)
							}
						}
					}
				}
			}
		}

		// Detect route registrations: v1.GET("/path", handler, middleware...)
		if node.Type() == "call_expression" {
			fnNode := node.ChildByFieldName("function")
			if fnNode != nil && fnNode.Type() == "selector_expression" {
				obj := fnNode.ChildByFieldName("operand")
				method := fnNode.ChildByFieldName("field")
				if obj != nil && method != nil {
					methodName := nodeText(method, src)
					if httpMethods[methodName] {
						argsNode := node.ChildByFieldName("arguments")
						if argsNode == nil || argsNode.ChildCount() < 2 {
							goto recurse
						}

						// First non-punctuation arg is the path
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
								path = extractStringArg(arg, src)
							case 1:
								handler = strings.TrimSpace(nodeText(arg, src))
							default:
								mw := strings.TrimSpace(nodeText(arg, src))
								if mw != "" {
									middleware = append(middleware, mw)
								}
							}
							argIdx++
						}

						if path == "" || handler == "" {
							goto recurse
						}

						// Resolve group prefix
						routerVar := nodeText(obj, src)
						groupPrefix := groupPrefixes[routerVar]
						fullPath := groupPrefix + path

						// Collect group-level middleware
						allMiddleware := append([]string{}, groupMiddleware[routerVar]...)
						allMiddleware = append(allMiddleware, middleware...)

						meta := map[string]string{
							"http_method": methodName,
							"http_path":   fullPath,
						}
						if groupPrefix != "" {
							meta["group_prefix"] = groupPrefix
						}
						if len(allMiddleware) > 0 {
							meta["middleware"] = strings.Join(allMiddleware, ",")
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

		// Also detect http.HandleFunc("/path", handler)
		if node.Type() == "call_expression" {
			fnNode := node.ChildByFieldName("function")
			if fnNode != nil {
				calleeText := strings.TrimSpace(nodeText(fnNode, src))
				if calleeText == "http.HandleFunc" || calleeText == "http.Handle" {
					argsNode := node.ChildByFieldName("arguments")
					if argsNode != nil {
						path := ""
						handler := ""
						argIdx := 0
						for i := range int(argsNode.ChildCount()) {
							arg := argsNode.Child(i)
							if t := arg.Type(); t == "," || t == "(" || t == ")" {
								continue
							}
							switch argIdx {
							case 0:
								path = extractStringArg(arg, src)
							case 1:
								handler = strings.TrimSpace(nodeText(arg, src))
							}
							argIdx++
						}
						if path != "" && handler != "" {
							edges = append(edges, types.CodeEdge{
								Kind:     types.EdgeRoute,
								FromID:   fileID,
								ToID:     handler,
								CallSite: fmt.Sprintf("%s:%d", filePath, node.StartPoint().Row+1),
								Metadata: map[string]string{
									"http_method": "ANY",
									"http_path":   path,
								},
							})
						}
					}
				}
			}
		}

	recurse:
		for i := range int(node.ChildCount()) {
			walk(node.Child(i))
		}
	}

	walk(root)
	return edges
}

// extractGoBindings detects HTTP request/response bindings inside a handler function body.
// It recognizes Echo patterns: c.Param("name"), c.QueryParam("name"), c.Bind(&req), c.JSON(status, data).
// These are emitted as data_flow edges with source_type metadata so the API surface service
// can identify where external input enters and what data exits.
func extractGoBindings(n *sitter.Node, src []byte, filePath, scopeQual string) []types.CodeEdge {
	if n.Type() != "function_declaration" && n.Type() != "method_declaration" {
		return nil
	}
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

		if node.Type() == "call_expression" {
			fnNode := node.ChildByFieldName("function")
			if fnNode != nil && fnNode.Type() == "selector_expression" {
				method := fnNode.ChildByFieldName("field")
				if method != nil {
					methodName := nodeText(method, src)
					argsNode := node.ChildByFieldName("arguments")

					switch methodName {
					case "Param", "FormValue":
						// c.Param("id") or r.FormValue("key")
						paramName := extractFirstStringArg(argsNode, src)
						if paramName != "" {
							edges = append(edges, types.CodeEdge{
								Kind:     types.EdgeDataFlow,
								FromID:   fromID,
								ToID:     fromID, // self-referencing: data enters this function
								CallSite: fmt.Sprintf("%s:%d", filePath, node.StartPoint().Row+1),
								Metadata: map[string]string{
									"source_type": "path_param",
									"param_name":  paramName,
								},
							})
						}

					case "QueryParam", "QueryParams":
						paramName := extractFirstStringArg(argsNode, src)
						if paramName != "" {
							edges = append(edges, types.CodeEdge{
								Kind:     types.EdgeDataFlow,
								FromID:   fromID,
								ToID:     fromID,
								CallSite: fmt.Sprintf("%s:%d", filePath, node.StartPoint().Row+1),
								Metadata: map[string]string{
									"source_type": "query_param",
									"param_name":  paramName,
								},
							})
						}

					case "Bind":
						// c.Bind(&req) — extract the bound type name
						typeName := ""
						if argsNode != nil {
							for i := range int(argsNode.ChildCount()) {
								arg := argsNode.Child(i)
								if t := arg.Type(); t == "," || t == "(" || t == ")" {
									continue
								}
								argText := strings.TrimSpace(nodeText(arg, src))
								argText = strings.TrimPrefix(argText, "&")
								typeName = argText
								break
							}
						}
						edges = append(edges, types.CodeEdge{
							Kind:     types.EdgeDataFlow,
							FromID:   fromID,
							ToID:     fromID,
							CallSite: fmt.Sprintf("%s:%d", filePath, node.StartPoint().Row+1),
							Metadata: map[string]string{
								"source_type": "request_body",
								"bound_type":  typeName,
							},
						})

					case "JSON":
						// c.JSON(status, data) — extract the response type
						if argsNode != nil {
							argIdx := 0
							for i := range int(argsNode.ChildCount()) {
								arg := argsNode.Child(i)
								if t := arg.Type(); t == "," || t == "(" || t == ")" {
									continue
								}
								if argIdx == 1 { // second arg is the response data
									responseExpr := strings.TrimSpace(nodeText(arg, src))
									edges = append(edges, types.CodeEdge{
										Kind:     types.EdgeDataFlow,
										FromID:   fromID,
										ToID:     fromID,
										CallSite: fmt.Sprintf("%s:%d", filePath, node.StartPoint().Row+1),
										Metadata: map[string]string{
											"source_type":   "response",
											"response_expr": responseExpr,
										},
									})
									break
								}
								argIdx++
							}
						}
					}
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

// extractStringArg extracts a string literal value from a node, stripping quotes.
func extractStringArg(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	text := nodeText(n, src)
	text = strings.Trim(text, `"'` + "`")
	return text
}

// extractFirstStringArg extracts the first string literal argument from an arguments node.
func extractFirstStringArg(argsNode *sitter.Node, src []byte) string {
	if argsNode == nil {
		return ""
	}
	for i := range int(argsNode.ChildCount()) {
		arg := argsNode.Child(i)
		if t := arg.Type(); t == "," || t == "(" || t == ")" {
			continue
		}
		return extractStringArg(arg, src)
	}
	return ""
}

// extractFirstIdentifier extracts the first identifier name from a node (for LHS of assignments).
func extractFirstIdentifier(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	if n.Type() == "identifier" {
		return nodeText(n, src)
	}
	// expression_list: first identifier child
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		if child.Type() == "identifier" {
			return nodeText(child, src)
		}
	}
	return ""
}

// ── Control Flow Graph (CFG) extraction ──────────────────────────────────────

// extractGoCFG builds intra-procedural control flow edges for a Go function.
// It identifies branching constructs (if/else, for, switch, return) and emits
// EdgeControlFlow edges between statement line positions.
//
// Edge representation: FromID and ToID are both the enclosing function's node ID.
// Metadata carries from_line, to_line, and branch_type to identify the specific
// control flow transition within the function body.
func extractGoCFG(n *sitter.Node, src []byte, filePath, scopeQual string) []types.CodeEdge {
	if n.Type() != "function_declaration" && n.Type() != "method_declaration" {
		return nil
	}
	body := n.ChildByFieldName("body")
	if body == nil {
		return nil
	}

	fnID := makeNodeID(string(types.NodeFunction), scopeQual)
	var edges []types.CodeEdge

	// cfgEdge is a helper to emit a control flow edge.
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

	// Walk the block's direct children (statements) and build CFG edges.
	var walkBlock func(block *sitter.Node)
	walkBlock = func(block *sitter.Node) {
		if block == nil {
			return
		}

		stmts := directStatements(block)
		for i, stmt := range stmts {
			line := int(stmt.StartPoint().Row) + 1

			// Sequential edge: previous statement → this statement.
			if i > 0 {
				prevLine := int(stmts[i-1].StartPoint().Row) + 1
				prevType := stmts[i-1].Type()
				// Don't emit sequential edge after return (dead code).
				if prevType != "return_statement" {
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

				// Edge: if_condition → true branch.
				if consequence != nil && consequence.ChildCount() > 0 {
					firstTrue := firstStatementLine(consequence)
					if firstTrue > 0 {
						cfgEdge(line, firstTrue, "if_true", condText)
					}
					walkBlock(consequence)
				}

				// Edge: if_condition → false branch (else/else-if).
				if alternative != nil {
					// alternative may be a block or another if_statement (else if).
					firstFalse := firstStatementLine(alternative)
					if firstFalse > 0 {
						cfgEdge(line, firstFalse, "if_false", condText)
					}
					walkBlock(alternative)
				}

				// Merge: both branches → next statement after the if.
				if i+1 < len(stmts) {
					nextLine := int(stmts[i+1].StartPoint().Row) + 1
					if consequence != nil {
						lastTrue := lastStatementLine(consequence)
						if lastTrue > 0 {
							cfgEdge(lastTrue, nextLine, "sequential", "")
						}
					}
					if alternative != nil {
						lastFalse := lastStatementLine(alternative)
						if lastFalse > 0 {
							cfgEdge(lastFalse, nextLine, "sequential", "")
						}
					}
				}

			case "for_statement":
				// For loops: entry → condition → body → back to condition, condition → exit.
				bodyNode := stmt.ChildByFieldName("body")
				if bodyNode != nil && bodyNode.ChildCount() > 0 {
					firstBody := firstStatementLine(bodyNode)
					if firstBody > 0 {
						cfgEdge(line, firstBody, "loop_entry", "")
					}

					// Loop back edge: last body statement → loop header.
					lastBody := lastStatementLine(bodyNode)
					if lastBody > 0 {
						cfgEdge(lastBody, line, "loop_back", "")
					}

					walkBlock(bodyNode)
				}

			case "return_statement":
				cfgEdge(line, line, "return", "")

			case "go_statement":
				// Goroutine launch — sequential flow continues but spawns concurrent work.
				// No special CFG edge needed beyond sequential.

			case "defer_statement":
				// Deferred call — executes at function exit. Sequential flow continues.

			case "switch_statement", "type_switch_statement":
				// Walk into case clauses.
				for j := range int(stmt.ChildCount()) {
					child := stmt.Child(j)
					if child.Type() == "expression_case_clause" || child.Type() == "type_case_clause" || child.Type() == "default_case" {
						walkBlock(child)
					}
				}

			case "select_statement":
				for j := range int(stmt.ChildCount()) {
					child := stmt.Child(j)
					if child.Type() == "communication_case" || child.Type() == "default_case" {
						walkBlock(child)
					}
				}
			}
		}
	}

	walkBlock(body)
	return edges
}

// directStatements returns the direct statement children of a block node,
// filtering out punctuation ({, }, etc).
func directStatements(block *sitter.Node) []*sitter.Node {
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

// firstStatementLine returns the line number of the first non-punctuation child.
func firstStatementLine(block *sitter.Node) int {
	stmts := directStatements(block)
	if len(stmts) > 0 {
		return int(stmts[0].StartPoint().Row) + 1
	}
	return 0
}

// lastStatementLine returns the line number of the last non-punctuation child.
func lastStatementLine(block *sitter.Node) int {
	stmts := directStatements(block)
	if len(stmts) > 0 {
		return int(stmts[len(stmts)-1].StartPoint().Row) + 1
	}
	return 0
}

// ── Data Dependence (def-use chain) extraction ──────────────────────────────

// extractGoDataDep builds intra-procedural data dependence edges for a Go function.
// It tracks variable definitions (assignments, parameters, short var declarations)
// and their uses as arguments in subsequent expressions.
func extractGoDataDep(n *sitter.Node, src []byte, filePath, scopeQual string) []types.CodeEdge {
	if n.Type() != "function_declaration" && n.Type() != "method_declaration" {
		return nil
	}
	body := n.ChildByFieldName("body")
	if body == nil {
		return nil
	}

	fnID := makeNodeID(string(types.NodeFunction), scopeQual)

	// Collect parameter definitions.
	type varDef struct {
		name    string
		line    int
		defType string // "parameter", "assignment", "return_value", "for_range"
	}

	var defs []varDef

	// Extract parameters as definitions.
	params := n.ChildByFieldName("parameters")
	if params != nil {
		var walkParams func(node *sitter.Node)
		walkParams = func(node *sitter.Node) {
			if node == nil {
				return
			}
			if node.Type() == "parameter_declaration" || node.Type() == "variadic_parameter_declaration" {
				nameNode := node.ChildByFieldName("name")
				if nameNode != nil {
					name := nodeText(nameNode, src)
					if name != "_" {
						defs = append(defs, varDef{
							name:    name,
							line:    int(nameNode.StartPoint().Row) + 1,
							defType: "parameter",
						})
					}
				}
			}
			for i := range int(node.ChildCount()) {
				walkParams(node.Child(i))
			}
		}
		walkParams(params)
	}

	// Also extract receiver as a definition (for methods).
	receiver := n.ChildByFieldName("receiver")
	if receiver != nil {
		var walkReceiver func(node *sitter.Node)
		walkReceiver = func(node *sitter.Node) {
			if node == nil {
				return
			}
			if node.Type() == "parameter_declaration" {
				nameNode := node.ChildByFieldName("name")
				if nameNode != nil {
					name := nodeText(nameNode, src)
					if name != "_" {
						defs = append(defs, varDef{
							name:    name,
							line:    int(nameNode.StartPoint().Row) + 1,
							defType: "parameter",
						})
					}
				}
			}
			for i := range int(node.ChildCount()) {
				walkReceiver(node.Child(i))
			}
		}
		walkReceiver(receiver)
	}

	// Collect assignment definitions from the body.
	var collectDefs func(node *sitter.Node)
	collectDefs = func(node *sitter.Node) {
		if node == nil {
			return
		}
		switch node.Type() {
		case "short_var_declaration":
			lhs := node.ChildByFieldName("left")
			rhs := node.ChildByFieldName("right")
			defType := "assignment"
			if rhs != nil && findCallExpr(rhs) != nil {
				defType = "return_value"
			}
			if lhs != nil {
				if lhs.Type() == "identifier" {
					name := nodeText(lhs, src)
					if name != "_" && name != "err" {
						defs = append(defs, varDef{name: name, line: int(node.StartPoint().Row) + 1, defType: defType})
					}
				} else {
					for i := range int(lhs.ChildCount()) {
						child := lhs.Child(i)
						if child.Type() == "identifier" {
							name := nodeText(child, src)
							if name != "_" && name != "err" {
								defs = append(defs, varDef{name: name, line: int(node.StartPoint().Row) + 1, defType: defType})
							}
						}
					}
				}
			}

		case "assignment_statement":
			lhs := node.ChildByFieldName("left")
			rhs := node.ChildByFieldName("right")
			defType := "assignment"
			if rhs != nil && findCallExpr(rhs) != nil {
				defType = "return_value"
			}
			if lhs != nil && lhs.Type() == "identifier" {
				name := nodeText(lhs, src)
				if name != "_" && name != "err" {
					defs = append(defs, varDef{name: name, line: int(node.StartPoint().Row) + 1, defType: defType})
				}
			}

		case "range_clause":
			// for k, v := range slice { ... }
			for i := range int(node.ChildCount()) {
				child := node.Child(i)
				if child.Type() == "identifier" {
					name := nodeText(child, src)
					if name != "_" {
						defs = append(defs, varDef{name: name, line: int(node.StartPoint().Row) + 1, defType: "for_range"})
					}
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

	// Build a map: variable name → most recent definitions (simple last-def-wins for now).
	defMap := make(map[string]varDef)
	for _, d := range defs {
		defMap[d.name] = d
	}

	// Collect uses: identifiers in call arguments and expressions.
	var edges []types.CodeEdge

	var collectUses func(node *sitter.Node)
	collectUses = func(node *sitter.Node) {
		if node == nil {
			return
		}

		// Look for identifiers used as arguments to calls.
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
					} else if arg.Type() == "selector_expression" {
						// user.Email — track the operand
						operand := arg.ChildByFieldName("operand")
						if operand != nil && operand.Type() == "identifier" {
							varName = nodeText(operand, src)
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

// extractGoReturnFlows detects return-value taint propagation within a function
// body. When a call's return value is assigned to a local variable and that
// variable is later passed as an argument to another call, a data_flow edge
// is emitted connecting the producing call to the consuming call.
//
// Example:
//
//	result := process(input)   // varMap["result"] = "process"
//	db.Query(result)           // edge: process → db.Query, flow_type: return_value
//
// This is intra-procedural (single function body, single pass). It does NOT
// track across function boundaries or through struct fields.
func extractGoReturnFlows(n *sitter.Node, src []byte, filePath, scopeQual string) []types.CodeEdge {
	if n.Type() != "function_declaration" && n.Type() != "method_declaration" {
		return nil
	}
	body := n.ChildByFieldName("body")
	if body == nil {
		return nil
	}

	fromID := makeNodeID(string(types.NodeFunction), scopeQual)

	// Pass 1: collect variables assigned from call return values.
	// varMap: variable name → producing callee qualified name + call site.
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
		case "short_var_declaration":
			// pattern: lhs := rhs
			lhs := node.ChildByFieldName("left")
			rhs := node.ChildByFieldName("right")
			if lhs != nil && rhs != nil {
				// RHS must be a call_expression (or expression_list containing one)
				rhsCall := findCallExpr(rhs)
				if rhsCall != nil {
					calleeNode := rhsCall.ChildByFieldName("function")
					if calleeNode != nil {
						callee := strings.TrimSpace(nodeText(calleeNode, src))
						site := fmt.Sprintf("%s:%d", filePath, node.StartPoint().Row+1)
						// Extract variable names from LHS expression_list
						for i := range int(lhs.ChildCount()) {
							child := lhs.Child(i)
							if child.Type() == "identifier" {
								name := nodeText(child, src)
								// Skip blank identifiers and err
								if name != "_" && name != "err" {
									varMap[name] = callOrigin{callee: callee, callSite: site}
								}
							}
						}
						// Single identifier LHS
						if lhs.Type() == "identifier" {
							name := nodeText(lhs, src)
							if name != "_" && name != "err" {
								varMap[name] = callOrigin{callee: callee, callSite: site}
							}
						}
					}
				}
			}

		case "assignment_statement":
			// pattern: lhs = rhs (reassignment of existing var)
			lhs := node.ChildByFieldName("left")
			rhs := node.ChildByFieldName("right")
			if lhs != nil && rhs != nil {
				rhsCall := findCallExpr(rhs)
				if rhsCall != nil {
					calleeNode := rhsCall.ChildByFieldName("function")
					if calleeNode != nil {
						callee := strings.TrimSpace(nodeText(calleeNode, src))
						site := fmt.Sprintf("%s:%d", filePath, node.StartPoint().Row+1)
						if lhs.Type() == "identifier" {
							name := nodeText(lhs, src)
							if name != "_" && name != "err" {
								varMap[name] = callOrigin{callee: callee, callSite: site}
							}
						}
						// expression_list LHS
						for i := range int(lhs.ChildCount()) {
							child := lhs.Child(i)
							if child.Type() == "identifier" {
								name := nodeText(child, src)
								if name != "_" && name != "err" {
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
					// Check if argument is a tracked variable (direct identifier reference)
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
					// Also check selector expressions: tracked var used as operand (e.g. result.Field)
					if arg.Type() == "selector_expression" {
						operand := arg.ChildByFieldName("operand")
						if operand != nil {
							opText := strings.TrimSpace(nodeText(operand, src))
							if origin, ok := varMap[opText]; ok {
								fieldNode := arg.ChildByFieldName("field")
								fieldPath := ""
								if fieldNode != nil {
									fieldPath = opText + "." + nodeText(fieldNode, src)
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

// findCallExpr finds the first call_expression in a node tree.
// Handles expression_list wrapping (e.g. result, err := func()).
func findCallExpr(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if n.Type() == "call_expression" {
		return n
	}
	for i := range int(n.ChildCount()) {
		if found := findCallExpr(n.Child(i)); found != nil {
			return found
		}
	}
	return nil
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
