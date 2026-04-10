// CGO_ENABLED=1 required — tree-sitter uses C libraries.
package lang

import (
	"context"
	"strings"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	gositter "github.com/smacker/go-tree-sitter/golang"

	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// ── helper ────────────────────────────────────────────────────────────────────

func parseGoAST(t *testing.T, src string) *sitter.Node {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(gositter.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Cleanup(func() { tree.Close() })
	return tree.RootNode()
}

// ── TestGoExtractor_Language ──────────────────────────────────────────────────

func TestGoExtractor_Language(t *testing.T) {
	e := &GoExtractor{}
	if e.Language() == nil {
		t.Error("Language() returned nil")
	}
}

// ── TestGoExtractor_FunctionDeclaration ──────────────────────────────────────

func TestGoExtractor_FunctionDeclaration(t *testing.T) {
	src := `package main
func Hello(name string) string { return "hi " + name }`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	nodes, _ := e.Extract(root, fe)

	var found *types.CodeNode
	for i := range nodes {
		if nodes[i].Kind == types.NodeFunction && nodes[i].Name == "Hello" {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no NodeFunction named 'Hello'; nodes: %v", nodes)
	}
	if !strings.Contains(found.Qualified, "Hello") {
		t.Errorf("Qualified = %q; want it to contain 'Hello'", found.Qualified)
	}
	if found.Visibility != "public" {
		t.Errorf("Visibility = %q; want %q", found.Visibility, "public")
	}
}

// ── TestGoExtractor_PrivateFunction ──────────────────────────────────────────

func TestGoExtractor_PrivateFunction(t *testing.T) {
	src := `package util
func helper() {}`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "util/util.go", Language: "go", Content: []byte(src)}
	nodes, _ := e.Extract(root, fe)

	var found *types.CodeNode
	for i := range nodes {
		if nodes[i].Kind == types.NodeFunction && nodes[i].Name == "helper" {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no NodeFunction named 'helper'")
	}
	if found.Visibility != "private" {
		t.Errorf("Visibility = %q; want %q", found.Visibility, "private")
	}
}

// ── TestGoExtractor_MethodDeclaration ─────────────────────────────────────────

func TestGoExtractor_MethodDeclaration(t *testing.T) {
	src := `package http
type Handler struct{}
func (h *Handler) ServeHTTP() {}`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "http/handler.go", Language: "go", Content: []byte(src)}
	nodes, _ := e.Extract(root, fe)

	var found *types.CodeNode
	for i := range nodes {
		if nodes[i].Kind == types.NodeFunction && strings.Contains(nodes[i].Qualified, "Handler") && strings.Contains(nodes[i].Qualified, "ServeHTTP") {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no NodeFunction for Handler.ServeHTTP; nodes: %v", nodes)
	}
	// Qualified should be something like "http.Handler.ServeHTTP"
	if !strings.Contains(found.Qualified, "ServeHTTP") {
		t.Errorf("Qualified = %q; want it to contain 'ServeHTTP'", found.Qualified)
	}
}

// ── TestGoExtractor_TypeSpecStruct ────────────────────────────────────────────

func TestGoExtractor_TypeSpecStruct(t *testing.T) {
	src := `package model
type User struct { Name string }`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "model/user.go", Language: "go", Content: []byte(src)}
	nodes, _ := e.Extract(root, fe)

	var found *types.CodeNode
	for i := range nodes {
		if nodes[i].Kind == types.NodeClass && nodes[i].Name == "User" {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no NodeClass named 'User'; nodes: %v", nodes)
	}
	if !strings.Contains(found.Qualified, "User") {
		t.Errorf("Qualified = %q; want it to contain 'User'", found.Qualified)
	}
}

// ── TestGoExtractor_TypeSpecInterface ─────────────────────────────────────────

func TestGoExtractor_TypeSpecInterface(t *testing.T) {
	src := `package iface
type Stringer interface { String() string }`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "iface/iface.go", Language: "go", Content: []byte(src)}
	nodes, _ := e.Extract(root, fe)

	var found *types.CodeNode
	for i := range nodes {
		if nodes[i].Kind == types.NodeClass && nodes[i].Name == "Stringer" {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no NodeClass named 'Stringer'; nodes: %v", nodes)
	}
}

// ── TestGoExtractor_NonStructNonInterface ─────────────────────────────────────

func TestGoExtractor_NonStructNonInterface(t *testing.T) {
	src := `package e
type Status int`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "e/status.go", Language: "go", Content: []byte(src)}
	nodes, _ := e.Extract(root, fe)

	for _, n := range nodes {
		if n.Kind == types.NodeClass && n.Name == "Status" {
			t.Errorf("unexpected NodeClass for non-struct/non-interface type 'Status'")
		}
	}
}

// ── TestGoExtractor_ImportDeclaration ─────────────────────────────────────────

func TestGoExtractor_ImportDeclaration(t *testing.T) {
	src := `package main
import "fmt"
func F() { fmt.Println("x") }`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	var importEdge *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeImports && strings.Contains(edges[i].ToID, "fmt") {
			importEdge = &edges[i]
			break
		}
	}
	if importEdge == nil {
		t.Fatalf("no EdgeImports for 'fmt'; edges: %v", edges)
	}
}

// ── TestGoExtractor_ImportCreatesModuleNodes ─────────────────────────────────

func TestGoExtractor_ImportCreatesModuleNodes(t *testing.T) {
	src := `package main
import (
	"fmt"
	"golang.org/x/sync/errgroup"
)`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	nodes, edges := e.Extract(root, fe)

	// Verify module nodes exist.
	moduleNodes := map[string]*types.CodeNode{}
	for i := range nodes {
		if nodes[i].Kind == types.NodeModule {
			moduleNodes[nodes[i].Qualified] = &nodes[i]
		}
	}
	if _, ok := moduleNodes["fmt"]; !ok {
		t.Error("missing module node for 'fmt'")
	}
	if m, ok := moduleNodes["golang.org/x/sync/errgroup"]; !ok {
		t.Error("missing module node for 'golang.org/x/sync/errgroup'")
	} else {
		if m.Name != "errgroup" {
			t.Errorf("module Name = %q; want %q", m.Name, "errgroup")
		}
		if m.Language != "go" {
			t.Errorf("module Language = %q; want %q", m.Language, "go")
		}
	}

	// Verify corresponding import edges exist.
	importCount := 0
	for _, e := range edges {
		if e.Kind == types.EdgeImports {
			importCount++
		}
	}
	if importCount != 2 {
		t.Errorf("import edge count = %d; want 2", importCount)
	}
}

// ── TestGoExtractor_CallExpression ────────────────────────────────────────────

func TestGoExtractor_CallExpression(t *testing.T) {
	src := `package main
import "fmt"
func Run() { fmt.Println("x") }`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	var callEdge *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeCalls && strings.Contains(edges[i].FromID, "Run") {
			callEdge = &edges[i]
			break
		}
	}
	if callEdge == nil {
		t.Fatalf("no EdgeCalls from 'Run'; edges: %v", edges)
	}
}

// ── TestGoVisibility ──────────────────────────────────────────────────────────

func TestGoVisibility_Public(t *testing.T) {
	if got := goVisibility("Public"); got != "public" {
		t.Errorf("goVisibility(Public) = %q; want %q", got, "public")
	}
}

func TestGoVisibility_Private(t *testing.T) {
	if got := goVisibility("private"); got != "private" {
		t.Errorf("goVisibility(private) = %q; want %q", got, "private")
	}
}

func TestGoVisibility_Empty(t *testing.T) {
	if got := goVisibility(""); got != "private" {
		t.Errorf("goVisibility(\"\") = %q; want %q", got, "private")
	}
}

// ── TestGoPackageFromPath ─────────────────────────────────────────────────────

func TestGoPackageFromPath_Nested(t *testing.T) {
	got := goPackageFromPath("internal/adapters/surreal/client.go")
	if got != "surreal" {
		t.Errorf("goPackageFromPath = %q; want %q", got, "surreal")
	}
}

func TestGoPackageFromPath_Root(t *testing.T) {
	got := goPackageFromPath("main.go")
	if got != "main" {
		t.Errorf("goPackageFromPath = %q; want %q", got, "main")
	}
}

func TestGoPackageFromPath_SingleFile(t *testing.T) {
	got := goPackageFromPath("util.go")
	if got != "util" {
		t.Errorf("goPackageFromPath = %q; want %q", got, "util")
	}
}

// ── TestMakeNodeID_Lang ───────────────────────────────────────────────────────

func TestMakeNodeID_Lang(t *testing.T) {
	got := makeNodeID("function", "pkg.A/B.C")
	want := "function:pkg⋅A⋅B⋅C"
	if got != want {
		t.Errorf("makeNodeID = %q; want %q", got, want)
	}
}

// ── TestNodeText_NilNode ──────────────────────────────────────────────────────

func TestNodeText_NilNode(t *testing.T) {
	got := nodeText(nil, []byte("src"))
	if got != "" {
		t.Errorf("nodeText(nil, src) = %q; want %q", got, "")
	}
}

// ── TestDetectGoCallType_Goroutine ────────────────────────────────────────────

func TestDetectGoCallType_Goroutine(t *testing.T) {
	src := `package main
func F() { go helper() }
func helper() {}`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	for _, edge := range edges {
		if edge.Kind == types.EdgeCalls && strings.Contains(edge.FromID, "F") {
			if edge.CallType != "goroutine" {
				t.Errorf("CallType = %q; want %q", edge.CallType, "goroutine")
			}
			return
		}
	}
	t.Errorf("no EdgeCalls from 'F' found; edges: %v", edges)
}

// ── TestDetectGoCallType_Deferred ─────────────────────────────────────────────

func TestDetectGoCallType_Deferred(t *testing.T) {
	src := `package main
func F() { defer cleanup() }
func cleanup() {}`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	for _, edge := range edges {
		if edge.Kind == types.EdgeCalls && strings.Contains(edge.FromID, "F") {
			if edge.CallType != "deferred" {
				t.Errorf("CallType = %q; want %q", edge.CallType, "deferred")
			}
			return
		}
	}
	t.Errorf("no EdgeCalls from 'F' found; edges: %v", edges)
}

// ── TestGoExtractor_DocComment ────────────────────────────────────────────────

func TestGoExtractor_DocComment(t *testing.T) {
	src := `package main
// Hello greets someone.
func Hello() {}`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	nodes, _ := e.Extract(root, fe)

	var found *types.CodeNode
	for i := range nodes {
		if nodes[i].Kind == types.NodeFunction && nodes[i].Name == "Hello" {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no NodeFunction named 'Hello'")
	}
	if !strings.Contains(found.Docstring, "Hello greets") {
		t.Errorf("Docstring = %q; want it to contain 'Hello greets'", found.Docstring)
	}
}

// ── TestGoExtractor_PointerReceiver ────────────────────────────────────────────

func TestGoExtractor_PointerReceiver(t *testing.T) {
	src := `package svc
type Service struct{}
func (s *Service) Run() {}`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "svc/service.go", Language: "go", Content: []byte(src)}
	nodes, _ := e.Extract(root, fe)

	var found *types.CodeNode
	for i := range nodes {
		if nodes[i].Kind == types.NodeFunction && nodes[i].Name == "Run" {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no NodeFunction named 'Run'; nodes: %v", nodes)
	}
	if !strings.Contains(found.Qualified, "Service") {
		t.Errorf("Qualified = %q; want it to contain 'Service'", found.Qualified)
	}
}

// ── TestGoPackageFromPath_Empty ────────────────────────────────────────────────

func TestGoPackageFromPath_NoExtension(t *testing.T) {
	// File with no "." in name — returns basename.
	got := goPackageFromPath("myfile")
	if got != "myfile" {
		t.Errorf("goPackageFromPath = %q; want %q", got, "myfile")
	}
}

// ── TestGoExtractor_ReturnValueFlow ──────────────────────────────────────────

func TestGoExtractor_ReturnValueFlow_Simple(t *testing.T) {
	src := `package main

func process(input string) string { return input }
func sink(query string) {}

func Handler(input string) {
	result := process(input)
	sink(result)
}`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	var found *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeDataFlow &&
			edges[i].Metadata["flow_type"] == "return_value" &&
			edges[i].Metadata["via_var"] == "result" {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no return_value data_flow edge via 'result'; edges: %v", edges)
	}
	if found.Metadata["from_call"] != "process" {
		t.Errorf("from_call = %q; want %q", found.Metadata["from_call"], "process")
	}
	if !strings.Contains(found.ToID, "sink") {
		t.Errorf("ToID = %q; want it to contain 'sink'", found.ToID)
	}
}

func TestGoExtractor_ReturnValueFlow_MultiReturn(t *testing.T) {
	src := `package main

import "errors"

func fetch(url string) (string, error) { return "", errors.New("x") }
func parse(data string) {}

func Run() {
	data, err := fetch("http://example.com")
	_ = err
	parse(data)
}`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	// Should track "data" but NOT "err" or "_"
	var found *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeDataFlow &&
			edges[i].Metadata["flow_type"] == "return_value" &&
			edges[i].Metadata["via_var"] == "data" {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no return_value data_flow edge via 'data'; edges: %v", edges)
	}

	// Ensure "err" was NOT tracked
	for _, edge := range edges {
		if edge.Kind == types.EdgeDataFlow &&
			edge.Metadata["flow_type"] == "return_value" &&
			edge.Metadata["via_var"] == "err" {
			t.Error("should not track 'err' variable")
		}
	}
}

func TestGoExtractor_ReturnValueFlow_Chain(t *testing.T) {
	src := `package main

func step1(input string) string { return input }
func step2(data string) string { return data }
func final(query string) {}

func Pipeline(input string) {
	a := step1(input)
	b := step2(a)
	final(b)
}`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	// Should have two return_value edges: step1→step2 (via a) and step2→final (via b)
	returnFlowCount := 0
	viaVars := map[string]bool{}
	for _, edge := range edges {
		if edge.Kind == types.EdgeDataFlow && edge.Metadata["flow_type"] == "return_value" {
			returnFlowCount++
			viaVars[edge.Metadata["via_var"]] = true
		}
	}

	// "a" is used as arg to step2, and "b" is used as arg to final
	if !viaVars["a"] {
		t.Error("missing return_value edge via variable 'a'")
	}
	if !viaVars["b"] {
		t.Error("missing return_value edge via variable 'b'")
	}
	if returnFlowCount < 2 {
		t.Errorf("return_value edge count = %d; want >= 2", returnFlowCount)
	}
}

func TestGoExtractor_ReturnValueFlow_SelectorAccess(t *testing.T) {
	src := `package main

type User struct { Email string }

func getUser(id string) *User { return &User{Email: "test"} }
func sendEmail(email string) {}

func Notify(id string) {
	user := getUser(id)
	sendEmail(user.Email)
}`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	var found *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeDataFlow &&
			edges[i].Metadata["flow_type"] == "return_value" &&
			edges[i].Metadata["via_var"] == "user" {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no return_value data_flow edge via 'user'; edges: %v", edges)
	}
	if found.Metadata["field_path"] != "user.Email" {
		t.Errorf("field_path = %q; want %q", found.Metadata["field_path"], "user.Email")
	}
	if found.Metadata["from_call"] != "getUser" {
		t.Errorf("from_call = %q; want %q", found.Metadata["from_call"], "getUser")
	}
}

// ── TestGoExtractor_RouteExtraction ──────────────────────────────────────────

func TestGoExtractor_RouteExtraction_Echo(t *testing.T) {
	src := `package http

func registerRoutes(e *echo.Echo) {
	e.GET("/health", handleHealth)
	v1 := e.Group("/api/v1")
	v1.GET("/repos", handleListRepos)
	v1.POST("/repos", handleCreateRepo)
	v1.DELETE("/repos/:slug", handleDeleteRepo)
}
`
	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "http/server.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	// Collect route edges by method+path key
	type routeKey struct{ method, path string }
	routes := map[routeKey]*types.CodeEdge{}
	for i := range edges {
		if edges[i].Kind == types.EdgeRoute {
			k := routeKey{edges[i].Metadata["http_method"], edges[i].Metadata["http_path"]}
			routes[k] = &edges[i]
		}
	}

	// GET /health — direct route (no group prefix)
	if r, ok := routes[routeKey{"GET", "/health"}]; !ok {
		t.Error("missing route for GET /health")
	} else if !strings.Contains(r.ToID, "handleHealth") {
		t.Errorf("/health handler = %q; want handleHealth", r.ToID)
	}

	// GET /api/v1/repos — group prefix resolved
	if _, ok := routes[routeKey{"GET", "/api/v1/repos"}]; !ok {
		t.Error("missing route for GET /api/v1/repos")
	}

	// POST /api/v1/repos
	if _, ok := routes[routeKey{"POST", "/api/v1/repos"}]; !ok {
		t.Error("missing route for POST /api/v1/repos")
	}

	// DELETE /api/v1/repos/:slug
	if _, ok := routes[routeKey{"DELETE", "/api/v1/repos/:slug"}]; !ok {
		t.Error("missing route for DELETE /api/v1/repos/:slug")
	}
}

func TestGoExtractor_RouteExtraction_WithMiddleware(t *testing.T) {
	src := `package http

func registerRoutes(e *echo.Echo) {
	v1 := e.Group("/api", authMiddleware)
	v1.Use(rateLimiter)
	v1.GET("/users", handleUsers, validateInput)
}
`
	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "http/server.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	var routeEdge *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeRoute {
			routeEdge = &edges[i]
			break
		}
	}
	if routeEdge == nil {
		t.Fatal("no EdgeRoute found")
	}

	if routeEdge.Metadata["http_path"] != "/api/users" {
		t.Errorf("path = %q; want /api/users", routeEdge.Metadata["http_path"])
	}

	// Middleware should include group-level (authMiddleware, rateLimiter) + route-level (validateInput)
	mw := routeEdge.Metadata["middleware"]
	if !strings.Contains(mw, "authMiddleware") {
		t.Errorf("middleware = %q; should contain authMiddleware", mw)
	}
	if !strings.Contains(mw, "rateLimiter") {
		t.Errorf("middleware = %q; should contain rateLimiter", mw)
	}
	if !strings.Contains(mw, "validateInput") {
		t.Errorf("middleware = %q; should contain validateInput", mw)
	}
}

func TestGoExtractor_RouteExtraction_StdLib(t *testing.T) {
	src := `package main

func main() {
	http.HandleFunc("/api/data", handleData)
}
`
	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	var found *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeRoute {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no EdgeRoute for http.HandleFunc")
	}
	if found.Metadata["http_path"] != "/api/data" {
		t.Errorf("path = %q; want /api/data", found.Metadata["http_path"])
	}
	if !strings.Contains(found.ToID, "handleData") {
		t.Errorf("handler = %q; want handleData", found.ToID)
	}
}

// ── TestGoExtractor_Bindings ────────────────────────────────────────────────

func TestGoExtractor_Bindings_Param(t *testing.T) {
	src := `package http

func handleGetUser(c echo.Context) error {
	id := c.Param("id")
	_ = id
	return nil
}
`
	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "http/handler.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	var found *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeDataFlow &&
			edges[i].Metadata["source_type"] == "path_param" {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no path_param binding edge found")
	}
	if found.Metadata["param_name"] != "id" {
		t.Errorf("param_name = %q; want %q", found.Metadata["param_name"], "id")
	}
}

func TestGoExtractor_Bindings_JSON(t *testing.T) {
	src := `package http

func handleGetUser(c echo.Context) error {
	user := getUser()
	return c.JSON(200, user)
}
`
	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "http/handler.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	var found *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeDataFlow &&
			edges[i].Metadata["source_type"] == "response" {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no response binding edge found")
	}
	if found.Metadata["response_expr"] != "user" {
		t.Errorf("response_expr = %q; want %q", found.Metadata["response_expr"], "user")
	}
}

// ── TestGoExtractor_CFG ─────────────────────────────────────────────────────

func TestGoExtractor_CFG_IfElse(t *testing.T) {
	src := `package main

func Handler(ok bool) {
	x := 1
	if ok {
		x = 2
	} else {
		x = 3
	}
	use(x)
}
`
	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	cfgEdges := filterEdges(edges, types.EdgeControlFlow)
	if len(cfgEdges) == 0 {
		t.Fatal("no control_flow edges found")
	}

	// Should have if_true and if_false branches.
	hasTrueBranch := false
	hasFalseBranch := false
	for _, edge := range cfgEdges {
		switch edge.Metadata["branch_type"] {
		case "if_true":
			hasTrueBranch = true
		case "if_false":
			hasFalseBranch = true
		}
	}
	if !hasTrueBranch {
		t.Error("missing if_true branch edge")
	}
	if !hasFalseBranch {
		t.Error("missing if_false branch edge")
	}
}

func TestGoExtractor_CFG_ForLoop(t *testing.T) {
	src := `package main

func Process() {
	for i := 0; i < 10; i++ {
		work(i)
	}
	done()
}
`
	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	cfgEdges := filterEdges(edges, types.EdgeControlFlow)

	hasLoopEntry := false
	hasLoopBack := false
	for _, edge := range cfgEdges {
		switch edge.Metadata["branch_type"] {
		case "loop_entry":
			hasLoopEntry = true
		case "loop_back":
			hasLoopBack = true
		}
	}
	if !hasLoopEntry {
		t.Error("missing loop_entry edge")
	}
	if !hasLoopBack {
		t.Error("missing loop_back edge")
	}
}

func TestGoExtractor_CFG_Return(t *testing.T) {
	src := `package main

func Early(ok bool) int {
	if !ok {
		return 0
	}
	return 1
}
`
	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	cfgEdges := filterEdges(edges, types.EdgeControlFlow)

	hasReturn := false
	for _, edge := range cfgEdges {
		if edge.Metadata["branch_type"] == "return" {
			hasReturn = true
			break
		}
	}
	if !hasReturn {
		t.Error("missing return edge")
	}
}

// ── TestGoExtractor_DataDep ─────────────────────────────────────────────────

func TestGoExtractor_DataDep_ParamToUse(t *testing.T) {
	src := `package main

func Handler(input string) {
	sink(input)
}
`
	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	depEdges := filterEdges(edges, types.EdgeDataDep)
	var found *types.CodeEdge
	for i := range depEdges {
		if depEdges[i].Metadata["var_name"] == "input" {
			found = &depEdges[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no data_dep edge for 'input'")
	}
	if found.Metadata["def_type"] != "parameter" {
		t.Errorf("def_type = %q; want parameter", found.Metadata["def_type"])
	}
}

func TestGoExtractor_DataDep_AssignmentToUse(t *testing.T) {
	src := `package main

func Handler() {
	result := process()
	sink(result)
}
`
	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	depEdges := filterEdges(edges, types.EdgeDataDep)
	var found *types.CodeEdge
	for i := range depEdges {
		if depEdges[i].Metadata["var_name"] == "result" {
			found = &depEdges[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no data_dep edge for 'result'")
	}
	if found.Metadata["def_type"] != "return_value" {
		t.Errorf("def_type = %q; want return_value", found.Metadata["def_type"])
	}
}

func TestGoExtractor_DataDep_SkipsErrAndBlank(t *testing.T) {
	src := `package main

func Handler() {
	_, err := process()
	_ = err
	handle(err)
}
`
	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	depEdges := filterEdges(edges, types.EdgeDataDep)
	for _, edge := range depEdges {
		if edge.Metadata["var_name"] == "_" {
			t.Error("should not track blank identifier '_'")
		}
		if edge.Metadata["var_name"] == "err" {
			t.Error("should not track 'err' variable")
		}
	}
}

// filterEdges returns edges matching the given kind.
func filterEdges(edges []types.CodeEdge, kind types.EdgeKind) []types.CodeEdge {
	var result []types.CodeEdge
	for _, e := range edges {
		if e.Kind == kind {
			result = append(result, e)
		}
	}
	return result
}

func TestGoExtractor_ReturnValueFlow_NoLiterals(t *testing.T) {
	// Return values assigned but only passed as literals should NOT produce edges
	src := `package main

func compute() int { return 42 }
func use(x int) {}

func F() {
	_ = compute()
	use(123)
}`

	root := parseGoAST(t, src)
	e := &GoExtractor{}
	fe := domain.FileEntry{Path: "main.go", Language: "go", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	for _, edge := range edges {
		if edge.Kind == types.EdgeDataFlow && edge.Metadata["flow_type"] == "return_value" {
			t.Errorf("unexpected return_value edge: %+v", edge)
		}
	}
}
