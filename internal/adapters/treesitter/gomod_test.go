package treesitter

import (
	"context"
	"testing"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

func TestParseGoMod_RequireBlock(t *testing.T) {
	content := []byte(`module github.com/commit0-dev/commit0

go 1.26

require (
	github.com/smacker/go-tree-sitter v0.0.0-20240827094848
	golang.org/x/sync v0.7.0
	github.com/surrealdb/surrealdb.go v1.0.0 // indirect
)

require github.com/spf13/cobra v1.8.0
`)

	file := domain.FileEntry{
		Path:     "go.mod",
		Language: "gomod",
		Content:  content,
	}

	result, err := ParseGoMod(context.Background(), file)
	if err != nil {
		t.Fatalf("ParseGoMod: %v", err)
	}

	if result.Language != "gomod" {
		t.Errorf("Language = %q; want %q", result.Language, "gomod")
	}

	// Collect module nodes by qualified name.
	modules := make(map[string]*types.CodeNode)
	for i := range result.Nodes {
		if result.Nodes[i].Kind == types.NodeModule {
			modules[result.Nodes[i].Qualified] = &result.Nodes[i]
		}
	}

	// Expect: the module itself + 4 dependencies = 5 modules.
	if len(modules) != 5 {
		t.Errorf("module count = %d; want 5; modules: %v", len(modules), modules)
	}

	// Check the repo's own module.
	if m, ok := modules["github.com/commit0-dev/commit0"]; !ok {
		t.Error("missing module node for repo module path")
	} else if m.Docstring != "module root" {
		t.Errorf("repo module Docstring = %q; want %q", m.Docstring, "module root")
	}

	// Check a dependency with version.
	if m, ok := modules["golang.org/x/sync"]; !ok {
		t.Error("missing module node for golang.org/x/sync")
	} else {
		if m.Name != "sync" {
			t.Errorf("Name = %q; want %q", m.Name, "sync")
		}
		if m.Docstring != "v0.7.0" {
			t.Errorf("Docstring (version) = %q; want %q", m.Docstring, "v0.7.0")
		}
	}

	// Check single-line require.
	if _, ok := modules["github.com/spf13/cobra"]; !ok {
		t.Error("missing module node for single-line require github.com/spf13/cobra")
	}
}

func TestParseGoMod_EmptyContent(t *testing.T) {
	file := domain.FileEntry{Path: "go.mod", Language: "gomod", Content: nil}
	_, err := ParseGoMod(context.Background(), file)
	if err == nil {
		t.Error("expected validation error for empty content")
	}
}

func TestParseGoMod_EmptyPath(t *testing.T) {
	file := domain.FileEntry{Path: "", Language: "gomod", Content: []byte("module foo")}
	_, err := ParseGoMod(context.Background(), file)
	if err == nil {
		t.Error("expected validation error for empty path")
	}
}
