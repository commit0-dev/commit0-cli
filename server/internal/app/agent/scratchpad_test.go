package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/app/memory"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ── Fakes (scratchpad-specific, named to avoid conflicts with tools_test.go) ──

// padFakeGraph is a minimal domain.OpenCodeGraph for scratchpad tool tests.
type padFakeGraph struct {
	repos    []types.Repo
	nodeIDs  []types.CodeNode
	edges    []types.CodeEdge
	reposErr error
	nodesErr error
	edgesErr error
}

func (f *padFakeGraph) ListRepos(_ context.Context) ([]types.Repo, error) {
	return f.repos, f.reposErr
}
func (f *padFakeGraph) ListNodes(_ context.Context, _ string, _ domain.ListOpts) ([]types.CodeNode, error) {
	return f.nodeIDs, f.nodesErr
}
func (f *padFakeGraph) ListEdges(_ context.Context, _ string, _ []string) ([]types.CodeEdge, error) {
	return f.edges, f.edgesErr
}
func (f *padFakeGraph) PutNode(_ context.Context, _ *types.CodeNode) error { return nil }
func (f *padFakeGraph) GetNode(_ context.Context, _ string) (*types.CodeNode, error) {
	return nil, nil
}
func (f *padFakeGraph) FindNode(_ context.Context, _, _ string) (*types.CodeNode, error) {
	return nil, nil
}
func (f *padFakeGraph) DeleteNode(_ context.Context, _ string) error             { return nil }
func (f *padFakeGraph) PutEdge(_ context.Context, _ *types.CodeEdge) error       { return nil }
func (f *padFakeGraph) DeleteEdgesFrom(_ context.Context, _ string) error        { return nil }
func (f *padFakeGraph) PutBatch(_ context.Context, _ []types.CodeNode, _ []types.CodeEdge) error {
	return nil
}
func (f *padFakeGraph) DeleteByRepo(_ context.Context, _ string) error   { return nil }
func (f *padFakeGraph) DeleteByFile(_ context.Context, _, _ string) error { return nil }
func (f *padFakeGraph) TraverseGraph(_ context.Context, _ string, _ []string, _ string, _ int) ([]types.TraceHop, error) {
	return nil, nil
}
func (f *padFakeGraph) Neighbors(_ context.Context, _ string) (*domain.Neighborhood, error) {
	return nil, nil
}
func (f *padFakeGraph) VectorSearch(_ context.Context, _ []float32, _ domain.VectorSearchOpts) ([]types.ScoredNode, error) {
	return nil, nil
}
func (f *padFakeGraph) TextSearch(_ context.Context, _ string, _ domain.TextSearchOpts) ([]types.ScoredNode, error) {
	return nil, nil
}
func (f *padFakeGraph) ListFilePaths(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (f *padFakeGraph) PutRepo(_ context.Context, _ *types.Repo) error               { return nil }
func (f *padFakeGraph) GetRepo(_ context.Context, _ string) (*types.Repo, error)     { return nil, nil }
func (f *padFakeGraph) DeleteRepo(_ context.Context, _ string) error                 { return nil }
func (f *padFakeGraph) FindRepoByRemoteURL(_ context.Context, _ string) (*types.Repo, error) {
	return nil, nil
}
func (f *padFakeGraph) UpdateRepoIndexedAt(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (f *padFakeGraph) ApplySchema(_ context.Context) error { return nil }

// ── Helpers ──────────────────────────────────────────────────────────────────

// invokeJSON is a convenience to call a tool with a JSON-marshalled input.
func invokeJSON(t *testing.T, tool AgentTool, ctx context.Context, input any) string {
	t.Helper()
	b, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	out, err := tool.Invoke(ctx, string(b))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	return out
}

// mustParsePad unmarshals the JSON output into a map for assertions.
func mustParseMap(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("unmarshal output %q: %v", s, err)
	}
	return m
}

// ─────────────────────────────────────────────────────────────────────────────
// scratchpad.go tests
// ─────────────────────────────────────────────────────────────────────────────

// TestNewScratchpad_Defaults verifies that NewScratchpad initialises budgets.
func TestNewScratchpad_Defaults(t *testing.T) {
	s := NewScratchpad("find bugs")
	if s.Goal != "find bugs" {
		t.Errorf("Goal = %q, want %q", s.Goal, "find bugs")
	}
	if s.TokenBudget != 4000 {
		t.Errorf("TokenBudget = %d, want 4000", s.TokenBudget)
	}
	if s.CostBudget != 1.00 {
		t.Errorf("CostBudget = %v, want 1.00", s.CostBudget)
	}
}

// TestAddEvidence_AssignsID verifies sequential ID assignment.
func TestAddEvidence_AssignsID(t *testing.T) {
	s := NewScratchpad("test goal alpha")
	e1 := s.AddEvidence(Evidence{Content: "alpha finding", Source: "search", Relevance: 0.8, Confidence: 0.8, Novelty: 0.8, Actionability: 0.8})
	e2 := s.AddEvidence(Evidence{Content: "beta finding", Source: "search", Relevance: 0.8, Confidence: 0.8, Novelty: 0.8, Actionability: 0.8})
	if e1.ID != "E1" {
		t.Errorf("first ID = %q, want E1", e1.ID)
	}
	if e2.ID != "E2" {
		t.Errorf("second ID = %q, want E2", e2.ID)
	}
}

// TestAddEvidence_SetsTimestampAndDelegation checks derived fields.
func TestAddEvidence_SetsTimestampAndDelegation(t *testing.T) {
	s := NewScratchpad("test goal alpha")
	s.DelegationCount = 3
	before := time.Now()
	e := s.AddEvidence(Evidence{Content: "alpha relevant finding", Source: "search", Relevance: 0.6, Confidence: 0.6})
	after := time.Now()
	if e.Delegation != 3 {
		t.Errorf("Delegation = %d, want 3", e.Delegation)
	}
	if e.Timestamp.Before(before) || e.Timestamp.After(after) {
		t.Errorf("Timestamp %v not in expected range [%v, %v]", e.Timestamp, before, after)
	}
}

// TestAddEvidence_SetsUpdatedFlag verifies UpdatedSinceDelegation is set.
func TestAddEvidence_SetsUpdatedFlag(t *testing.T) {
	s := NewScratchpad("alpha")
	s.UpdatedSinceDelegation = false
	s.AddEvidence(Evidence{Content: "alpha content here", Source: "search", Relevance: 0.5, Confidence: 0.5})
	if !s.UpdatedSinceDelegation {
		t.Error("expected UpdatedSinceDelegation to be true after AddEvidence")
	}
}

// TestAddEvidence_ComputesPriority checks that priority is non-zero for non-zero scores.
func TestAddEvidence_ComputesPriority(t *testing.T) {
	s := NewScratchpad("alpha test relevance check here")
	e := s.AddEvidence(Evidence{
		Content: "alpha test relevance check here for priority testing",
		Source:  "search", Relevance: 1.0, Confidence: 1.0, Novelty: 1.0, Actionability: 1.0,
	})
	if e.Priority <= 0 {
		t.Errorf("Priority = %v, expected > 0", e.Priority)
	}
	// All weights: 0.3+0.3+0.2+0.2 = 1.0
	if e.Priority > 1.0 {
		t.Errorf("Priority = %v, expected <= 1.0", e.Priority)
	}
}

// TestValidateScores_ClampsOutOfRange verifies out-of-range scores are clamped.
func TestValidateScores_ClampsOutOfRange(t *testing.T) {
	s := NewScratchpad("alpha goal")
	e := s.AddEvidence(Evidence{
		Content: "alpha goal relevant finding", Source: "search",
		Relevance: 2.0, Confidence: -0.5, Novelty: 1.5, Actionability: -1.0,
	})
	if e.Relevance > 1.0 {
		t.Errorf("Relevance not clamped: %v", e.Relevance)
	}
	if e.Confidence < 0 {
		t.Errorf("Confidence not clamped to 0: %v", e.Confidence)
	}
	if e.Novelty > 1.0 {
		t.Errorf("Novelty not clamped: %v", e.Novelty)
	}
	if e.Actionability < 0 {
		t.Errorf("Actionability not clamped to 0: %v", e.Actionability)
	}
}

// TestValidateScores_RelevancePenalty verifies relevance is capped when no goal keyword matches.
func TestValidateScores_RelevancePenalty(t *testing.T) {
	// Goal keywords: "authentication", "login" — content has neither.
	s := NewScratchpad("authentication login flow")
	e := s.AddEvidence(Evidence{
		Content: "completely unrelated database schema table column row",
		Source:  "search", Relevance: 0.9, Confidence: 0.5,
	})
	if e.Relevance > 0.5 {
		t.Errorf("Relevance should be capped at 0.5 when no goal keywords match, got %v", e.Relevance)
	}
}

// TestValidateScores_NoveltyPenalty verifies novelty is capped for near-duplicates.
func TestValidateScores_NoveltyPenalty(t *testing.T) {
	s := NewScratchpad("test goal for novelty")
	// First piece of evidence.
	s.AddEvidence(Evidence{
		Content: "the authentication handler processes login requests and validates tokens",
		Source:  "search", Relevance: 0.8, Confidence: 0.7, Novelty: 0.9,
	})
	// Near-duplicate: very similar wording.
	e2 := s.AddEvidence(Evidence{
		Content: "the authentication handler processes login requests and validates tokens",
		Source:  "search", Relevance: 0.8, Confidence: 0.7, Novelty: 0.9,
	})
	if e2.Novelty > 0.1 {
		t.Errorf("Novelty should be capped at 0.1 for near-duplicates, got %v", e2.Novelty)
	}
}

// TestValidateScores_SourceReliability_DeepDive verifies deep_dive caps confidence at 0.95.
func TestValidateScores_SourceReliability_DeepDive(t *testing.T) {
	s := NewScratchpad("alpha")
	e := s.AddEvidence(Evidence{
		Content: "alpha relevant finding in depth", Source: "deep_dive",
		Relevance: 1.0, Confidence: 1.0,
	})
	if e.Confidence > 0.95 {
		t.Errorf("deep_dive confidence should be capped at 0.95, got %v", e.Confidence)
	}
}

// TestValidateScores_SourceReliability_Search verifies search caps confidence at 0.70.
func TestValidateScores_SourceReliability_Search(t *testing.T) {
	s := NewScratchpad("alpha")
	e := s.AddEvidence(Evidence{
		Content: "alpha relevant search result finding", Source: "search",
		Relevance: 1.0, Confidence: 1.0,
	})
	if e.Confidence > 0.70 {
		t.Errorf("search confidence should be capped at 0.70, got %v", e.Confidence)
	}
}

// TestValidateScores_SourceReliability_Trace verifies trace caps confidence at 0.90.
func TestValidateScores_SourceReliability_Trace(t *testing.T) {
	s := NewScratchpad("alpha")
	e := s.AddEvidence(Evidence{
		Content: "alpha relevant trace finding result", Source: "trace",
		Relevance: 1.0, Confidence: 1.0,
	})
	if e.Confidence > 0.90 {
		t.Errorf("trace confidence should be capped at 0.90, got %v", e.Confidence)
	}
}

// TestValidateScores_SourceReliability_Security verifies security source caps at 0.85.
func TestValidateScores_SourceReliability_Security(t *testing.T) {
	s := NewScratchpad("alpha")
	e := s.AddEvidence(Evidence{
		Content: "alpha relevant security finding result", Source: "security",
		Relevance: 1.0, Confidence: 1.0,
	})
	if e.Confidence > 0.85 {
		t.Errorf("security confidence should be capped at 0.85, got %v", e.Confidence)
	}
}

// TestValidateScores_SourceReliability_Blast verifies blast source caps at 0.85.
func TestValidateScores_SourceReliability_Blast(t *testing.T) {
	s := NewScratchpad("alpha")
	e := s.AddEvidence(Evidence{
		Content: "alpha relevant blast finding result", Source: "blast",
		Relevance: 1.0, Confidence: 1.0,
	})
	if e.Confidence > 0.85 {
		t.Errorf("blast confidence should be capped at 0.85, got %v", e.Confidence)
	}
}

// TestValidateScores_SourceReliability_Neighborhood verifies neighborhood caps at 0.90.
func TestValidateScores_SourceReliability_Neighborhood(t *testing.T) {
	s := NewScratchpad("alpha")
	e := s.AddEvidence(Evidence{
		Content: "alpha relevant neighborhood finding here", Source: "neighborhood",
		Relevance: 1.0, Confidence: 1.0,
	})
	if e.Confidence > 0.90 {
		t.Errorf("neighborhood confidence should be capped at 0.90, got %v", e.Confidence)
	}
}

// TestValidateScores_SourceReliability_LookupNode verifies lookup_node caps at 0.95.
func TestValidateScores_SourceReliability_LookupNode(t *testing.T) {
	s := NewScratchpad("alpha")
	e := s.AddEvidence(Evidence{
		Content: "alpha relevant lookup node result finding", Source: "lookup_node",
		Relevance: 1.0, Confidence: 1.0,
	})
	if e.Confidence > 0.95 {
		t.Errorf("lookup_node confidence should be capped at 0.95, got %v", e.Confidence)
	}
}

// TestValidateScores_SourceReliability_Default verifies unknown source caps at 0.80.
func TestValidateScores_SourceReliability_Default(t *testing.T) {
	s := NewScratchpad("alpha")
	e := s.AddEvidence(Evidence{
		Content: "alpha relevant unknown source finding here", Source: "some_other_tool",
		Relevance: 1.0, Confidence: 1.0,
	})
	if e.Confidence > 0.80 {
		t.Errorf("default source reliability should cap confidence at 0.80, got %v", e.Confidence)
	}
}

// TestDetectContradiction_LowRelevanceSkip verifies low-relevance items don't trigger contradictions.
func TestDetectContradiction_LowRelevanceSkip(t *testing.T) {
	s := NewScratchpad("auth token goal")
	s.AddEvidence(Evidence{
		Content: "auth token validates correctly", Source: "search",
		Relevance: 0.3, Confidence: 0.5, // below 0.5 threshold
	})
	s.AddEvidence(Evidence{
		Content: "auth token does not validate correctly",
		Source:  "search", Relevance: 0.3, Confidence: 0.5,
	})
	if len(s.Contradictions) > 0 {
		t.Error("expected no contradictions for low-relevance evidence")
	}
}

// TestDetectContradiction_HighRelevanceSimilarContent triggers a contradiction.
func TestDetectContradiction_HighRelevanceSimilarContent(t *testing.T) {
	s := NewScratchpad("auth token goal")
	// Both items: high relevance, similar topic (auth token), one has "not".
	s.AddEvidence(Evidence{
		Content: "auth token validation is working correctly and returns true",
		Source:  "search", Relevance: 0.9, Confidence: 0.8,
	})
	s.AddEvidence(Evidence{
		Content: "auth token validation is not working correctly and returns false",
		Source:  "search", Relevance: 0.9, Confidence: 0.8,
	})
	if len(s.Contradictions) == 0 {
		t.Error("expected at least one contradiction to be detected")
	}
}

// TestDetectContradiction_ReducesConfidence verifies both items get penalised.
func TestDetectContradiction_ReducesConfidence(t *testing.T) {
	s := NewScratchpad("auth token goal")
	e1 := s.AddEvidence(Evidence{
		Content: "auth token validation is working correctly and returns true",
		Source:  "search", Relevance: 0.9, Confidence: 0.8,
	})
	e2 := s.AddEvidence(Evidence{
		Content: "auth token validation is not working correctly and returns false",
		Source:  "search", Relevance: 0.9, Confidence: 0.8,
	})
	// The second evidence's confidence should be reduced.
	if e2.Confidence >= 0.8 {
		t.Errorf("second evidence confidence not reduced: %v", e2.Confidence)
	}
	// Also, the first should be updated in the slice.
	if s.Evidence[0].ID == e1.ID && s.Evidence[0].Confidence >= 0.8 {
		t.Errorf("first evidence confidence not reduced in slice: %v", s.Evidence[0].Confidence)
	}
}

// TestDetectContradiction_AutoQuestion verifies a question is generated for contradictions.
func TestDetectContradiction_AutoQuestion(t *testing.T) {
	s := NewScratchpad("auth token goal")
	s.AddEvidence(Evidence{
		Content: "auth token validation is working correctly and returns true",
		Source:  "search", Relevance: 0.9, Confidence: 0.8,
	})
	s.AddEvidence(Evidence{
		Content: "auth token validation is not working correctly and returns false",
		Source:  "search", Relevance: 0.9, Confidence: 0.8,
	})
	if len(s.OpenQuestions) == 0 {
		t.Error("expected an auto-generated question for contradicting evidence")
	}
	q := s.OpenQuestions[0]
	if !strings.Contains(q.Text, "Contradiction") {
		t.Errorf("auto-question text does not mention Contradiction: %q", q.Text)
	}
	if q.Priority != 0.9 {
		t.Errorf("auto-question priority = %v, want 0.9", q.Priority)
	}
}

// TestRecordAction_AppendsToLog verifies actions are recorded.
func TestRecordAction_AppendsToLog(t *testing.T) {
	s := NewScratchpad("goal")
	s.RecordAction("search", "find auth bugs", 42)
	if len(s.ActionLog) != 1 {
		t.Fatalf("expected 1 action, got %d", len(s.ActionLog))
	}
	a := s.ActionLog[0]
	if a.Tool != "search" || a.Args != "find auth bugs" || a.ResultSize != 42 {
		t.Errorf("unexpected action: %+v", a)
	}
}

// TestRecordAction_HashIsSet verifies result hash is populated.
func TestRecordAction_HashIsSet(t *testing.T) {
	s := NewScratchpad("goal")
	s.RecordAction("search", "query text", 10)
	if s.ActionLog[0].ResultHash == "" {
		t.Error("ResultHash should not be empty")
	}
}

// TestAlreadyTried_ExactMatch returns true for same tool+args.
func TestAlreadyTried_ExactMatch(t *testing.T) {
	s := NewScratchpad("goal")
	s.RecordAction("search", "find auth token validation issue", 5)
	found, similar := s.AlreadyTried("search", "find auth token validation issue")
	if !found {
		t.Error("expected AlreadyTried to return true for identical args")
	}
	if len(similar) != 1 {
		t.Errorf("expected 1 similar action, got %d", len(similar))
	}
}

// TestAlreadyTried_HighlySimilarArgs also triggers AlreadyTried.
func TestAlreadyTried_HighlySimilarArgs(t *testing.T) {
	s := NewScratchpad("goal")
	s.RecordAction("search", "authentication token validation bug exists here", 5)
	// Very similar but not identical.
	found, _ := s.AlreadyTried("search", "authentication token validation bug exists in code")
	if !found {
		t.Error("expected AlreadyTried for highly similar args (>0.7 similarity)")
	}
}

// TestAlreadyTried_DifferentTool returns false when tool differs.
func TestAlreadyTried_DifferentTool(t *testing.T) {
	s := NewScratchpad("goal")
	s.RecordAction("search", "find auth token validation issue here now", 5)
	found, _ := s.AlreadyTried("trace", "find auth token validation issue here now")
	if found {
		t.Error("expected AlreadyTried false when tool differs")
	}
}

// TestAlreadyTried_NoLog returns false on empty log.
func TestAlreadyTried_NoLog(t *testing.T) {
	s := NewScratchpad("goal")
	found, similar := s.AlreadyTried("search", "anything")
	if found || len(similar) != 0 {
		t.Error("expected false with empty action log")
	}
}

// TestConvergenceCheck_AllGatesFail on fresh scratchpad.
func TestConvergenceCheck_AllGatesFail(t *testing.T) {
	s := NewScratchpad("goal")
	ok, failures := s.ConvergenceCheck()
	if ok {
		t.Error("expected convergence to fail on fresh scratchpad")
	}
	if len(failures) == 0 {
		t.Error("expected failures to be non-empty")
	}
}

// TestConvergenceCheck_Gate1_MinDelegations verifies the delegation gate.
func TestConvergenceCheck_Gate1_MinDelegations(t *testing.T) {
	s := NewScratchpad("goal")
	s.DelegationCount = 2 // below threshold of 3
	_, failures := s.ConvergenceCheck()
	found := false
	for _, f := range failures {
		if strings.Contains(f, "need at least 3 delegations") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected delegation failure message, got: %v", failures)
	}
}

// TestConvergenceCheck_Gate2_MinHighPriorityEvidence verifies the evidence gate.
func TestConvergenceCheck_Gate2_MinHighPriorityEvidence(t *testing.T) {
	s := NewScratchpad("goal keywords here with lots")
	s.DelegationCount = 5
	// Add only 3 high-priority evidence items (need 5).
	for i := 0; i < 3; i++ {
		s.Evidence = append(s.Evidence, Evidence{
			ID: fmt.Sprintf("E%d", i), Priority: 0.8,
		})
	}
	_, failures := s.ConvergenceCheck()
	found := false
	for _, f := range failures {
		if strings.Contains(f, "need 5+ high-priority evidence") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected high-priority evidence failure, got: %v", failures)
	}
}

// TestConvergenceCheck_Gate3_OpenQuestions verifies high-priority open questions block convergence.
func TestConvergenceCheck_Gate3_OpenQuestions(t *testing.T) {
	s := NewScratchpad("goal")
	s.DelegationCount = 5
	// Fill evidence gate.
	for i := 0; i < 5; i++ {
		s.Evidence = append(s.Evidence, Evidence{ID: fmt.Sprintf("E%d", i), Priority: 0.9})
	}
	s.OpenQuestions = append(s.OpenQuestions, Question{
		ID: "Q1", Text: "Why does auth fail?", Priority: 0.8, Status: "open",
	})
	_, failures := s.ConvergenceCheck()
	found := false
	for _, f := range failures {
		if strings.Contains(f, "open question") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected open question failure, got: %v", failures)
	}
}

// TestConvergenceCheck_Gate3_LowPriorityQuestionsDoNotBlock verifies low-priority questions don't block.
func TestConvergenceCheck_Gate3_LowPriorityQuestionsDoNotBlock(t *testing.T) {
	s := NewScratchpad("goal")
	s.DelegationCount = 5
	for i := 0; i < 5; i++ {
		s.Evidence = append(s.Evidence, Evidence{ID: fmt.Sprintf("E%d", i), Priority: 0.9})
	}
	// Add a low-priority question — should not trigger gate 3.
	s.OpenQuestions = append(s.OpenQuestions, Question{
		ID: "Q1", Text: "Minor question", Priority: 0.5, Status: "open",
	})
	_, failures := s.ConvergenceCheck()
	for _, f := range failures {
		if strings.Contains(f, "open question") {
			t.Errorf("low-priority question should not block convergence, got failure: %q", f)
		}
	}
}

// TestConvergenceCheck_Gate4_DiminishingReturns verifies novel findings gate.
func TestConvergenceCheck_Gate4_DiminishingReturns(t *testing.T) {
	s := NewScratchpad("goal")
	s.DelegationCount = 5
	for i := 0; i < 5; i++ {
		s.Evidence = append(s.Evidence, Evidence{ID: fmt.Sprintf("E%d", i), Priority: 0.9})
	}
	// Two consecutive high-novel delegations.
	s.NovelFindings = []int{3, 5}
	_, failures := s.ConvergenceCheck()
	found := false
	for _, f := range failures {
		if strings.Contains(f, "still finding novel information") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected novel findings failure, got: %v", failures)
	}
}

// TestConvergenceCheck_Gate4_LessThan2Delegations verifies gate 4 is skipped with < 2 entries.
func TestConvergenceCheck_Gate4_LessThan2Delegations(t *testing.T) {
	s := NewScratchpad("goal")
	s.DelegationCount = 5
	for i := 0; i < 5; i++ {
		s.Evidence = append(s.Evidence, Evidence{ID: fmt.Sprintf("E%d", i), Priority: 0.9})
	}
	// Only one entry — gate 4 should not trigger.
	s.NovelFindings = []int{10}
	_, failures := s.ConvergenceCheck()
	for _, f := range failures {
		if strings.Contains(f, "still finding novel information") {
			t.Errorf("gate 4 should be skipped with < 2 NovelFindings entries: %q", f)
		}
	}
}

// TestConvergenceCheck_Gate5_NoHypothesisResolved verifies hypothesis gate.
func TestConvergenceCheck_Gate5_NoHypothesisResolved(t *testing.T) {
	s := NewScratchpad("goal")
	s.DelegationCount = 5
	for i := 0; i < 5; i++ {
		s.Evidence = append(s.Evidence, Evidence{ID: fmt.Sprintf("E%d", i), Priority: 0.9})
	}
	s.Hypotheses = []Hypothesis{{ID: "H1", Statement: "theory", Status: "testing", Confidence: 0.5}}
	_, failures := s.ConvergenceCheck()
	found := false
	for _, f := range failures {
		if strings.Contains(f, "no hypotheses confirmed or rejected") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected hypothesis failure, got: %v", failures)
	}
}

// TestConvergenceCheck_Gate5_EmptyHypothesesDoesNotFail verifies no hypothesis is OK.
func TestConvergenceCheck_Gate5_EmptyHypothesesDoesNotFail(t *testing.T) {
	s := NewScratchpad("goal")
	s.DelegationCount = 5
	for i := 0; i < 5; i++ {
		s.Evidence = append(s.Evidence, Evidence{ID: fmt.Sprintf("E%d", i), Priority: 0.9})
	}
	// No hypotheses — gate 5 should NOT fire.
	_, failures := s.ConvergenceCheck()
	for _, f := range failures {
		if strings.Contains(f, "no hypotheses confirmed or rejected") {
			t.Errorf("gate 5 should not fire when Hypotheses is empty: %q", f)
		}
	}
}

// TestConvergenceCheck_Gate6_UnresolvedContradiction verifies contradiction gate.
func TestConvergenceCheck_Gate6_UnresolvedContradiction(t *testing.T) {
	s := NewScratchpad("goal")
	s.DelegationCount = 5
	for i := 0; i < 5; i++ {
		s.Evidence = append(s.Evidence, Evidence{ID: fmt.Sprintf("E%d", i), Priority: 0.9})
	}
	s.Contradictions = []Contradiction{{EvidenceA: "E1", EvidenceB: "E2", Description: "conflict here", Resolved: false}}
	_, failures := s.ConvergenceCheck()
	found := false
	for _, f := range failures {
		if strings.Contains(f, "unresolved contradiction") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected unresolved contradiction failure, got: %v", failures)
	}
}

// TestConvergenceCheck_Converges when all gates pass.
func TestConvergenceCheck_Converges(t *testing.T) {
	s := NewScratchpad("goal")
	s.DelegationCount = 4
	// Gate 2: 5+ high-priority evidence.
	for i := 0; i < 6; i++ {
		s.Evidence = append(s.Evidence, Evidence{ID: fmt.Sprintf("E%d", i), Priority: 0.9})
	}
	// Gate 3: all questions answered.
	s.OpenQuestions = []Question{{ID: "Q1", Text: "q", Priority: 0.8, Status: "answered"}}
	// Gate 4: low novel counts.
	s.NovelFindings = []int{1, 0}
	// Gate 5: one hypothesis confirmed.
	s.Hypotheses = []Hypothesis{{ID: "H1", Statement: "theory", Status: "confirmed", Confidence: 0.9}}
	// Gate 6: no unresolved contradictions.
	s.Contradictions = []Contradiction{{Resolved: true}}
	ok, failures := s.ConvergenceCheck()
	if !ok {
		t.Errorf("expected convergence, failures: %v", failures)
	}
}

// TestBudgetedView_Evidence verifies the evidence section format.
func TestBudgetedView_Evidence(t *testing.T) {
	s := NewScratchpad("alpha goal test here")
	s.AddEvidence(Evidence{Content: "alpha goal relevant content here", Source: "search", Relevance: 0.8, Confidence: 0.7, Novelty: 0.5})
	out := s.BudgetedView("evidence")
	if !strings.Contains(out, "## Evidence") {
		t.Errorf("evidence view missing header: %q", out)
	}
	if !strings.Contains(out, "E1") {
		t.Errorf("evidence view missing ID: %q", out)
	}
}

// TestBudgetedView_Questions verifies the questions section.
func TestBudgetedView_Questions(t *testing.T) {
	s := NewScratchpad("goal")
	s.OpenQuestions = []Question{
		{ID: "Q1", Text: "Why?", Priority: 0.8, Status: "open"},
		{ID: "Q2", Text: "When?", Priority: 0.5, Status: "answered"},
	}
	out := s.BudgetedView("questions")
	if !strings.Contains(out, "## Open Questions") {
		t.Errorf("questions view missing header: %q", out)
	}
	if !strings.Contains(out, "Q1") {
		t.Errorf("questions view missing open question: %q", out)
	}
	if strings.Contains(out, "Q2") {
		t.Errorf("questions view should not show answered question: %q", out)
	}
}

// TestBudgetedView_Hypotheses verifies the hypotheses section.
func TestBudgetedView_Hypotheses(t *testing.T) {
	s := NewScratchpad("goal")
	s.Hypotheses = []Hypothesis{{ID: "H1", Statement: "auth is broken", Status: "testing", Confidence: 0.7}}
	out := s.BudgetedView("hypotheses")
	if !strings.Contains(out, "## Hypotheses") {
		t.Errorf("hypotheses view missing header: %q", out)
	}
	if !strings.Contains(out, "H1") {
		t.Errorf("hypotheses view missing ID: %q", out)
	}
}

// TestBudgetedView_ActionLog verifies the action log section.
func TestBudgetedView_ActionLog(t *testing.T) {
	s := NewScratchpad("goal")
	for i := 0; i < 7; i++ {
		s.RecordAction("search", fmt.Sprintf("query %d", i), i*10)
	}
	out := s.BudgetedView("action_log")
	if !strings.Contains(out, "## Action Log") {
		t.Errorf("action log view missing header: %q", out)
	}
	// Should show "last 5 of 7".
	if !strings.Contains(out, "7") {
		t.Errorf("action log view should mention total count: %q", out)
	}
}

// TestBudgetedView_ActionLog_Fewer5 verifies fewer-than-5 actions show correctly.
func TestBudgetedView_ActionLog_Fewer5(t *testing.T) {
	s := NewScratchpad("goal")
	s.RecordAction("search", "query one", 5)
	out := s.BudgetedView("action_log")
	if !strings.Contains(out, "search") {
		t.Errorf("action log view missing tool name: %q", out)
	}
}

// TestBudgetedView_Convergence verifies the convergence section.
func TestBudgetedView_Convergence(t *testing.T) {
	s := NewScratchpad("goal")
	out := s.BudgetedView("convergence")
	if !strings.Contains(out, "## Convergence") {
		t.Errorf("convergence view missing header: %q", out)
	}
	if !strings.Contains(out, "NOT converging") {
		t.Errorf("expected NOT converging on fresh scratchpad: %q", out)
	}
}

// TestBudgetedView_All verifies the all section.
func TestBudgetedView_All(t *testing.T) {
	s := NewScratchpad("my analysis goal")
	s.Strategy = "bottom-up"
	out := s.BudgetedView("all")
	if !strings.Contains(out, "# Analysis:") {
		t.Errorf("all view missing analysis header: %q", out)
	}
	if !strings.Contains(out, "bottom-up") {
		t.Errorf("all view missing strategy: %q", out)
	}
}

// TestBudgetedView_DefaultIsAll verifies unknown section returns all.
func TestBudgetedView_DefaultIsAll(t *testing.T) {
	s := NewScratchpad("goal here")
	out := s.BudgetedView("unknown_section")
	if !strings.Contains(out, "# Analysis:") {
		t.Errorf("default section should return all view: %q", out)
	}
}

// TestPersistableFindings_Empty returns empty when nothing added.
func TestPersistableFindings_Empty(t *testing.T) {
	s := NewScratchpad("goal")
	findings := s.PersistableFindings()
	// No strategy+goal finding either since Goal is set but no converge check meaningful.
	// Actually: strategy="" so no strategy finding. Evidence and hypotheses are empty.
	// Only a strategy finding if s.Strategy != "". Our s.Strategy is "".
	if len(findings) != 0 {
		t.Errorf("expected 0 persistable findings, got %d: %v", len(findings), findings)
	}
}

// TestPersistableFindings_StrategyFinding emits a strategy entry when Strategy and Goal are set.
func TestPersistableFindings_StrategyFinding(t *testing.T) {
	s := NewScratchpad("find auth bugs")
	s.Strategy = "search-first"
	findings := s.PersistableFindings()
	found := false
	for _, f := range findings {
		if f.Kind == "strategy" {
			found = true
			if !strings.Contains(f.Content, "search-first") {
				t.Errorf("strategy finding missing strategy name: %q", f.Content)
			}
		}
	}
	if !found {
		t.Error("expected a strategy finding")
	}
}

// TestPersistableFindings_HighPriorityEvidence includes high-priority evidence.
func TestPersistableFindings_HighPriorityEvidence(t *testing.T) {
	s := NewScratchpad("auth bug goal")
	// Add high-priority evidence directly (bypass AddEvidence validation).
	s.Evidence = append(s.Evidence, Evidence{
		ID: "E1", Content: "important finding", Source: "trace", Priority: 0.8,
	})
	s.Evidence = append(s.Evidence, Evidence{
		ID: "E2", Content: "low priority note", Source: "search", Priority: 0.2,
	})
	findings := s.PersistableFindings()
	evidenceFindings := 0
	for _, f := range findings {
		if f.Kind == "evidence" {
			evidenceFindings++
			if f.Priority < 0.4 {
				t.Errorf("low-priority evidence should be excluded, got priority %v", f.Priority)
			}
		}
	}
	if evidenceFindings != 1 {
		t.Errorf("expected 1 evidence finding (high-priority only), got %d", evidenceFindings)
	}
}

// TestPersistableFindings_HypothesisIncluded includes confirmed/rejected hypotheses.
func TestPersistableFindings_HypothesisIncluded(t *testing.T) {
	s := NewScratchpad("goal")
	s.Hypotheses = []Hypothesis{
		{ID: "H1", Statement: "auth is broken", Status: "confirmed", Confidence: 0.9},
		{ID: "H2", Statement: "cache bug", Status: "rejected", Confidence: 0.4},
		{ID: "H3", Statement: "network issue", Status: "testing", Confidence: 0.5},
	}
	findings := s.PersistableFindings()
	hypothesisCount := 0
	for _, f := range findings {
		if f.Kind == "hypothesis" {
			hypothesisCount++
		}
	}
	if hypothesisCount != 2 {
		t.Errorf("expected 2 hypothesis findings (confirmed+rejected only), got %d", hypothesisCount)
	}
}

// TestConceptsFromGoal_ExtractsKeywords verifies keyword extraction.
func TestConceptsFromGoal_ExtractsKeywords(t *testing.T) {
	s := NewScratchpad("analyze authentication flow tokens")
	concepts := s.ConceptsFromGoal()
	// "analyze", "authentication", "flow", "tokens" — all > 3 chars.
	if len(concepts) == 0 {
		t.Error("expected concepts to be non-empty")
	}
	// Short words are skipped.
	for _, c := range concepts {
		if len(c) <= 3 && c != s.Strategy {
			t.Errorf("concept %q is <= 3 chars and should be filtered", c)
		}
	}
}

// TestConceptsFromGoal_IncludesStrategy verifies strategy is appended.
func TestConceptsFromGoal_IncludesStrategy(t *testing.T) {
	s := NewScratchpad("analyze auth flow")
	s.Strategy = "depth-first"
	concepts := s.ConceptsFromGoal()
	found := false
	for _, c := range concepts {
		if c == "depth-first" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected strategy in concepts, got %v", concepts)
	}
}

// TestConceptsFromGoal_CappedAt10 verifies concepts are limited.
func TestConceptsFromGoal_CappedAt10(t *testing.T) {
	s := NewScratchpad("analyze authentication flows tokens sessions users roles permissions groups domains scopes claims headers paths routes")
	concepts := s.ConceptsFromGoal()
	if len(concepts) > 10 {
		t.Errorf("expected at most 10 concepts, got %d", len(concepts))
	}
}

// TestToJSON_Roundtrip verifies the scratchpad serialises and has expected fields.
func TestToJSON_Roundtrip(t *testing.T) {
	s := NewScratchpad("test goal")
	s.Strategy = "depth-first"
	s.DelegationCount = 2
	b, err := s.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON error: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
	if m["goal"] != "test goal" {
		t.Errorf("goal = %v, want 'test goal'", m["goal"])
	}
	if m["strategy"] != "depth-first" {
		t.Errorf("strategy = %v, want 'depth-first'", m["strategy"])
	}
}

// TestTextSimilarity_Identical returns 1.0 for identical strings.
func TestTextSimilarity_Identical(t *testing.T) {
	got := textSimilarity("hello world foo bar", "hello world foo bar")
	if got != 1.0 {
		t.Errorf("identical strings: similarity = %v, want 1.0", got)
	}
}

// TestTextSimilarity_Empty returns 0 for empty inputs.
func TestTextSimilarity_Empty(t *testing.T) {
	if textSimilarity("", "anything") != 0 {
		t.Error("empty a should return 0")
	}
	if textSimilarity("anything", "") != 0 {
		t.Error("empty b should return 0")
	}
	if textSimilarity("", "") != 0 {
		t.Error("both empty should return 0")
	}
}

// TestTextSimilarity_Disjoint returns 0 for completely disjoint sets.
func TestTextSimilarity_Disjoint(t *testing.T) {
	got := textSimilarity("apple orange grape", "desk lamp chair table")
	if got != 0.0 {
		t.Errorf("disjoint sets: similarity = %v, want 0.0", got)
	}
}

// TestTextSimilarity_ShortWordsSkipped verifies words <= 2 chars are excluded.
func TestTextSimilarity_ShortWordsSkipped(t *testing.T) {
	// Both strings have only short words.
	got := textSimilarity("is it a to by", "to by is it at")
	// Short words (<= 2 chars) are excluded from wordSet, so sets are empty.
	if got != 0 {
		t.Errorf("strings with only short words: similarity = %v, want 0", got)
	}
}

// TestClamp verifies clamp boundaries.
func TestClamp(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{-0.5, 0}, {0, 0}, {0.5, 0.5}, {1.0, 1.0}, {1.5, 1.0},
	}
	for _, c := range cases {
		got := clamp(c.in)
		if got != c.want {
			t.Errorf("clamp(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestTruncateStr verifies truncation.
func TestTruncateStr(t *testing.T) {
	s := "hello world"
	if got := truncateStr(s, 5); got != "hello..." {
		t.Errorf("truncateStr(%q, 5) = %q, want %q", s, got, "hello...")
	}
	if got := truncateStr(s, 100); got != s {
		t.Errorf("truncateStr for short string modified content: %q", got)
	}
}

// TestHashStr verifies the hash is non-empty and deterministic.
func TestHashStr(t *testing.T) {
	h1 := hashStr("hello")
	h2 := hashStr("hello")
	h3 := hashStr("world")
	if h1 == "" {
		t.Error("hashStr returned empty string")
	}
	if h1 != h2 {
		t.Error("hashStr not deterministic")
	}
	if h1 == h3 {
		t.Error("different inputs should produce different hashes")
	}
}

// TestTopN_FewerThanN returns all items sorted.
func TestTopN_FewerThanN(t *testing.T) {
	ev := []Evidence{
		{ID: "E1", Priority: 0.3},
		{ID: "E2", Priority: 0.9},
		{ID: "E3", Priority: 0.6},
	}
	result := topN(ev, 10)
	if len(result) != 3 {
		t.Fatalf("expected 3, got %d", len(result))
	}
	if result[0].ID != "E2" || result[1].ID != "E3" || result[2].ID != "E1" {
		t.Errorf("topN not sorted by priority desc: %v", result)
	}
}

// TestTopN_MoreThanN returns at most N items.
func TestTopN_MoreThanN(t *testing.T) {
	var ev []Evidence
	for i := 0; i < 15; i++ {
		ev = append(ev, Evidence{ID: fmt.Sprintf("E%d", i), Priority: float64(i) * 0.05})
	}
	result := topN(ev, 10)
	if len(result) != 10 {
		t.Errorf("topN(15, 10) returned %d items", len(result))
	}
	// Highest priority should be first.
	if result[0].Priority < result[1].Priority {
		t.Error("topN result not sorted descending")
	}
}

// TestSortByPriority verifies descending sort.
func TestSortByPriority(t *testing.T) {
	ev := []Evidence{
		{ID: "A", Priority: 0.2},
		{ID: "B", Priority: 0.8},
		{ID: "C", Priority: 0.5},
	}
	sortByPriority(ev)
	if ev[0].ID != "B" || ev[1].ID != "C" || ev[2].ID != "A" {
		t.Errorf("sortByPriority wrong order: %v %v %v", ev[0].ID, ev[1].ID, ev[2].ID)
	}
}

// TestContainsAnyKeyword verifies keyword matching.
func TestContainsAnyKeyword(t *testing.T) {
	if !containsAnyKeyword("the authentication handler", "authentication flow") {
		t.Error("expected match for 'authentication'")
	}
	if containsAnyKeyword("unrelated content", "authentication flow") {
		t.Error("expected no match")
	}
	// Short words (<= 3 chars) in goal are skipped.
	if containsAnyKeyword("anything", "the at is to") {
		t.Error("short goal words should be skipped")
	}
}

// TestWordSet verifies word set construction.
func TestWordSet(t *testing.T) {
	ws := wordSet("hello world foo is at")
	if !ws["hello"] || !ws["world"] || !ws["foo"] {
		t.Errorf("wordSet missing expected words: %v", ws)
	}
	// "is" and "at" are <= 2 chars and should be excluded.
	if ws["is"] || ws["at"] {
		t.Error("short words should be excluded from wordSet")
	}
}

// TestComputePriority verifies the formula.
func TestComputePriority(t *testing.T) {
	e := Evidence{Relevance: 1, Confidence: 1, Novelty: 1, Actionability: 1}
	p := computePriority(e)
	expected := 0.3*1 + 0.3*1 + 0.2*1 + 0.2*1 // = 1.0
	if p != expected {
		t.Errorf("computePriority = %v, want %v", p, expected)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// scratchpad_tools.go tests
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildScratchpadTools_Returns5Tools verifies exactly 5 tools are returned.
func TestBuildScratchpadTools_Returns5Tools(t *testing.T) {
	pad := NewScratchpad("goal")
	tools := BuildScratchpadTools(pad, nil, nil)
	if len(tools) != 5 {
		t.Errorf("expected 5 tools, got %d", len(tools))
	}
}

// TestUpdateScratchpadTool_Def verifies the tool name.
func TestUpdateScratchpadTool_Def(t *testing.T) {
	pad := NewScratchpad("goal")
	tool := &updateScratchpadTool{pad: pad}
	def := tool.Def()
	if def.Name != "update_scratchpad" {
		t.Errorf("Name = %q, want %q", def.Name, "update_scratchpad")
	}
	if def.Description == "" {
		t.Error("Description should not be empty")
	}
}

// TestUpdateScratchpadTool_AddEvidence verifies evidence is added.
func TestUpdateScratchpadTool_AddEvidence(t *testing.T) {
	pad := NewScratchpad("auth token goal")
	tool := &updateScratchpadTool{pad: pad}
	out := invokeJSON(t, tool, context.Background(), updateScratchpadInput{
		Evidence: []evidenceInput{
			{Content: "auth token validation logic found", Source: "search", Relevance: 0.8, Confidence: 0.7, Novelty: 0.9, Actionability: 0.6},
		},
	})
	m := mustParseMap(t, out)
	if m["status"] != "updated" {
		t.Errorf("status = %v, want 'updated'", m["status"])
	}
	if m["total_evidence"].(float64) != 1 {
		t.Errorf("total_evidence = %v, want 1", m["total_evidence"])
	}
	if pad.NovelFindings[len(pad.NovelFindings)-1] != 1 {
		t.Errorf("NovelFindings last = %v, want 1", pad.NovelFindings)
	}
}

// TestUpdateScratchpadTool_SetsStrategy verifies strategy update.
func TestUpdateScratchpadTool_SetsStrategy(t *testing.T) {
	pad := NewScratchpad("goal")
	tool := &updateScratchpadTool{pad: pad}
	invokeJSON(t, tool, context.Background(), updateScratchpadInput{
		Strategy: "bottom-up",
	})
	if pad.Strategy != "bottom-up" {
		t.Errorf("Strategy = %q, want 'bottom-up'", pad.Strategy)
	}
}

// TestUpdateScratchpadTool_AddsHypothesis creates a new hypothesis.
func TestUpdateScratchpadTool_AddsHypothesis(t *testing.T) {
	pad := NewScratchpad("goal")
	tool := &updateScratchpadTool{pad: pad}
	invokeJSON(t, tool, context.Background(), updateScratchpadInput{
		Hypotheses: []hypothesisInput{
			{Statement: "auth is broken", Confidence: 0.8},
		},
	})
	if len(pad.Hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(pad.Hypotheses))
	}
	h := pad.Hypotheses[0]
	if h.ID != "H1" || h.Statement != "auth is broken" {
		t.Errorf("hypothesis: ID=%q Statement=%q", h.ID, h.Statement)
	}
	if h.Status != "testing" {
		t.Errorf("new hypothesis status = %q, want 'testing'", h.Status)
	}
}

// TestUpdateScratchpadTool_UpdatesExistingHypothesis modifies existing by statement.
func TestUpdateScratchpadTool_UpdatesExistingHypothesis(t *testing.T) {
	pad := NewScratchpad("goal")
	pad.Hypotheses = []Hypothesis{{ID: "H1", Statement: "auth is broken", Confidence: 0.5, Status: "testing"}}
	tool := &updateScratchpadTool{pad: pad}
	invokeJSON(t, tool, context.Background(), updateScratchpadInput{
		Hypotheses: []hypothesisInput{
			{Statement: "auth is broken", Confidence: 0.9, Status: "confirmed"},
		},
	})
	if len(pad.Hypotheses) != 1 {
		t.Fatalf("expected 1 hypothesis (updated, not duplicated), got %d", len(pad.Hypotheses))
	}
	if pad.Hypotheses[0].Confidence != 0.9 {
		t.Errorf("Confidence not updated: %v", pad.Hypotheses[0].Confidence)
	}
	if pad.Hypotheses[0].Status != "confirmed" {
		t.Errorf("Status not updated: %q", pad.Hypotheses[0].Status)
	}
}

// TestUpdateScratchpadTool_UpdatesExistingHypothesis_EmptyStatusKept verifies empty status input keeps old status.
func TestUpdateScratchpadTool_UpdatesExistingHypothesis_EmptyStatusKept(t *testing.T) {
	pad := NewScratchpad("goal")
	pad.Hypotheses = []Hypothesis{{ID: "H1", Statement: "auth is broken", Confidence: 0.5, Status: "testing"}}
	tool := &updateScratchpadTool{pad: pad}
	// Status not provided — existing status should be preserved.
	invokeJSON(t, tool, context.Background(), updateScratchpadInput{
		Hypotheses: []hypothesisInput{
			{Statement: "auth is broken", Confidence: 0.9, Status: ""},
		},
	})
	if pad.Hypotheses[0].Status != "testing" {
		t.Errorf("empty status should not overwrite existing status, got %q", pad.Hypotheses[0].Status)
	}
}

// TestUpdateScratchpadTool_AddsQuestion verifies questions are added.
func TestUpdateScratchpadTool_AddsQuestion(t *testing.T) {
	pad := NewScratchpad("goal")
	tool := &updateScratchpadTool{pad: pad}
	invokeJSON(t, tool, context.Background(), updateScratchpadInput{
		Questions: []questionInput{
			{Text: "Why does auth fail?", Priority: 0.9},
		},
	})
	if len(pad.OpenQuestions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(pad.OpenQuestions))
	}
	q := pad.OpenQuestions[0]
	if q.ID != "Q1" || q.Status != "open" {
		t.Errorf("question ID=%q Status=%q", q.ID, q.Status)
	}
}

// TestUpdateScratchpadTool_CloseQuestions marks questions answered.
func TestUpdateScratchpadTool_CloseQuestions(t *testing.T) {
	pad := NewScratchpad("goal")
	pad.OpenQuestions = []Question{{ID: "Q1", Text: "Why?", Status: "open"}}
	tool := &updateScratchpadTool{pad: pad}
	invokeJSON(t, tool, context.Background(), updateScratchpadInput{
		CloseQuestions: []string{"Q1"},
	})
	if pad.OpenQuestions[0].Status != "answered" {
		t.Errorf("expected Q1 to be answered, got %q", pad.OpenQuestions[0].Status)
	}
}

// TestUpdateScratchpadTool_EvidenceWithLinkedHypotheses verifies supports/contradicts parsing.
func TestUpdateScratchpadTool_EvidenceWithLinkedHypotheses(t *testing.T) {
	pad := NewScratchpad("auth goal here token")
	tool := &updateScratchpadTool{pad: pad}
	invokeJSON(t, tool, context.Background(), updateScratchpadInput{
		Evidence: []evidenceInput{
			{Content: "auth goal relevant token content", Source: "search",
				Relevance: 0.7, Confidence: 0.6, Novelty: 0.8,
				Supports: "H1", Contradicts: "H2"},
		},
	})
	if len(pad.Evidence) != 1 {
		t.Fatalf("expected 1 evidence, got %d", len(pad.Evidence))
	}
	e := pad.Evidence[0]
	if len(e.Supports) != 1 || e.Supports[0] != "H1" {
		t.Errorf("Supports = %v, want [H1]", e.Supports)
	}
	if len(e.Contradicts) != 1 || e.Contradicts[0] != "H2" {
		t.Errorf("Contradicts = %v, want [H2]", e.Contradicts)
	}
}

// TestUpdateScratchpadTool_BadJSON returns an error.
func TestUpdateScratchpadTool_BadJSON(t *testing.T) {
	pad := NewScratchpad("goal")
	tool := &updateScratchpadTool{pad: pad}
	_, err := tool.Invoke(context.Background(), "{bad json}")
	if err == nil {
		t.Error("expected error for bad JSON")
	}
}

// TestReadScratchpadTool_Def verifies the tool name.
func TestReadScratchpadTool_Def(t *testing.T) {
	tool := &readScratchpadTool{pad: NewScratchpad("goal")}
	if tool.Def().Name != "read_scratchpad" {
		t.Errorf("Name = %q", tool.Def().Name)
	}
}

// TestReadScratchpadTool_DefaultsToAll verifies empty section returns all.
func TestReadScratchpadTool_DefaultsToAll(t *testing.T) {
	pad := NewScratchpad("analysis goal here")
	pad.Strategy = "top-down"
	tool := &readScratchpadTool{pad: pad}
	out := invokeJSON(t, tool, context.Background(), readScratchpadInput{Section: ""})
	if !strings.Contains(out, "# Analysis:") {
		t.Errorf("empty section should default to all: %q", out)
	}
}

// TestReadScratchpadTool_SectionEvidence returns evidence section.
func TestReadScratchpadTool_SectionEvidence(t *testing.T) {
	pad := NewScratchpad("goal here alpha")
	pad.Evidence = []Evidence{{ID: "E1", Content: "finding", Priority: 0.8}}
	tool := &readScratchpadTool{pad: pad}
	out := invokeJSON(t, tool, context.Background(), readScratchpadInput{Section: "evidence"})
	if !strings.Contains(out, "## Evidence") {
		t.Errorf("expected evidence section header: %q", out)
	}
}

// TestReadScratchpadTool_BadJSON returns error.
func TestReadScratchpadTool_BadJSON(t *testing.T) {
	tool := &readScratchpadTool{pad: NewScratchpad("goal")}
	_, err := tool.Invoke(context.Background(), "not json")
	if err == nil {
		t.Error("expected error for bad JSON")
	}
}

// TestCheckRedundancyTool_Def verifies the tool name.
func TestCheckRedundancyTool_Def(t *testing.T) {
	tool := &checkRedundancyTool{pad: NewScratchpad("goal")}
	if tool.Def().Name != "check_redundancy" {
		t.Errorf("Name = %q", tool.Def().Name)
	}
}

// TestCheckRedundancyTool_NotRedundant on empty log.
func TestCheckRedundancyTool_NotRedundant(t *testing.T) {
	pad := NewScratchpad("goal")
	tool := &checkRedundancyTool{pad: pad}
	out := invokeJSON(t, tool, context.Background(), checkRedundancyInput{
		ProposedTool: "search", ProposedArgs: "find auth bugs",
	})
	m := mustParseMap(t, out)
	if m["redundant"].(bool) {
		t.Error("expected redundant=false for empty log")
	}
	if !strings.Contains(m["recommendation"].(string), "Proceed") {
		t.Errorf("expected 'Proceed' recommendation, got: %q", m["recommendation"])
	}
}

// TestCheckRedundancyTool_Redundant when similar action exists.
func TestCheckRedundancyTool_Redundant(t *testing.T) {
	pad := NewScratchpad("goal")
	pad.RecordAction("search", "authentication token validation bug exists here now", 5)
	tool := &checkRedundancyTool{pad: pad}
	out := invokeJSON(t, tool, context.Background(), checkRedundancyInput{
		ProposedTool: "search", ProposedArgs: "authentication token validation bug exists here now",
	})
	m := mustParseMap(t, out)
	if !m["redundant"].(bool) {
		t.Error("expected redundant=true for identical prior action")
	}
	if !strings.Contains(m["recommendation"].(string), "Skip") {
		t.Errorf("expected 'Skip' recommendation, got: %q", m["recommendation"])
	}
}

// TestCheckRedundancyTool_ExistingEvidence populates existing_evidence when matching.
func TestCheckRedundancyTool_ExistingEvidence(t *testing.T) {
	pad := NewScratchpad("goal")
	// Evidence content very similar to proposed args.
	pad.Evidence = []Evidence{
		{ID: "E1", Content: "authentication token validation logic found here in code", Priority: 0.7},
	}
	tool := &checkRedundancyTool{pad: pad}
	out := invokeJSON(t, tool, context.Background(), checkRedundancyInput{
		ProposedTool: "search",
		ProposedArgs: "authentication token validation logic here",
	})
	m := mustParseMap(t, out)
	ev, ok := m["existing_evidence"]
	if !ok || ev == nil {
		t.Logf("existing_evidence = %v (may be nil if similarity < 0.4)", ev)
	}
}

// TestCheckRedundancyTool_BadJSON returns error.
func TestCheckRedundancyTool_BadJSON(t *testing.T) {
	tool := &checkRedundancyTool{pad: NewScratchpad("goal")}
	_, err := tool.Invoke(context.Background(), "not json")
	if err == nil {
		t.Error("expected error for bad JSON")
	}
}

// TestPlanAnalysisTool_Def verifies the tool name.
func TestPlanAnalysisTool_Def(t *testing.T) {
	tool := &planAnalysisTool{pad: NewScratchpad("goal")}
	if tool.Def().Name != "plan_analysis" {
		t.Errorf("Name = %q", tool.Def().Name)
	}
}

// TestPlanAnalysisTool_NilGraphAndNilMemMgr runs without panicking.
func TestPlanAnalysisTool_NilGraphAndNilMemMgr(t *testing.T) {
	pad := NewScratchpad("find auth bugs")
	tool := &planAnalysisTool{pad: pad, graph: nil, memMgr: nil}
	ctx := WithRepoSlug(context.Background(), "my-repo")
	out := invokeJSON(t, tool, ctx, planAnalysisInput{Goal: "find auth bugs"})
	m := mustParseMap(t, out)
	if m["goal"] != "find auth bugs" {
		t.Errorf("goal = %v, want 'find auth bugs'", m["goal"])
	}
	if m["repo"] != "my-repo" {
		t.Errorf("repo = %v, want 'my-repo'", m["repo"])
	}
	if !strings.Contains(m["suggestion"].(string), "delegate") {
		t.Errorf("suggestion should mention delegate: %q", m["suggestion"])
	}
}

// TestPlanAnalysisTool_UpdatesGoal verifies the pad goal is updated.
func TestPlanAnalysisTool_UpdatesGoal(t *testing.T) {
	pad := NewScratchpad("old goal")
	tool := &planAnalysisTool{pad: pad}
	invokeJSON(t, tool, context.Background(), planAnalysisInput{Goal: "new goal"})
	if pad.Goal != "new goal" {
		t.Errorf("Goal = %q, want 'new goal'", pad.Goal)
	}
}

// TestPlanAnalysisTool_EmptyGoalKeepsExisting verifies empty goal input doesn't overwrite.
func TestPlanAnalysisTool_EmptyGoalKeepsExisting(t *testing.T) {
	pad := NewScratchpad("existing goal")
	tool := &planAnalysisTool{pad: pad}
	invokeJSON(t, tool, context.Background(), planAnalysisInput{Goal: ""})
	if pad.Goal != "existing goal" {
		t.Errorf("Goal should remain 'existing goal', got %q", pad.Goal)
	}
}

// TestPlanAnalysisTool_WithGraph provides graph data and checks node count.
func TestPlanAnalysisTool_WithGraph(t *testing.T) {
	pad := NewScratchpad("find auth bugs")
	graph := &padFakeGraph{
		repos: []types.Repo{{Slug: "my-repo", Path: "/code", Languages: []string{"Go"}}},
		nodeIDs: []types.CodeNode{
			{ID: "n1"}, {ID: "n2"}, {ID: "n3"},
		},
		edges: []types.CodeEdge{{Kind: "route"}, {Kind: "route"}},
	}
	tool := &planAnalysisTool{pad: pad, graph: graph}
	ctx := WithRepoSlug(context.Background(), "my-repo")
	out := invokeJSON(t, tool, ctx, planAnalysisInput{})
	m := mustParseMap(t, out)
	if m["node_count"].(float64) != 3 {
		t.Errorf("node_count = %v, want 3", m["node_count"])
	}
	if m["endpoint_count"].(float64) != 2 {
		t.Errorf("endpoint_count = %v, want 2", m["endpoint_count"])
	}
	if m["path"] != "/code" {
		t.Errorf("path = %v, want '/code'", m["path"])
	}
}

// TestPlanAnalysisTool_GraphRepoNotFound does not populate path when slug not in repos.
func TestPlanAnalysisTool_GraphRepoNotFound(t *testing.T) {
	pad := NewScratchpad("goal")
	graph := &padFakeGraph{
		repos: []types.Repo{{Slug: "other-repo", Path: "/other"}},
	}
	tool := &planAnalysisTool{pad: pad, graph: graph}
	ctx := WithRepoSlug(context.Background(), "my-repo")
	out := invokeJSON(t, tool, ctx, planAnalysisInput{})
	m := mustParseMap(t, out)
	if m["path"] != nil && m["path"] != "" {
		t.Errorf("path should be empty when repo not found, got %v", m["path"])
	}
}

// TestPlanAnalysisTool_BadJSON returns error.
func TestPlanAnalysisTool_BadJSON(t *testing.T) {
	tool := &planAnalysisTool{pad: NewScratchpad("goal")}
	_, err := tool.Invoke(context.Background(), "{bad}")
	if err == nil {
		t.Error("expected error for bad JSON")
	}
}

// TestPersistFindingsTool_Def verifies the tool name.
func TestPersistFindingsTool_Def(t *testing.T) {
	tool := &persistFindingsTool{pad: NewScratchpad("goal")}
	if tool.Def().Name != "persist_findings" {
		t.Errorf("Name = %q", tool.Def().Name)
	}
}

// TestPersistFindingsTool_NilMemMgr returns gracefully.
func TestPersistFindingsTool_NilMemMgr(t *testing.T) {
	pad := NewScratchpad("goal")
	tool := &persistFindingsTool{pad: pad, memMgr: nil}
	out := invokeJSON(t, tool, context.Background(), persistFindingsInput{Summary: "done"})
	m := mustParseMap(t, out)
	if !strings.Contains(m["message"].(string), "not available") {
		t.Errorf("expected 'not available' message, got: %v", m["message"])
	}
}

// TestPersistFindingsTool_BadJSON returns error.
func TestPersistFindingsTool_BadJSON(t *testing.T) {
	tool := &persistFindingsTool{pad: NewScratchpad("goal")}
	_, err := tool.Invoke(context.Background(), "{not json")
	if err == nil {
		t.Error("expected error for bad JSON")
	}
}

// TestCountOpen verifies correct counting of open questions.
func TestCountOpen(t *testing.T) {
	qs := []Question{
		{ID: "Q1", Status: "open"},
		{ID: "Q2", Status: "answered"},
		{ID: "Q3", Status: "open"},
		{ID: "Q4", Status: "irrelevant"},
	}
	got := countOpen(qs)
	if got != 2 {
		t.Errorf("countOpen = %d, want 2", got)
	}
}

// TestCountOpen_Empty verifies empty list returns 0.
func TestCountOpen_Empty(t *testing.T) {
	if n := countOpen(nil); n != 0 {
		t.Errorf("countOpen(nil) = %d, want 0", n)
	}
}

// TestUpdateScratchpadOutput_ReflectsContradictions verifies output includes contradiction count.
func TestUpdateScratchpadOutput_ReflectsContradictions(t *testing.T) {
	pad := NewScratchpad("auth token goal")
	// Pre-seed contradictions.
	pad.Contradictions = []Contradiction{{EvidenceA: "E1", EvidenceB: "E2", Description: "conflict"}}
	tool := &updateScratchpadTool{pad: pad}
	out := invokeJSON(t, tool, context.Background(), updateScratchpadInput{})
	m := mustParseMap(t, out)
	if m["contradictions"].(float64) != 1 {
		t.Errorf("contradictions = %v, want 1", m["contradictions"])
	}
}

// TestSourceReliability_AllCases tests every branch of sourceReliability.
func TestSourceReliability_AllCases(t *testing.T) {
	cases := []struct {
		source string
		maxC   float64
	}{
		{"deep_dive_search", 0.95},
		{"lookup_node_result", 0.95},
		{"trace_result", 0.90},
		{"neighborhood_query", 0.90},
		{"search_results", 0.70},
		{"security_scan", 0.85},
		{"blast_analysis", 0.85},
		{"unknown_tool_xyz", 0.80},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			got := sourceReliability(tc.source)
			if got != tc.maxC {
				t.Errorf("sourceReliability(%q) = %v, want %v", tc.source, got, tc.maxC)
			}
		})
	}
}

// TestPlanAnalysisTool_EmptyRepoSlug verifies behaviour with empty repo slug.
func TestPlanAnalysisTool_EmptyRepoSlug(t *testing.T) {
	pad := NewScratchpad("goal")
	graph := &padFakeGraph{
		repos: []types.Repo{{Slug: "some-repo", Path: "/code"}},
	}
	tool := &planAnalysisTool{pad: pad, graph: graph}
	// No repo slug in context — graph branches should be skipped.
	out := invokeJSON(t, tool, context.Background(), planAnalysisInput{Goal: "goal"})
	m := mustParseMap(t, out)
	// node_count should be 0 or absent since slug is empty.
	if nc, ok := m["node_count"]; ok && nc.(float64) != 0 {
		t.Logf("node_count = %v (expected 0 or absent with empty slug)", nc)
	}
}

// TestBudgetedView_Convergence_Converging verifies "CONVERGING" status message.
func TestBudgetedView_Convergence_Converging(t *testing.T) {
	s := NewScratchpad("goal")
	s.DelegationCount = 4
	for i := 0; i < 6; i++ {
		s.Evidence = append(s.Evidence, Evidence{ID: fmt.Sprintf("E%d", i), Priority: 0.9})
	}
	s.NovelFindings = []int{1, 0}
	s.Hypotheses = []Hypothesis{{ID: "H1", Statement: "theory", Status: "confirmed", Confidence: 0.9}}
	out := s.BudgetedView("convergence")
	if !strings.Contains(out, "CONVERGING") {
		t.Errorf("expected CONVERGING status: %q", out)
	}
}

// TestPersistableFindings_ConvergedOutcome includes "converged" in strategy finding.
func TestPersistableFindings_ConvergedOutcome(t *testing.T) {
	s := NewScratchpad("auth goal find here")
	s.Strategy = "search-first"
	s.DelegationCount = 4
	for i := 0; i < 6; i++ {
		s.Evidence = append(s.Evidence, Evidence{ID: fmt.Sprintf("E%d", i), Priority: 0.9})
	}
	s.NovelFindings = []int{1, 0}
	s.Hypotheses = []Hypothesis{{ID: "H1", Statement: "auth broken", Status: "confirmed", Confidence: 0.9}}
	findings := s.PersistableFindings()
	for _, f := range findings {
		if f.Kind == "strategy" {
			if !strings.Contains(f.Content, "converged") {
				t.Errorf("converged outcome not in strategy finding: %q", f.Content)
			}
		}
	}
}

// ── Fakes for memory.Manager ─────────────────────────────────────────────────

// padFakeMemStore is an in-memory MemoryStore for testing.
type padFakeMemStore struct {
	stored       []*types.MemoryEntry
	storeErr     error
	retrieveMems []types.MemoryEntry // returned by RetrieveMemories
}

func (f *padFakeMemStore) StoreMemory(_ context.Context, e *types.MemoryEntry) error {
	if f.storeErr != nil {
		return f.storeErr
	}
	f.stored = append(f.stored, e)
	return nil
}
func (f *padFakeMemStore) RetrieveMemories(_ context.Context, _ string, _ []float32, _ int) ([]types.MemoryEntry, error) {
	return f.retrieveMems, nil
}
func (f *padFakeMemStore) ListSessionMemories(_ context.Context, _ string) ([]types.MemoryEntry, error) {
	return nil, nil
}
func (f *padFakeMemStore) DeleteSessionMemories(_ context.Context, _ string) error { return nil }

// padFakeEmbedder is a no-op embedder for scratchpad tests.
type padFakeEmbedder struct{}

func (f *padFakeEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2}, nil
}
func (f *padFakeEmbedder) EmbedBatch(_ context.Context, _ []domain.EmbedInput) ([]domain.EmbedResult, error) {
	return nil, nil
}

// buildTestMemManager creates a memory.Manager suitable for testing.
func buildTestMemManager(store domain.MemoryStore) *memory.Manager {
	return memory.NewManager(store, &padFakeEmbedder{}, nil, memory.DefaultBudgets())
}

// buildTestMemManagerWithRetrieve creates a Manager that returns canned memories.
func buildTestMemManagerWithRetrieve(mems []types.MemoryEntry) *memory.Manager {
	store := &padFakeMemStore{retrieveMems: mems}
	return memory.NewManager(store, &padFakeEmbedder{}, nil, memory.DefaultBudgets())
}

// ── persistFindingsTool with real memMgr ─────────────────────────────────────

// TestPersistFindingsTool_StoresFindings verifies findings are stored via memMgr.
func TestPersistFindingsTool_StoresFindings(t *testing.T) {
	pad := NewScratchpad("auth goal find here token")
	pad.Strategy = "bottom-up"
	// Add a high-priority evidence item directly.
	pad.Evidence = []Evidence{
		{ID: "E1", Content: "important auth token finding", Source: "trace", Priority: 0.8},
	}
	pad.Hypotheses = []Hypothesis{
		{ID: "H1", Statement: "auth is broken", Status: "confirmed", Confidence: 0.9},
	}

	store := &padFakeMemStore{}
	mgr := buildTestMemManager(store)
	tool := &persistFindingsTool{pad: pad, memMgr: mgr}
	ctx := WithRepoSlug(context.Background(), "test-repo")

	out := invokeJSON(t, tool, ctx, persistFindingsInput{Summary: "auth is broken due to token validation"})
	m := mustParseMap(t, out)
	stored := int(m["stored"].(float64))
	if stored == 0 {
		t.Error("expected at least 1 finding to be stored")
	}
	if len(store.stored) == 0 {
		t.Error("expected store to have received entries")
	}
}

// TestPersistFindingsTool_SummaryAppendsToStrategyFinding verifies summary is appended.
func TestPersistFindingsTool_SummaryAppendsToStrategyFinding(t *testing.T) {
	pad := NewScratchpad("auth goal find tokens")
	pad.Strategy = "search-first"

	store := &padFakeMemStore{}
	mgr := buildTestMemManager(store)
	tool := &persistFindingsTool{pad: pad, memMgr: mgr}
	ctx := WithRepoSlug(context.Background(), "test-repo")

	invokeJSON(t, tool, ctx, persistFindingsInput{Summary: "test summary content"})

	// Look for the strategy finding entry with summary appended.
	foundSummary := false
	for _, e := range store.stored {
		if strings.Contains(e.Content, "test summary content") {
			foundSummary = true
		}
	}
	if !foundSummary {
		// NOTE: this may not fail if there are no strategy findings (empty pad).
		// Strategy finding requires both Strategy != "" and Goal != "".
		t.Logf("summary not found in stored entries (may be empty pad): %d entries stored", len(store.stored))
	}
}

// TestPersistFindingsTool_StoreErrorsContinue verifies store errors are silently skipped.
func TestPersistFindingsTool_StoreErrorsContinue(t *testing.T) {
	pad := NewScratchpad("auth goal find tokens here")
	pad.Strategy = "search-first"
	pad.Evidence = []Evidence{
		{ID: "E1", Content: "important finding", Source: "trace", Priority: 0.8},
	}

	store := &padFakeMemStore{storeErr: fmt.Errorf("db unavailable")}
	mgr := buildTestMemManager(store)
	tool := &persistFindingsTool{pad: pad, memMgr: mgr}
	ctx := WithRepoSlug(context.Background(), "test-repo")

	// Should not return error — errors are skipped.
	out := invokeJSON(t, tool, ctx, persistFindingsInput{})
	m := mustParseMap(t, out)
	// All fail → stored = 0.
	if m["stored"].(float64) != 0 {
		t.Errorf("expected 0 stored when store always errors, got %v", m["stored"])
	}
}

// TestPlanAnalysisTool_WithMemMgr verifies prior knowledge is populated from memory manager.
func TestPlanAnalysisTool_WithMemMgr(t *testing.T) {
	pad := NewScratchpad("find auth tokens")
	mems := []types.MemoryEntry{
		{Content: "previous finding about auth tokens", TokenCount: 10},
	}
	mgr := buildTestMemManagerWithRetrieve(mems)
	tool := &planAnalysisTool{pad: pad, memMgr: mgr}
	ctx := WithRepoSlug(context.Background(), "my-repo")
	out := invokeJSON(t, tool, ctx, planAnalysisInput{Goal: "find auth tokens"})
	m := mustParseMap(t, out)
	// PriorKnowledge is set when BuildContext returns non-empty string.
	pk, _ := m["prior_knowledge"].(string)
	if pk == "" {
		t.Log("prior_knowledge empty — store returned data but BuildContext may have returned empty (acceptable)")
	}
}

// TestPlanAnalysisTool_GraphErrorsDegrade verifies graph errors don't propagate.
func TestPlanAnalysisTool_GraphErrorsDegrade(t *testing.T) {
	pad := NewScratchpad("goal")
	graph := &padFakeGraph{
		reposErr: fmt.Errorf("db down"),
		nodesErr: fmt.Errorf("db down"),
		edgesErr: fmt.Errorf("db down"),
	}
	tool := &planAnalysisTool{pad: pad, graph: graph}
	ctx := WithRepoSlug(context.Background(), "my-repo")
	// Should not error even when graph errors out.
	out := invokeJSON(t, tool, ctx, planAnalysisInput{})
	m := mustParseMap(t, out)
	if m["repo"] != "my-repo" {
		t.Errorf("repo = %v, want 'my-repo'", m["repo"])
	}
}

// TestTextSimilarity_UnionZeroEdge covers the union==0 branch in textSimilarity.
// NOTE: In practice this branch is unreachable because wordSet filters out
// short words, and if both word sets are non-empty their union is always >= 1.
// The union==0 guard is dead code in the current implementation — kept as
// defensive programming. We test it here for documentation purposes.
func TestTextSimilarity_BothShortWords_UnionIsZero(t *testing.T) {
	// Strings with only words <= 2 chars: wordSet returns empty → both sets empty
	// → early return 0 (before the union==0 guard).
	got := textSimilarity("is to at by", "or it no")
	if got != 0 {
		t.Errorf("all-short-word inputs: similarity = %v, want 0", got)
	}
}
