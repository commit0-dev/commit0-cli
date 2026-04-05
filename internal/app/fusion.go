package app

import (
	"math"
	"sort"

	"github.com/commit0-dev/commit0/pkg/types"
)

// RRFWeights configures the Reciprocal Rank Fusion scoring
type RRFWeights struct {
	Vector float64 // Weight for vector similarity scores
	FTS    float64 // Weight for full-text search scores
	K      float64 // Smoothing constant to prevent division by small ranks
}

// DefaultRRFWeights returns recommended RRF configuration
func DefaultRRFWeights() RRFWeights {
	return RRFWeights{Vector: 1.0, FTS: 1.0, K: 60}
}

// ReciprocalRankFusion merges vector and FTS search results using RRF formula
func ReciprocalRankFusion(vector []types.ScoredNode, fts []types.ScoredNode, w RRFWeights) []types.ScoredNode {
	if w.K < 0 {
		w.K = 60
	}

	// Map to accumulate scores: node ID -> (node, score)
	scores := make(map[string]float64)
	nodes := make(map[string]types.ScoredNode)

	// Process vector results
	for rank, node := range vector {
		rankScore := w.Vector / (w.K + float64(rank+1))
		scores[node.Node.ID] += rankScore

		if _, exists := nodes[node.Node.ID]; !exists {
			nodes[node.Node.ID] = node
		}
	}

	// Process FTS results
	for rank, node := range fts {
		rankScore := w.FTS / (w.K + float64(rank+1))
		scores[node.Node.ID] += rankScore

		if _, exists := nodes[node.Node.ID]; !exists {
			nodes[node.Node.ID] = node
		}
	}

	// Apply centrality boost: score *= log(1 + centrality)
	// Guard against Centrality=0 which would zero out scores
	for id, node := range nodes {
		score := scores[id]
		boost := math.Max(1.0, math.Log1p(float64(node.Centrality)))
		scores[id] = score * boost
	}

	// Build result slice with updated fused scores
	result := make([]types.ScoredNode, 0, len(nodes))
	for id, node := range nodes {
		node.FusedScore = scores[id]
		result = append(result, node)
	}

	// Sort by fused score, descending
	sort.Slice(result, func(i, j int) bool {
		if result[i].FusedScore != result[j].FusedScore {
			return result[i].FusedScore > result[j].FusedScore
		}
		// Tiebreak by node ID for deterministic ordering
		return result[i].Node.ID < result[j].Node.ID
	})

	return result
}
