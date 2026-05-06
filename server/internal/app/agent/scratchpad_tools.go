package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/commit0-dev/commit0/server/internal/app/memory"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// BuildScratchpadTools creates the scratchpad interaction tools.
// memMgr can be nil — persistence features degrade gracefully.
func BuildScratchpadTools(pad *Scratchpad, graph domain.OpenCodeGraph, memMgr *memory.Manager) []AgentTool {
	return []AgentTool{
		&updateScratchpadTool{pad: pad},
		&readScratchpadTool{pad: pad},
		&checkRedundancyTool{pad: pad},
		&planAnalysisTool{pad: pad, graph: graph, memMgr: memMgr},
		&persistFindingsTool{pad: pad, memMgr: memMgr},
	}
}

// ── update_scratchpad ───────────────────────────────────────────────────────

type updateScratchpadInput struct {
	Evidence       []evidenceInput   `json:"evidence,omitempty"`
	Hypotheses     []hypothesisInput `json:"hypotheses,omitempty"`
	Questions      []questionInput   `json:"questions,omitempty"`
	CloseQuestions []string          `json:"close_questions,omitempty"`
	Strategy       string            `json:"strategy,omitempty"`
}

type evidenceInput struct {
	Content       string  `json:"content"`
	Source        string  `json:"source"`
	Relevance     float64 `json:"relevance"`
	Confidence    float64 `json:"confidence"`
	Novelty       float64 `json:"novelty"`
	Actionability float64 `json:"actionability"`
	Supports      string  `json:"supports,omitempty"`
	Contradicts   string  `json:"contradicts_hyp,omitempty"`
}

type hypothesisInput struct {
	Statement  string  `json:"statement"`
	Confidence float64 `json:"confidence"`
	Status     string  `json:"status"`
}

type questionInput struct {
	Text     string  `json:"text"`
	Priority float64 `json:"priority"`
}

type updateScratchpadOutput struct {
	Status          string `json:"status"`
	TotalEvidence   int    `json:"total_evidence"`
	TotalHypotheses int    `json:"total_hypotheses"`
	OpenQuestions   int    `json:"open_questions"`
	Contradictions  int    `json:"contradictions"`
	NovelThisUpdate int    `json:"novel_this_update"`
}

type updateScratchpadTool struct{ pad *Scratchpad }

func (t *updateScratchpadTool) Def() ToolDef {
	return ToolDef{
		Name: "update_scratchpad",
		Description: "Record evidence, hypotheses, and questions from your investigation. " +
			"Call AFTER every delegation or tool call. " +
			"Evidence scores are validated server-side.",
		InputExample: updateScratchpadInput{},
	}
}

func (t *updateScratchpadTool) Invoke(_ context.Context, argsJSON string) (string, error) {
	var input updateScratchpadInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	if input.Strategy != "" {
		t.pad.Strategy = input.Strategy
	}

	added := 0
	for _, e := range input.Evidence {
		var supports, contradicts []string
		if e.Supports != "" {
			supports = []string{e.Supports}
		}
		if e.Contradicts != "" {
			contradicts = []string{e.Contradicts}
		}
		t.pad.AddEvidence(Evidence{
			Content:       e.Content,
			Source:        e.Source,
			Relevance:     e.Relevance,
			Confidence:    e.Confidence,
			Novelty:       e.Novelty,
			Actionability: e.Actionability,
			Supports:      supports,
			Contradicts:   contradicts,
		})
		added++
	}

	for _, h := range input.Hypotheses {
		found := false
		for i := range t.pad.Hypotheses {
			if t.pad.Hypotheses[i].Statement == h.Statement {
				t.pad.Hypotheses[i].Confidence = clamp(h.Confidence)
				if h.Status != "" {
					t.pad.Hypotheses[i].Status = h.Status
				}
				found = true
				break
			}
		}
		if !found {
			t.pad.Hypotheses = append(t.pad.Hypotheses, Hypothesis{
				ID:         fmt.Sprintf("H%d", len(t.pad.Hypotheses)+1),
				Statement:  h.Statement,
				Confidence: clamp(h.Confidence),
				Status:     "testing",
			})
		}
	}

	for _, q := range input.Questions {
		t.pad.nextQuestionID++
		t.pad.OpenQuestions = append(t.pad.OpenQuestions, Question{
			ID:       fmt.Sprintf("Q%d", t.pad.nextQuestionID),
			Text:     q.Text,
			Priority: clamp(q.Priority),
			Status:   "open",
		})
	}

	for _, qid := range input.CloseQuestions {
		for i := range t.pad.OpenQuestions {
			if t.pad.OpenQuestions[i].ID == qid {
				t.pad.OpenQuestions[i].Status = "answered"
			}
		}
	}

	t.pad.UpdatedSinceDelegation = true
	t.pad.NovelFindings = append(t.pad.NovelFindings, added)

	return marshalJSON(updateScratchpadOutput{
		Status:          "updated",
		TotalEvidence:   len(t.pad.Evidence),
		TotalHypotheses: len(t.pad.Hypotheses),
		OpenQuestions:   countOpen(t.pad.OpenQuestions),
		Contradictions:  len(t.pad.Contradictions),
		NovelThisUpdate: added,
	})
}

// ── read_scratchpad ─────────────────────────────────────────────────────────

type readScratchpadInput struct {
	Section string `json:"section"`
}

type readScratchpadTool struct{ pad *Scratchpad }

func (t *readScratchpadTool) Def() ToolDef {
	return ToolDef{
		Name: "read_scratchpad",
		Description: "Read current analysis state (token-budgeted). " +
			"Sections: all, evidence, questions, hypotheses, action_log, convergence",
		InputExample: readScratchpadInput{},
	}
}

func (t *readScratchpadTool) Invoke(_ context.Context, argsJSON string) (string, error) {
	var input readScratchpadInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	section := input.Section
	if section == "" {
		section = "all"
	}
	return t.pad.BudgetedView(section), nil
}

// ── check_redundancy ────────────────────────────────────────────────────────

type checkRedundancyInput struct {
	ProposedTool string `json:"proposed_tool"`
	ProposedArgs string `json:"proposed_args"`
}

type checkRedundancyOutput struct {
	Redundant        bool     `json:"redundant"`
	SimilarActions   []string `json:"similar_actions,omitempty"`
	ExistingEvidence []string `json:"existing_evidence,omitempty"`
	Recommendation   string   `json:"recommendation"`
}

type checkRedundancyTool struct{ pad *Scratchpad }

func (t *checkRedundancyTool) Def() ToolDef {
	return ToolDef{
		Name: "check_redundancy",
		Description: "Check if a proposed action has already been tried or if the answer " +
			"is already in evidence. Call BEFORE delegating or using a tool.",
		InputExample: checkRedundancyInput{},
	}
}

func (t *checkRedundancyTool) Invoke(_ context.Context, argsJSON string) (string, error) {
	var input checkRedundancyInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	redundant, similar := t.pad.AlreadyTried(input.ProposedTool, input.ProposedArgs)

	var existingEvidence []string
	for _, e := range t.pad.Evidence {
		if textSimilarity(e.Content, input.ProposedArgs) > 0.4 {
			existingEvidence = append(existingEvidence, fmt.Sprintf("[%s] %s", e.ID, truncateStr(e.Content, 60)))
		}
	}

	out := checkRedundancyOutput{
		Redundant:        redundant,
		ExistingEvidence: existingEvidence,
	}
	if redundant {
		var similarStrs []string
		for _, a := range similar {
			similarStrs = append(similarStrs, fmt.Sprintf("%s(%s) → %d results", a.Tool, truncateStr(a.Args, 40), a.ResultSize))
		}
		out.SimilarActions = similarStrs
		out.Recommendation = "Skip — try a different angle or use existing evidence."
	} else {
		out.Recommendation = "Proceed — not tried before."
	}
	return marshalJSON(out)
}

// ── plan_analysis ───────────────────────────────────────────────────────────

type planAnalysisInput struct {
	Goal string `json:"goal"`
}

type planAnalysisOutput struct {
	Repo           string   `json:"repo"`
	Goal           string   `json:"goal"`
	Path           string   `json:"path,omitempty"`
	Languages      []string `json:"languages,omitempty"`
	NodeCount      int      `json:"node_count,omitempty"`
	EndpointCount  int      `json:"endpoint_count,omitempty"`
	PriorKnowledge string   `json:"prior_knowledge,omitempty"`
	Suggestion     string   `json:"suggestion"`
}

type planAnalysisTool struct {
	pad    *Scratchpad
	graph  domain.OpenCodeGraph
	memMgr *memory.Manager
}

func (t *planAnalysisTool) Def() ToolDef {
	return ToolDef{
		Name: "plan_analysis",
		Description: "Get repo context before starting investigation. " +
			"Returns node count, endpoint count, languages. Call FIRST.",
		InputExample: planAnalysisInput{},
	}
}

func (t *planAnalysisTool) Invoke(ctx context.Context, argsJSON string) (string, error) {
	var input planAnalysisInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	if input.Goal != "" {
		t.pad.Goal = input.Goal
	}

	repoSlug := RepoSlugFrom(ctx)
	out := planAnalysisOutput{
		Repo:       repoSlug,
		Goal:       t.pad.Goal,
		Suggestion: "Start with delegate(search, ...) to discover relevant entities, then delegate(trace, ...) to map structure.",
	}

	if t.graph != nil && repoSlug != "" {
		bgCtx := context.Background()

		if repos, err := t.graph.ListRepos(bgCtx); err == nil {
			for _, r := range repos {
				if r.Slug == repoSlug {
					out.Path = r.Path
					out.Languages = r.Languages
					break
				}
			}
		}

		if ids, err := t.graph.ListNodes(bgCtx, repoSlug, domain.ListOpts{IDsOnly: true}); err == nil {
			out.NodeCount = len(ids)
		}

		if routes, err := t.graph.ListEdges(bgCtx, repoSlug, []string{"route"}); err == nil {
			out.EndpointCount = len(routes)
		}
	}

	if t.memMgr != nil && repoSlug != "" && t.pad.Goal != "" {
		memCtx := context.Background()
		prior, err := t.memMgr.BuildContext(memCtx, "", repoSlug, t.pad.Goal)
		if err == nil && prior != "" {
			out.PriorKnowledge = prior
		}
	}

	return marshalJSON(out)
}

// ── persist_findings ────────────────────────────────────────────────────────

type persistFindingsInput struct {
	Summary string `json:"summary"`
}

type persistFindingsOutput struct {
	Stored  int    `json:"stored"`
	Message string `json:"message"`
}

type persistFindingsTool struct {
	pad    *Scratchpad
	memMgr *memory.Manager
}

func (t *persistFindingsTool) Def() ToolDef {
	return ToolDef{
		Name: "persist_findings",
		Description: "Store key findings as persistent memories for future investigations. " +
			"Call AFTER write_report to save what you learned about this codebase.",
		InputExample: persistFindingsInput{},
	}
}

func (t *persistFindingsTool) Invoke(ctx context.Context, argsJSON string) (string, error) {
	var input persistFindingsInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	if t.memMgr == nil {
		return marshalJSON(persistFindingsOutput{Message: "Memory persistence not available."})
	}

	repoSlug := RepoSlugFrom(ctx)
	findings := t.pad.PersistableFindings()
	concepts := t.pad.ConceptsFromGoal()

	stored := 0
	bgCtx := context.Background()
	for _, f := range findings {
		content := f.Content
		if input.Summary != "" && f.Kind == "strategy" {
			content = fmt.Sprintf("%s | Summary: %s", content, input.Summary)
		}
		if err := t.memMgr.StorePersistentMemory(bgCtx, repoSlug, content, concepts); err != nil {
			continue
		}
		stored++
	}

	return marshalJSON(persistFindingsOutput{
		Stored:  stored,
		Message: fmt.Sprintf("Stored %d findings as persistent memories for repo '%s'.", stored, repoSlug),
	})
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func countOpen(questions []Question) int {
	n := 0
	for _, q := range questions {
		if q.Status == "open" {
			n++
		}
	}
	return n
}
