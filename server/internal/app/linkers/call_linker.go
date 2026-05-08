package linkers

import (
	"strings"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// CallLinker resolves call-type edges (calls, constructs, tests) against
// the global symbol table. It also classifies each edge into the correct
// label based on the calling function's context:
//
//   - calls:      runtime behavioral dependency (default)
//   - constructs: initialization/wiring (New*, init, wire*, main)
//   - tests:      test invocations (_test.go, Test*, Benchmark*)
//
// This follows OpenCodeGraph's principle: different relationships get
// different labels. Traversal is label-parameterized, so blast radius
// naturally follows only "calls" edges without construction/test noise.
type CallLinker struct{}

func (l *CallLinker) Name() string { return "call" }

// Labels returns every edge kind CallLinker may produce. The linker examines
// EdgeCalls inputs and may reclassify each to EdgeTests (test caller) or
// EdgeConstructs (constructor / init / wire*). Declaring all three lets the
// pipeline (and dashboards) reflect what the linker actually emits.
func (l *CallLinker) Labels() []types.EdgeKind {
	return []types.EdgeKind{types.EdgeCalls, types.EdgeTests, types.EdgeConstructs}
}

func (l *CallLinker) Link(edges []types.CodeEdge, sym *domain.SymbolTable) ([]types.CodeEdge, domain.LinkStats) {
	stats := domain.LinkStats{LinkerName: l.Name()}

	for i := range edges {
		e := &edges[i]
		if e.Kind != types.EdgeCalls {
			continue
		}

		if isResolved(e.ToID) {
			// Already resolved — still classify the edge label.
			e.Kind = classifyCallKind(e.FromID, sym)
			e.Confidence = 1.0
			e.Provenance = &types.Provenance{
				Source:    "call_linker",
				Method:    "pre_resolved",
				CreatedAt: time.Now(),
			}
			continue
		}
		stats.Processed++

		resolved, ok := sym.Resolve(e.ToID, e.FromID)
		if ok {
			e.ToID = resolved
			e.Confidence = 1.0
			stats.Resolved++
		} else {
			e.Confidence = 0.5
			stats.Unresolved++
		}

		e.Provenance = &types.Provenance{
			Source:    "call_linker",
			Method:    "symbol_resolution",
			CreatedAt: time.Now(),
		}

		// Classify the edge label based on the caller's context.
		e.Kind = classifyCallKind(e.FromID, sym)
	}

	return edges, stats
}

// classifyCallKind determines whether a call edge is a runtime call,
// construction/init, or test invocation based on the calling function.
func classifyCallKind(callerID string, sym *domain.SymbolTable) types.EdgeKind {
	meta, ok := sym.Nodes[callerID]
	if !ok {
		return types.EdgeCalls // unknown caller — default to runtime
	}

	// Test: caller is in a test file or named Test*/Benchmark*
	if strings.Contains(meta.FilePath, "_test.go") || strings.Contains(meta.FilePath, "_test.") {
		return types.EdgeTests
	}
	if strings.HasPrefix(meta.Name, "Test") || strings.HasPrefix(meta.Name, "Benchmark") {
		return types.EdgeTests
	}

	// Construction: caller is a constructor, init, main, or wiring function
	if strings.HasPrefix(meta.Name, "New") {
		return types.EdgeConstructs
	}
	if meta.Name == "init" || meta.Name == "main" {
		return types.EdgeConstructs
	}
	if strings.HasPrefix(meta.Name, "wire") || strings.HasPrefix(meta.Name, "setup") || strings.HasPrefix(meta.Name, "register") {
		return types.EdgeConstructs
	}
	if strings.HasSuffix(meta.FilePath, "wire.go") || strings.HasSuffix(meta.FilePath, "main.go") {
		return types.EdgeConstructs
	}

	return types.EdgeCalls // default: runtime behavioral call
}

// isResolved returns true if the ID is already a concrete node ID (has ":" separator).
func isResolved(id string) bool {
	return strings.Contains(id, ":")
}
