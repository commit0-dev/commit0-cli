// CGO_ENABLED=1 required — tree-sitter uses C libraries.
package lang

import (
	"context"
	"strings"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	gositter "github.com/smacker/go-tree-sitter/golang"

	"github.com/commit0-dev/commit0/internal/domain"
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
