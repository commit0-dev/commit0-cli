package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Scratchpad is the analysis memory — persists across all tool calls and
// delegations within one analysis session. It is the memory, the ranking
// system, and the feedback loop.
type Scratchpad struct {
	// What we're investigating.
	Goal     string     `json:"goal"`
	Strategy string     `json:"strategy"`
	Plan     []PlanStep `json:"plan"`

	// What we've found (ranked by Priority).
	Evidence []Evidence `json:"evidence"`

	// What we've done (prevents redundant queries).
	ActionLog []Action `json:"action_log"`

	// What we still need to know.
	OpenQuestions []Question `json:"open_questions"`

	// What we think is going on.
	Hypotheses []Hypothesis `json:"hypotheses"`

	// Convergence tracking.
	DelegationCount        int   `json:"delegation_count"`
	NovelFindings          []int `json:"novel_findings"` // novel count per delegation
	UpdatedSinceDelegation bool  `json:"updated_since_delegation"`

	// Budget.
	TokenBudget    int     `json:"token_budget"`    // max tokens for read output (default: 4000)
	CostBudget     float64 `json:"cost_budget"`     // max cost in dollars (default: 1.00)
	CostConsumed   float64 `json:"cost_consumed"`   // running total
	TokensConsumed int     `json:"tokens_consumed"` // total tokens used

	// Contradictions detected.
	Contradictions []Contradiction `json:"contradictions"`

	mu             sync.Mutex `json:"-"` // protects all fields from concurrent access
	nextEvidenceID int
	nextQuestionID int
}

// NewScratchpad creates a scratchpad with default budgets.
func NewScratchpad(goal string) *Scratchpad {
	return &Scratchpad{
		Goal:        goal,
		TokenBudget: 4000,
		CostBudget:  1.00,
	}
}

// PlanStep is one step in the investigation plan.
type PlanStep struct {
	Step      int    `json:"step"`
	Action    string `json:"action"`
	AgentType string `json:"agent_type"` // search, trace, security, deep_dive
	Status    string `json:"status"`     // pending, done, skipped
}

// Evidence is a scored finding with provenance.
type Evidence struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	Source     string `json:"source"` // tool or delegation that produced it
	SourceArgs string `json:"source_args"`

	// Scores (0.0-1.0) — proposed by agent, validated by server.
	Relevance     float64 `json:"relevance"`
	Confidence    float64 `json:"confidence"`
	Novelty       float64 `json:"novelty"`
	Actionability float64 `json:"actionability"`

	// Computed.
	Priority float64 `json:"priority"`

	// Provenance.
	Delegation int       `json:"delegation"`
	Timestamp  time.Time `json:"timestamp"`

	// Links to hypotheses.
	Supports    []string `json:"supports,omitempty"`
	Contradicts []string `json:"contradicts_hyp,omitempty"`
}

// Action records a tool call to prevent redundancy.
type Action struct {
	Tool       string    `json:"tool"`
	Args       string    `json:"args"`
	ResultHash string    `json:"result_hash"`
	ResultSize int       `json:"result_size"`
	Useful     bool      `json:"useful"`
	Timestamp  time.Time `json:"timestamp"`
}

// Question is an open investigation question.
type Question struct {
	ID       string  `json:"id"`
	Text     string  `json:"text"`
	Priority float64 `json:"priority"`
	Status   string  `json:"status"` // open, answered, irrelevant
	Answer   string  `json:"answer,omitempty"`
	Source   string  `json:"source"` // evidence ID or delegation that raised it
}

// Hypothesis is a theory under test.
type Hypothesis struct {
	ID            string   `json:"id"`
	Statement     string   `json:"statement"`
	Confidence    float64  `json:"confidence"`
	Supporting    []string `json:"supporting"`    // evidence IDs
	Contradicting []string `json:"contradicting"` // evidence IDs
	Status        string   `json:"status"`        // testing, confirmed, rejected, inconclusive
}

// Contradiction records conflicting evidence.
type Contradiction struct {
	EvidenceA   string `json:"evidence_a"`
	EvidenceB   string `json:"evidence_b"`
	Description string `json:"description"`
	Resolved    bool   `json:"resolved"`
}

// ── Evidence Management ─────────────────────────────────────────────────────

// AddEvidence adds a new evidence item with server-side score validation.
func (s *Scratchpad) AddEvidence(e Evidence) Evidence {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextEvidenceID++
	e.ID = fmt.Sprintf("E%d", s.nextEvidenceID)
	e.Timestamp = time.Now()
	e.Delegation = s.DelegationCount

	// Server-side validation.
	e = s.validateScores(e)

	// Check for contradictions with existing evidence.
	for _, existing := range s.Evidence {
		if c := detectContradiction(e, existing); c != nil {
			s.Contradictions = append(s.Contradictions, *c)
			// Reduce confidence on both.
			e.Confidence = max(0, e.Confidence-0.2)
			for i := range s.Evidence {
				if s.Evidence[i].ID == existing.ID {
					s.Evidence[i].Confidence = max(0, s.Evidence[i].Confidence-0.2)
					s.Evidence[i].Priority = computePriority(s.Evidence[i])
				}
			}
			// Auto-generate resolution question.
			s.nextQuestionID++
			s.OpenQuestions = append(s.OpenQuestions, Question{
				ID:       fmt.Sprintf("Q%d", s.nextQuestionID),
				Text:     fmt.Sprintf("Contradiction: '%s' vs '%s' — which is correct?", truncateStr(e.Content, 60), truncateStr(existing.Content, 60)),
				Priority: 0.9,
				Status:   "open",
				Source:   e.ID,
			})
		}
	}

	e.Priority = computePriority(e)
	s.Evidence = append(s.Evidence, e)
	s.UpdatedSinceDelegation = true
	return e
}

// validateScores applies server-side adjustments to prevent hallucinated scores.
func (s *Scratchpad) validateScores(e Evidence) Evidence {
	// Clamp all scores to [0, 1].
	e.Relevance = clamp(e.Relevance)
	e.Confidence = clamp(e.Confidence)
	e.Novelty = clamp(e.Novelty)
	e.Actionability = clamp(e.Actionability)

	// NOVELTY: check for near-duplicates.
	for _, existing := range s.Evidence {
		if textSimilarity(e.Content, existing.Content) > 0.8 {
			e.Novelty = min(e.Novelty, 0.1)
			break
		}
	}

	// RELEVANCE: penalize if no goal keywords match.
	if !containsAnyKeyword(e.Content, s.Goal) {
		e.Relevance = min(e.Relevance, 0.5)
	}

	// CONFIDENCE: cap by source reliability.
	e.Confidence = min(e.Confidence, sourceReliability(e.Source))

	return e
}

// sourceReliability returns the max confidence for a given source type.
func sourceReliability(source string) float64 {
	switch {
	case strings.Contains(source, "deep_dive"), strings.Contains(source, "lookup_node"):
		return 0.95 // direct code observation
	case strings.Contains(source, "trace"), strings.Contains(source, "neighborhood"):
		return 0.90 // structural analysis
	case strings.Contains(source, "search"):
		return 0.70 // search results need verification
	case strings.Contains(source, "security"), strings.Contains(source, "blast"):
		return 0.85 // analysis results
	default:
		return 0.80
	}
}

func computePriority(e Evidence) float64 {
	return 0.3*e.Relevance + 0.3*e.Confidence + 0.2*e.Novelty + 0.2*e.Actionability
}

// ── Action Log ──────────────────────────────────────────────────────────────

// RecordAction logs a tool call for redundancy checking.
func (s *Scratchpad) RecordAction(tool, args string, resultSize int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ActionLog = append(s.ActionLog, Action{
		Tool:       tool,
		Args:       args,
		ResultHash: hashStr(args),
		ResultSize: resultSize,
		Timestamp:  time.Now(),
	})
}

// AlreadyTried checks if a similar action was already performed.
func (s *Scratchpad) AlreadyTried(tool, args string) (bool, []Action) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var similar []Action
	for _, a := range s.ActionLog {
		if a.Tool == tool && textSimilarity(a.Args, args) > 0.7 {
			similar = append(similar, a)
		}
	}
	return len(similar) > 0, similar
}

// ── Convergence ─────────────────────────────────────────────────────────────

// ConvergenceCheck evaluates all 5 convergence gates.
func (s *Scratchpad) ConvergenceCheck() (bool, []string) {
	var failures []string

	// Gate 1: Minimum delegations.
	if s.DelegationCount < 3 {
		failures = append(failures, fmt.Sprintf("need at least 3 delegations (have %d)", s.DelegationCount))
	}

	// Gate 2: Minimum high-priority evidence.
	highPriority := 0
	for _, e := range s.Evidence {
		if e.Priority > 0.5 {
			highPriority++
		}
	}
	if highPriority < 5 {
		failures = append(failures, fmt.Sprintf("need 5+ high-priority evidence (have %d)", highPriority))
	}

	// Gate 3: No high-priority open questions.
	for _, q := range s.OpenQuestions {
		if q.Status == "open" && q.Priority > 0.7 {
			failures = append(failures, fmt.Sprintf("open question (priority %.1f): %s", q.Priority, truncateStr(q.Text, 60)))
			break // report first one only
		}
	}

	// Gate 4: Diminishing returns.
	if len(s.NovelFindings) >= 2 {
		last := s.NovelFindings[len(s.NovelFindings)-1]
		prev := s.NovelFindings[len(s.NovelFindings)-2]
		if last >= 2 || prev >= 2 {
			failures = append(failures, "still finding novel information")
		}
	}

	// Gate 5: At least one hypothesis resolved.
	hasConclusion := false
	for _, h := range s.Hypotheses {
		if h.Status == "confirmed" || h.Status == "rejected" {
			hasConclusion = true
			break
		}
	}
	if !hasConclusion && len(s.Hypotheses) > 0 {
		failures = append(failures, "no hypotheses confirmed or rejected")
	}

	// Gate 6: No unresolved contradictions.
	for _, c := range s.Contradictions {
		if !c.Resolved {
			failures = append(failures, fmt.Sprintf("unresolved contradiction: %s", truncateStr(c.Description, 60)))
			break
		}
	}

	return len(failures) == 0, failures
}

// ── Budgeted View ───────────────────────────────────────────────────────────

// BudgetedView returns a token-limited summary of the scratchpad.
func (s *Scratchpad) BudgetedView(section string) string {
	switch section {
	case "evidence":
		return s.viewEvidence()
	case "questions":
		return s.viewQuestions()
	case "hypotheses":
		return s.viewHypotheses()
	case "action_log":
		return s.viewActionLog()
	case "convergence":
		return s.viewConvergence()
	default: // "all"
		return s.viewAll()
	}
}

func (s *Scratchpad) viewEvidence() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Evidence (%d items)\n", len(s.Evidence)))
	// Top 10 by priority.
	sorted := topN(s.Evidence, 10)
	for _, e := range sorted {
		fmt.Fprintf(&sb, "- [%s] (P:%.2f R:%.1f C:%.1f N:%.1f) %s\n",
			e.ID, e.Priority, e.Relevance, e.Confidence, e.Novelty,
			truncateStr(e.Content, 80))
	}
	return sb.String()
}

func (s *Scratchpad) viewQuestions() string {
	var sb strings.Builder
	open := 0
	for _, q := range s.OpenQuestions {
		if q.Status == "open" {
			open++
		}
	}
	sb.WriteString(fmt.Sprintf("## Open Questions (%d)\n", open))
	for _, q := range s.OpenQuestions {
		if q.Status == "open" {
			fmt.Fprintf(&sb, "- [%s] (priority:%.1f) %s\n", q.ID, q.Priority, q.Text)
		}
	}
	return sb.String()
}

func (s *Scratchpad) viewHypotheses() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Hypotheses (%d)\n", len(s.Hypotheses)))
	for _, h := range s.Hypotheses {
		fmt.Fprintf(&sb, "- [%s] %s (confidence:%.2f, status:%s, +%d/-%d evidence)\n",
			h.ID, truncateStr(h.Statement, 60), h.Confidence, h.Status,
			len(h.Supporting), len(h.Contradicting))
	}
	return sb.String()
}

func (s *Scratchpad) viewActionLog() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Action Log (last 5 of %d)\n", len(s.ActionLog)))
	start := len(s.ActionLog) - 5
	if start < 0 {
		start = 0
	}
	for _, a := range s.ActionLog[start:] {
		fmt.Fprintf(&sb, "- %s(%s) → %d results\n", a.Tool, truncateStr(a.Args, 40), a.ResultSize)
	}
	return sb.String()
}

func (s *Scratchpad) viewConvergence() string {
	converging, failures := s.ConvergenceCheck()
	var sb strings.Builder
	sb.WriteString("## Convergence\n")
	fmt.Fprintf(&sb, "Delegations: %d\n", s.DelegationCount)
	fmt.Fprintf(&sb, "Evidence: %d items\n", len(s.Evidence))
	fmt.Fprintf(&sb, "Novel findings per delegation: %v\n", s.NovelFindings)
	fmt.Fprintf(&sb, "Cost: $%.4f / $%.2f\n", s.CostConsumed, s.CostBudget)
	if converging {
		sb.WriteString("Status: CONVERGING — ready to synthesize\n")
	} else {
		sb.WriteString("Status: NOT converging\n")
		for _, f := range failures {
			fmt.Fprintf(&sb, "  - %s\n", f)
		}
	}
	return sb.String()
}

func (s *Scratchpad) viewAll() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Analysis: %s\nStrategy: %s\n\n", s.Goal, s.Strategy)
	sb.WriteString(s.viewEvidence())
	sb.WriteString("\n")
	sb.WriteString(s.viewHypotheses())
	sb.WriteString("\n")
	sb.WriteString(s.viewQuestions())
	sb.WriteString("\n")
	sb.WriteString(s.viewConvergence())
	return sb.String()
}

// ── Persistence ─────────────────────────────────────────────────────────────

// PersistableFindings returns the high-priority evidence and confirmed hypotheses
// suitable for storing as persistent cross-session memories.
func (s *Scratchpad) PersistableFindings() []PersistableFinding {
	var findings []PersistableFinding

	// Top evidence by priority (max 10).
	sorted := topN(s.Evidence, 10)
	for _, e := range sorted {
		if e.Priority < 0.4 {
			continue // skip low-priority
		}
		findings = append(findings, PersistableFinding{
			Content:  e.Content,
			Source:   e.Source,
			Priority: e.Priority,
			Kind:     "evidence",
		})
	}

	// Confirmed/rejected hypotheses.
	for _, h := range s.Hypotheses {
		if h.Status == "confirmed" || h.Status == "rejected" {
			findings = append(findings, PersistableFinding{
				Content:  fmt.Sprintf("[%s] %s (confidence: %.2f)", h.Status, h.Statement, h.Confidence),
				Source:   "hypothesis",
				Priority: h.Confidence,
				Kind:     "hypothesis",
			})
		}
	}

	// Strategy outcome.
	if s.Strategy != "" && s.Goal != "" {
		converged, _ := s.ConvergenceCheck()
		outcome := "incomplete"
		if converged {
			outcome = "converged"
		}
		findings = append(findings, PersistableFinding{
			Content:  fmt.Sprintf("Strategy '%s' for '%s': %s (%d delegations, %d evidence)", s.Strategy, truncateStr(s.Goal, 60), outcome, s.DelegationCount, len(s.Evidence)),
			Source:   "strategy",
			Priority: 0.6,
			Kind:     "strategy",
		})
	}

	return findings
}

// PersistableFinding is a single item ready to be stored as persistent memory.
type PersistableFinding struct {
	Content  string
	Source   string
	Priority float64
	Kind     string // evidence, hypothesis, strategy
}

// ConceptsFromGoal extracts keywords from the analysis goal for memory tagging.
func (s *Scratchpad) ConceptsFromGoal() []string {
	var concepts []string
	for _, w := range strings.Fields(strings.ToLower(s.Goal)) {
		if len(w) > 3 {
			concepts = append(concepts, w)
		}
	}
	if s.Strategy != "" {
		concepts = append(concepts, s.Strategy)
	}
	if len(concepts) > 10 {
		concepts = concepts[:10]
	}
	return concepts
}

// ── JSON Serialization ──────────────────────────────────────────────────────

// ToJSON serializes the scratchpad for debugging or persistence.
func (s *Scratchpad) ToJSON() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.MarshalIndent(s, "", "  ")
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// textSimilarity returns a rough similarity score between two strings (0-1).
// Uses Jaccard similarity on word sets.
func textSimilarity(a, b string) float64 {
	wordsA := wordSet(strings.ToLower(a))
	wordsB := wordSet(strings.ToLower(b))
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0
	}
	intersection := 0
	for w := range wordsA {
		if wordsB[w] {
			intersection++
		}
	}
	// union ≥ 1 here: wordsA and wordsB are both non-empty (early return
	// above), and intersection ≤ min(|A|, |B|), so |A| + |B| − intersection
	// ≥ max(|A|, |B|) ≥ 1.
	union := len(wordsA) + len(wordsB) - intersection
	return float64(intersection) / float64(union)
}

func wordSet(s string) map[string]bool {
	set := make(map[string]bool)
	for _, w := range strings.Fields(s) {
		if len(w) > 2 { // skip short words
			set[w] = true
		}
	}
	return set
}

func containsAnyKeyword(text, goal string) bool {
	textLower := strings.ToLower(text)
	for _, w := range strings.Fields(strings.ToLower(goal)) {
		if len(w) > 3 && strings.Contains(textLower, w) {
			return true
		}
	}
	return false
}

func detectContradiction(a, b Evidence) *Contradiction {
	// Simple heuristic: if both are high-relevance and one contains negation
	// of the other's key assertion. This is a rough check.
	if a.Relevance < 0.5 || b.Relevance < 0.5 {
		return nil
	}
	aLower := strings.ToLower(a.Content)
	bLower := strings.ToLower(b.Content)

	// Check for explicit contradiction signals.
	negations := []string{"no ", "not ", "never ", "none ", "without ", "missing ", "lacks "}
	for _, neg := range negations {
		if strings.Contains(aLower, neg) != strings.Contains(bLower, neg) {
			// One has negation, other doesn't — check if they're about the same topic.
			if textSimilarity(a.Content, b.Content) > 0.3 {
				return &Contradiction{
					EvidenceA:   a.ID,
					EvidenceB:   b.ID,
					Description: fmt.Sprintf("'%s' vs '%s'", truncateStr(a.Content, 50), truncateStr(b.Content, 50)),
				}
			}
		}
	}
	return nil
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func hashStr(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:8])
}

// topN returns the top N evidence items by Priority (descending).
func topN(evidence []Evidence, n int) []Evidence {
	if len(evidence) <= n {
		// Return a copy sorted by priority.
		sorted := make([]Evidence, len(evidence))
		copy(sorted, evidence)
		sortByPriority(sorted)
		return sorted
	}
	sorted := make([]Evidence, len(evidence))
	copy(sorted, evidence)
	sortByPriority(sorted)
	return sorted[:n]
}

func sortByPriority(evidence []Evidence) {
	// Simple insertion sort (small N).
	for i := 1; i < len(evidence); i++ {
		for j := i; j > 0 && evidence[j].Priority > evidence[j-1].Priority; j-- {
			evidence[j], evidence[j-1] = evidence[j-1], evidence[j]
		}
	}
}
