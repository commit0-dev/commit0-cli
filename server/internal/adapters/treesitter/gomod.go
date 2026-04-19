package treesitter

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ParseGoMod extracts module nodes from a go.mod file. It parses the module
// directive (the repo's own module path) and all require directives (direct
// and indirect dependencies), returning them as CodeNode entries with
// Kind=NodeModule. No tree-sitter grammar is needed — go.mod has a simple
// line-oriented format.
func ParseGoMod(_ context.Context, file domain.FileEntry) (*domain.ParsedFile, error) {
	if file.Path == "" {
		return nil, domain.Validation("file path must not be empty")
	}
	if len(file.Content) == 0 {
		return nil, domain.Validation("go.mod has empty content")
	}

	var nodes []types.CodeNode
	var edges []types.CodeEdge
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(bytes.NewReader(file.Content))
	inRequire := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip blanks and comments.
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		// Handle require block open/close.
		if strings.HasPrefix(line, "require (") || strings.HasPrefix(line, "require(") {
			inRequire = true
			continue
		}
		if inRequire && line == ")" {
			inRequire = false
			continue
		}

		// Single-line require: require golang.org/x/sync v0.7.0
		if strings.HasPrefix(line, "require ") && !strings.Contains(line, "(") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				addGoModDep(&nodes, seen, parts[1], parts[2])
			}
			continue
		}

		// Inside require block: golang.org/x/sync v0.7.0 // indirect
		if inRequire {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				addGoModDep(&nodes, seen, parts[0], parts[1])
			}
			continue
		}

		// Module directive: module github.com/commit0-dev/commit0
		if strings.HasPrefix(line, "module ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				modPath := parts[1]
				if !seen[modPath] {
					seen[modPath] = true
					nodes = append(nodes, types.CodeNode{
						ID:        makeNodeID(string(types.NodeModule), modPath),
						Kind:      types.NodeModule,
						Name:      goModName(modPath),
						Qualified: modPath,
						FilePath:  modPath,
						Language:  "go",
						Docstring: "module root",
					})
				}
			}
		}
	}

	sum := sha256.Sum256(file.Content)
	return &domain.ParsedFile{
		Path:        file.Path,
		Language:    "gomod",
		ContentHash: hex.EncodeToString(sum[:]),
		Nodes:       nodes,
		Edges:       edges,
		LineCount:   countLines(file.Content),
		SizeBytes:   len(file.Content),
	}, nil
}

// addGoModDep appends a module CodeNode for a require directive.
func addGoModDep(nodes *[]types.CodeNode, seen map[string]bool, modPath, version string) {
	if seen[modPath] {
		return
	}
	seen[modPath] = true
	*nodes = append(*nodes, types.CodeNode{
		ID:        makeNodeID(string(types.NodeModule), modPath),
		Kind:      types.NodeModule,
		Name:      goModName(modPath),
		Qualified: modPath,
		FilePath:  modPath,
		Language:  "go",
		Docstring: version, // store version in docstring for search/display
	})
}

// goModName returns the last path segment of a Go module path.
func goModName(modPath string) string {
	if idx := strings.LastIndex(modPath, "/"); idx >= 0 {
		return modPath[idx+1:]
	}
	return modPath
}
