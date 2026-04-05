// CGO_ENABLED=1 required — tree-sitter uses C libraries.
package lang

import (
	"context"
	"strings"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	pysitter "github.com/smacker/go-tree-sitter/python"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// ── helper ────────────────────────────────────────────────────────────────────

func parsePyAST(t *testing.T, src string) *sitter.Node {
	t.Helper()
	p := sitter.NewParser()
	p.SetLanguage(pysitter.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parsePyAST: %v", err)
	}
	t.Cleanup(func() { tree.Close() })
	return tree.RootNode()
}

// ── TestPythonExtractor_Language ─────────────────────────────────────────────

func TestPythonExtractor_Language(t *testing.T) {
	e := &PythonExtractor{}
	if e.Language() == nil {
		t.Error("Language() returned nil")
	}
}

// ── TestPythonExtractor_FunctionDef ──────────────────────────────────────────

func TestPythonExtractor_FunctionDef(t *testing.T) {
	src := "def greet(name):\n    return 'hi'\n"

	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "greet.py", Language: "python", Content: []byte(src)}
	nodes, _ := e.Extract(root, fe)

	var found *types.CodeNode
	for i := range nodes {
		if nodes[i].Kind == types.NodeFunction && nodes[i].Name == "greet" {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no NodeFunction named 'greet'; nodes: %v", nodes)
	}
}

// ── TestPythonExtractor_ClassDef ──────────────────────────────────────────────

func TestPythonExtractor_ClassDef(t *testing.T) {
	src := "class Animal:\n    pass\n"

	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "animal.py", Language: "python", Content: []byte(src)}
	nodes, _ := e.Extract(root, fe)

	var found *types.CodeNode
	for i := range nodes {
		if nodes[i].Kind == types.NodeClass && nodes[i].Name == "Animal" {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no NodeClass named 'Animal'; nodes: %v", nodes)
	}
}

// ── TestPythonExtractor_DecoratedFunction ─────────────────────────────────────

func TestPythonExtractor_DecoratedFunction(t *testing.T) {
	src := "@staticmethod\ndef decorated():\n    pass\n"

	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "deco.py", Language: "python", Content: []byte(src)}
	nodes, _ := e.Extract(root, fe)

	var found *types.CodeNode
	for i := range nodes {
		if nodes[i].Kind == types.NodeFunction && nodes[i].Name == "decorated" {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no NodeFunction named 'decorated' (expected decorated_definition to be unwrapped); nodes: %v", nodes)
	}
}

// ── TestPythonExtractor_Import ────────────────────────────────────────────────

func TestPythonExtractor_Import(t *testing.T) {
	src := "import os\ndef f(): pass\n"

	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "main.py", Language: "python", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	var found *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeImports && strings.Contains(edges[i].ToID, "os") {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no EdgeImports for 'os'; edges: %v", edges)
	}
}

// ── TestPythonExtractor_ImportFrom ────────────────────────────────────────────

func TestPythonExtractor_ImportFrom(t *testing.T) {
	src := "from pathlib import Path\ndef f(): pass\n"

	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "main.py", Language: "python", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	var found *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeImports && strings.Contains(edges[i].ToID, "pathlib") {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no EdgeImports for 'pathlib'; edges: %v", edges)
	}
}

// ── TestPythonExtractor_CallInFunction ───────────────────────────────────────

func TestPythonExtractor_CallInFunction(t *testing.T) {
	src := "def main():\n    print('hello')\n"

	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "main.py", Language: "python", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	var found *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeCalls {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no EdgeCalls found; edges: %v", edges)
	}
	if found.CallType != "direct" {
		t.Errorf("CallType = %q; want %q", found.CallType, "direct")
	}
}

// ── TestPythonExtractor_PrivateFunction ──────────────────────────────────────

func TestPythonExtractor_PrivateFunction(t *testing.T) {
	src := "def _helper():\n    pass\n"

	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "util.py", Language: "python", Content: []byte(src)}
	nodes, _ := e.Extract(root, fe)

	var found *types.CodeNode
	for i := range nodes {
		if nodes[i].Kind == types.NodeFunction && nodes[i].Name == "_helper" {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no NodeFunction named '_helper'; nodes: %v", nodes)
	}
	if found.Visibility != "private" {
		t.Errorf("Visibility = %q; want %q", found.Visibility, "private")
	}
}

// ── TestPyModuleFromPath ──────────────────────────────────────────────────────

func TestPyModuleFromPath_Nested(t *testing.T) {
	got := pyModuleFromPath("myapp/utils/helpers.py")
	if got != "myapp.utils.helpers" {
		t.Errorf("pyModuleFromPath = %q; want %q", got, "myapp.utils.helpers")
	}
}

func TestPyModuleFromPath_Root(t *testing.T) {
	got := pyModuleFromPath("main.py")
	if got != "main" {
		t.Errorf("pyModuleFromPath = %q; want %q", got, "main")
	}
}

// ── TestPythonExtractor_ClassInheritance ─────────────────────────────────────

func TestPythonExtractor_ClassInheritance(t *testing.T) {
	src := "class Dog(Animal):\n    pass\n"

	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "dog.py", Language: "python", Content: []byte(src)}
	nodes, edges := e.Extract(root, fe)

	// Find the Dog class node
	var dogNode *types.CodeNode
	for i := range nodes {
		if nodes[i].Kind == types.NodeClass && nodes[i].Name == "Dog" {
			dogNode = &nodes[i]
			break
		}
	}
	if dogNode == nil {
		t.Fatalf("no NodeClass named 'Dog'; nodes: %v", nodes)
	}

	// Find the EdgeInherits from Dog → Animal
	var found *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeInherits && edges[i].FromID == dogNode.ID {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no EdgeInherits from Dog; edges: %v", edges)
	}
	if !strings.Contains(found.ToID, "Animal") {
		t.Errorf("EdgeInherits ToID = %q; want it to contain 'Animal'", found.ToID)
	}
}

// ── TestPythonExtractor_Docstring ─────────────────────────────────────────────

func TestPythonExtractor_Docstring(t *testing.T) {
	src := "def greet():\n    \"\"\"Say hello.\"\"\"\n    pass\n"

	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "greet.py", Language: "python", Content: []byte(src)}
	nodes, _ := e.Extract(root, fe)

	var found *types.CodeNode
	for i := range nodes {
		if nodes[i].Kind == types.NodeFunction && nodes[i].Name == "greet" {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no NodeFunction named 'greet'")
	}
	if found.Docstring == "" {
		t.Error("expected non-empty Docstring, got empty")
	}
}

// ── TestPythonExtractor_AliasedImport ─────────────────────────────────────────

func TestPythonExtractor_AliasedImport(t *testing.T) {
	src := "import numpy as np\ndef f(): pass\n"

	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "main.py", Language: "python", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	var found *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeImports && strings.Contains(edges[i].ToID, "numpy") {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no EdgeImports for 'numpy'; edges: %v", edges)
	}
}

// ── TestPythonExtractor_MethodInClass ─────────────────────────────────────────

func TestPythonExtractor_MethodInClass(t *testing.T) {
	src := "class Svc:\n    def handle(self, req):\n        pass\n"

	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "svc.py", Language: "python", Content: []byte(src)}
	nodes, _ := e.Extract(root, fe)

	var found *types.CodeNode
	for i := range nodes {
		if nodes[i].Kind == types.NodeFunction && nodes[i].Name == "handle" {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no NodeFunction 'handle'; nodes: %v", nodes)
	}
	if !strings.Contains(found.Qualified, "Svc") {
		t.Errorf("Qualified = %q; want it to contain 'Svc'", found.Qualified)
	}
}

// ── TestPythonExtractor_ReturnAnnotation ─────────────────────────────────────

func TestPythonExtractor_ReturnAnnotation(t *testing.T) {
	src := "def compute(x: int) -> int:\n    return x * 2\n"

	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "compute.py", Language: "python", Content: []byte(src)}
	nodes, _ := e.Extract(root, fe)

	var found *types.CodeNode
	for i := range nodes {
		if nodes[i].Kind == types.NodeFunction && nodes[i].Name == "compute" {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no NodeFunction 'compute'")
	}
	if !strings.Contains(found.Signature, "->") {
		t.Errorf("Signature = %q; expected to contain '->'", found.Signature)
	}
}
