package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ResolveSymbol finds a code node by name with precision.
//
// Strategy:
//  1. Exact qualified match via FindNode — confident, return immediately
//  2. Suffix match across all nodes — if exactly 1 match, return it
//  3. If multiple suffix matches — return AmbiguousSymbolError with all candidates
//  4. If 0 suffix matches — vector search fallback (embedder may be nil)
func ResolveSymbol(ctx context.Context, graph domain.OpenCodeGraph, embedder domain.Embedder, repo, symbol string) (*types.CodeNode, error) {
	// 1. Exact qualified match
	node, err := graph.FindNode(ctx, repo, symbol)
	if err == nil && node != nil {
		return node, nil
	}

	// 2. Suffix match — get lightweight ID list, filter by suffix, then fetch full nodes.
	// IDsOnly avoids loading 3000+ full nodes with embeddings.
	idNodes, listErr := graph.ListNodes(ctx, repo, domain.ListOpts{IDsOnly: true})
	if listErr == nil {
		suffix := "." + symbol
		var matchIDs []string
		for _, n := range idNodes {
			// IDs have format "table:pkg.Type.Method" — extract qualified from after ":"
			q := n.ID
			if idx := strings.IndexByte(q, ':'); idx >= 0 {
				q = q[idx+1:]
			}
			// Replace ID separator (⋅) with dot for matching
			q = strings.ReplaceAll(q, "\u22c5", ".")
			if q == symbol || (len(q) > len(suffix) && q[len(q)-len(suffix):] == suffix) {
				matchIDs = append(matchIDs, n.ID)
			}
		}

		if len(matchIDs) == 1 {
			node, err := graph.GetNode(ctx, matchIDs[0])
			if err == nil && node != nil {
				return node, nil
			}
		} else if len(matchIDs) > 1 {
			// Fetch full nodes for the candidates to build the error message.
			ambig := make([]domain.SymbolCandidate, 0, len(matchIDs))
			for _, id := range matchIDs {
				node, err := graph.GetNode(ctx, id)
				if err == nil && node != nil {
					ambig = append(ambig, domain.SymbolCandidate{
						Qualified: node.Qualified,
						FilePath:  node.FilePath,
						StartLine: node.StartLine,
					})
				}
			}
			if len(ambig) > 1 {
				return nil, &domain.AmbiguousSymbolError{
					Symbol:     symbol,
					Candidates: ambig,
				}
			}
			if len(ambig) == 1 {
				node, _ := graph.GetNode(ctx, matchIDs[0])
				return node, nil
			}
		}
	}

	// 3. Vector search fallback
	if embedder != nil {
		vec, err := embedder.EmbedQuery(ctx, symbol)
		if err == nil {
			hits, err := graph.VectorSearch(ctx, vec, domain.VectorSearchOpts{
				RepoSlug: repo,
				TopK:     1,
				MinScore: 0.8,
			})
			if err == nil && len(hits) > 0 {
				return &hits[0].Node, nil
			}
		}
	}

	return nil, domain.NotFound(fmt.Sprintf("symbol %s not found", symbol))
}
