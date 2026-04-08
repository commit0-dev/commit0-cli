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

// ── TestPythonExtractor_ReturnValueFlow ─────────────────────────────────────

func TestPythonExtractor_ReturnValueFlow_Simple(t *testing.T) {
	src := `def handler(input):
    result = process(input)
    sink(result)
`

	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "app.py", Language: "python", Content: []byte(src)}
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
}

func TestPythonExtractor_ReturnValueFlow_Chain(t *testing.T) {
	src := `def pipeline(input):
    a = step1(input)
    b = step2(a)
    final(b)
`

	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "pipeline.py", Language: "python", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	viaVars := map[string]bool{}
	for _, edge := range edges {
		if edge.Kind == types.EdgeDataFlow && edge.Metadata["flow_type"] == "return_value" {
			viaVars[edge.Metadata["via_var"]] = true
		}
	}
	if !viaVars["a"] {
		t.Error("missing return_value edge via variable 'a'")
	}
	if !viaVars["b"] {
		t.Error("missing return_value edge via variable 'b'")
	}
}

// ── TestPythonExtractor_CFG ──────────────────────────────────────────────────

func TestPythonExtractor_CFG_IfElse(t *testing.T) {
	src := `def handler(ok):
    x = 1
    if ok:
        x = 2
    else:
        x = 3
    use(x)
`
	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "app.py", Language: "python", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	cfgEdges := pyFilterEdges(edges, types.EdgeControlFlow)
	if len(cfgEdges) == 0 {
		t.Fatal("no control_flow edges found")
	}

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

func TestPythonExtractor_DataDep_ParamToUse(t *testing.T) {
	src := `def handler(input):
    sink(input)
`
	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "app.py", Language: "python", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	depEdges := pyFilterEdges(edges, types.EdgeDataDep)
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

func pyFilterEdges(edges []types.CodeEdge, kind types.EdgeKind) []types.CodeEdge {
	var result []types.CodeEdge
	for _, e := range edges {
		if e.Kind == kind {
			result = append(result, e)
		}
	}
	return result
}

// ── TestPythonExtractor_RouteExtraction ─────────────────────────────────────

func TestPythonExtractor_RouteExtraction_FlaskGet(t *testing.T) {
	src := `from flask import Flask
app = Flask(__name__)

@app.get("/users")
def list_users():
    return []
`
	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "app.py", Language: "python", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	var found *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeRoute {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no EdgeRoute found for @app.get")
	}
	if found.Metadata["http_method"] != "GET" {
		t.Errorf("method = %q; want GET", found.Metadata["http_method"])
	}
	if found.Metadata["http_path"] != "/users" {
		t.Errorf("path = %q; want /users", found.Metadata["http_path"])
	}
	if !strings.Contains(found.ToID, "list_users") {
		t.Errorf("handler = %q; want list_users", found.ToID)
	}
}

func TestPythonExtractor_RouteExtraction_FlaskRoute(t *testing.T) {
	src := `from flask import Flask
app = Flask(__name__)

@app.route("/data", methods=["POST"])
def create_data():
    pass
`
	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "app.py", Language: "python", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	var found *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeRoute {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no EdgeRoute found for @app.route")
	}
	if found.Metadata["http_method"] != "POST" {
		t.Errorf("method = %q; want POST", found.Metadata["http_method"])
	}
	if found.Metadata["http_path"] != "/data" {
		t.Errorf("path = %q; want /data", found.Metadata["http_path"])
	}
}

func TestPythonExtractor_RouteExtraction_FastAPI(t *testing.T) {
	src := `from fastapi import FastAPI
app = FastAPI()

@app.post("/items")
def create_item(item: Item):
    return item
`
	root := parsePyAST(t, src)
	e := &PythonExtractor{}
	fe := domain.FileEntry{Path: "main.py", Language: "python", Content: []byte(src)}
	_, edges := e.Extract(root, fe)

	var found *types.CodeEdge
	for i := range edges {
		if edges[i].Kind == types.EdgeRoute {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no EdgeRoute found for @app.post")
	}
	if found.Metadata["http_method"] != "POST" {
		t.Errorf("method = %q; want POST", found.Metadata["http_method"])
	}
	if found.Metadata["http_path"] != "/items" {
		t.Errorf("path = %q; want /items", found.Metadata["http_path"])
	}
}
