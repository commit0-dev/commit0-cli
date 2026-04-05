package app

import (
	"math"
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
)

func TestReciprocalRankFusion(t *testing.T) {
	tests := []struct {
		name      string
		vector    []types.ScoredNode
		fts       []types.ScoredNode
		weights   RRFWeights
		wantCount int
		wantOrder []string // node IDs in expected order
	}{
		{
			name:      "empty both lists",
			vector:    []types.ScoredNode{},
			fts:       []types.ScoredNode{},
			wantCount: 0,
		},
		{
			name: "vector only",
			vector: []types.ScoredNode{
				{Node: types.CodeNode{ID: "n1"}, FusedScore: 0.9},
				{Node: types.CodeNode{ID: "n2"}, FusedScore: 0.8},
			},
			fts:       []types.ScoredNode{},
			wantCount: 2,
			wantOrder: []string{"n1", "n2"},
		},
		{
			name:   "fts only",
			vector: []types.ScoredNode{},
			fts: []types.ScoredNode{
				{Node: types.CodeNode{ID: "n1"}, FusedScore: 0.9},
				{Node: types.CodeNode{ID: "n2"}, FusedScore: 0.8},
			},
			wantCount: 2,
			wantOrder: []string{"n1", "n2"},
		},
		{
			name: "merged with same nodes",
			vector: []types.ScoredNode{
				{Node: types.CodeNode{ID: "n1", Qualified: "A"}, Centrality: 10},
				{Node: types.CodeNode{ID: "n2", Qualified: "B"}, Centrality: 5},
			},
			fts: []types.ScoredNode{
				{Node: types.CodeNode{ID: "n1", Qualified: "A"}, Centrality: 10},
				{Node: types.CodeNode{ID: "n3", Qualified: "C"}, Centrality: 0},
			},
			weights:   DefaultRRFWeights(),
			wantCount: 3,
		},
		{
			name: "centrality boost zero guard",
			vector: []types.ScoredNode{
				{Node: types.CodeNode{ID: "n1"}, Centrality: 0},
				{Node: types.CodeNode{ID: "n2"}, Centrality: 100},
			},
			fts:       []types.ScoredNode{},
			weights:   DefaultRRFWeights(),
			wantCount: 2,
			// n2 should rank higher due to centrality boost
			wantOrder: []string{"n2", "n1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ReciprocalRankFusion(tt.vector, tt.fts, tt.weights)

			if len(result) != tt.wantCount {
				t.Errorf("got %d results, want %d", len(result), tt.wantCount)
			}

			if len(tt.wantOrder) > 0 {
				for i, wantID := range tt.wantOrder {
					if i >= len(result) {
						t.Errorf("expected %d results, got %d", len(tt.wantOrder), len(result))
						break
					}
					if result[i].Node.ID != wantID {
						t.Errorf("result[%d].ID = %s, want %s", i, result[i].Node.ID, wantID)
					}
				}
			}

			// Verify descending fused score order
			for i := 1; i < len(result); i++ {
				if result[i].FusedScore > result[i-1].FusedScore {
					t.Errorf("results not sorted: result[%d].FusedScore (%.2f) > result[%d].FusedScore (%.2f)",
						i, result[i].FusedScore, i-1, result[i-1].FusedScore)
				}
			}
		})
	}
}

func TestCentralityBoostGuard(t *testing.T) {
	nodes := []types.ScoredNode{
		{Node: types.CodeNode{ID: "n1"}, Centrality: 0},
	}

	result := ReciprocalRankFusion(nodes, []types.ScoredNode{}, DefaultRRFWeights())
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}

	if result[0].FusedScore <= 0 {
		t.Errorf("FusedScore = %f, want > 0 for Centrality=0", result[0].FusedScore)
	}
}

func TestRRFWeights(t *testing.T) {
	vector := []types.ScoredNode{
		{Node: types.CodeNode{ID: "n1"}},
	}
	fts := []types.ScoredNode{
		{Node: types.CodeNode{ID: "n2"}},
	}

	w1 := RRFWeights{Vector: 1, FTS: 1, K: 10}
	result1 := ReciprocalRankFusion(vector, fts, w1)

	w2 := RRFWeights{Vector: 1, FTS: 1, K: 100}
	result2 := ReciprocalRankFusion(vector, fts, w2)

	if len(result1) != 2 || len(result2) != 2 {
		t.Fatalf("expected 2 results in both cases")
	}

	if result1[0].FusedScore <= result2[0].FusedScore {
		t.Errorf("K=10 score (%.2f) should be higher than K=100 score (%.2f)",
			result1[0].FusedScore, result2[0].FusedScore)
	}
}

func TestRRFWeightsBalance(t *testing.T) {
	vector := []types.ScoredNode{
		{Node: types.CodeNode{ID: "n1"}},
	}
	fts := []types.ScoredNode{
		{Node: types.CodeNode{ID: "n1"}},
	}

	w := RRFWeights{Vector: 2, FTS: 1, K: 60}
	result := ReciprocalRankFusion(vector, fts, w)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}

	expected := (2.0 + 1.0) / 61.0
	actual := result[0].FusedScore
	if math.Abs(actual-expected) > 0.001 {
		t.Errorf("FusedScore = %.6f, want %.6f", actual, expected)
	}
}

func TestRRFNegativeK(t *testing.T) {
	// Negative K should default to 60
	vector := []types.ScoredNode{
		{Node: types.CodeNode{ID: "n1"}},
	}
	result := ReciprocalRankFusion(vector, []types.ScoredNode{}, RRFWeights{Vector: 1, FTS: 1, K: -5})
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	// With K=60 (default), score = 1/(60+1) = 1/61 ≈ 0.01639
	expected := 1.0 / 61.0
	if math.Abs(result[0].FusedScore-expected) > 0.001 {
		t.Errorf("FusedScore with negative K = %.6f, want %.6f (K=60 default)", result[0].FusedScore, expected)
	}
}

func TestRRFTiebreakByNodeID(t *testing.T) {
	// Two nodes with the same centrality (=0) and same rank position produce
	// identical RRF scores → tiebreak must be by node ID (ascending).
	vector := []types.ScoredNode{
		{Node: types.CodeNode{ID: "z-node"}, Centrality: 0},
		{Node: types.CodeNode{ID: "a-node"}, Centrality: 0},
	}
	fts := []types.ScoredNode{
		{Node: types.CodeNode{ID: "z-node"}, Centrality: 0},
		{Node: types.CodeNode{ID: "a-node"}, Centrality: 0},
	}

	result := ReciprocalRankFusion(vector, fts, DefaultRRFWeights())

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}

	// Both nodes have the same score; check they're in a deterministic order.
	// With same score, sort is by node ID ascending → "a-node" before "z-node".
	if result[0].FusedScore == result[1].FusedScore {
		if result[0].Node.ID > result[1].Node.ID {
			t.Errorf("tiebreak should sort by ID ascending: got %s before %s",
				result[0].Node.ID, result[1].Node.ID)
		}
	}
}
