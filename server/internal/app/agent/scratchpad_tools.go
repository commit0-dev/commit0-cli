package agent

import (
	"context"
	"fmt"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/commit0-dev/commit0/server/internal/app/memory"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// BuildScratchpadTools creates the scratchpad interaction tools.
// memMgr can be nil — persistence features degrade gracefully.
func BuildScratchpadTools(pad *Scratchpad, store domain.GraphStore, memMgr *memory.Manager) ([]tool.Tool, error) {
	var tools []tool.Tool

	t, err := newUpdateScratchpadTool(pad)
	if err != nil {
		return nil, fmt.Errorf("update_scratchpad: %w", err)
	}
	tools = append(tools, t)

	t, err = newReadScratchpadTool(pad)
	if err != nil {
		return nil, fmt.Errorf("read_scratchpad: %w", err)
	}
	tools = append(tools, t)

	t, err = newCheckRedundancyTool(pad)
	if err != nil {
		return nil, fmt.Errorf("check_redundancy: %w", err)
	}
	tools = append(tools, t)

	t, err = newPlanAnalysisTool(pad, store, memMgr)
	if err != nil {
		return nil, fmt.Errorf("plan_analysis: %w", err)
	}
	tools = append(tools, t)

	t, err = newPersistFindingsTool(pad, memMgr)
	if err != nil {
		return nil, fmt.Errorf("persist_findings: %w", err)
	}
	tools = append(tools, t)

	return tools, nil
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
	Status           string `json:"status"`
	TotalEvidence    int    `json:"total_evidence"`
	TotalHypotheses  int    `json:"total_hypotheses"`
	OpenQuestions    int    `json:"open_questions"`
	Contradictions   int    `json:"contradictions"`
	NovelThisUpdate  int    `json:"novel_this_update"`
}

func newUpdateScratchpadTool(pad *Scratchpad) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "update_scratchpad",
		Description: "Record evidence, hypotheses, and questions from your investigation. " +
			"Call AFTER every delegation or tool call. " +
			"Evidence scores are validated server-side.",
	}, func(ctx tool.Context, input updateScratchpadInput) (updateScratchpadOutput, error) {
		if input.Strategy != "" {
			pad.Strategy = input.Strategy
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
			pad.AddEvidence(Evidence{
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
			for i := range pad.Hypotheses {
				if pad.Hypotheses[i].Statement == h.Statement {
					pad.Hypotheses[i].Confidence = clamp(h.Confidence)
					if h.Status != "" {
						pad.Hypotheses[i].Status = h.Status
					}
					found = true
					break
				}
			}
			if !found {
				pad.Hypotheses = append(pad.Hypotheses, Hypothesis{
					ID:         fmt.Sprintf("H%d", len(pad.Hypotheses)+1),
					Statement:  h.Statement,
					Confidence: clamp(h.Confidence),
					Status:     "testing",
				})
			}
		}

		for _, q := range input.Questions {
			pad.nextQuestionID++
			pad.OpenQuestions = append(pad.OpenQuestions, Question{
				ID:       fmt.Sprintf("Q%d", pad.nextQuestionID),
				Text:     q.Text,
				Priority: clamp(q.Priority),
				Status:   "open",
			})
		}

		for _, qid := range input.CloseQuestions {
			for i := range pad.OpenQuestions {
				if pad.OpenQuestions[i].ID == qid {
					pad.OpenQuestions[i].Status = "answered"
				}
			}
		}

		pad.UpdatedSinceDelegation = true
		pad.NovelFindings = append(pad.NovelFindings, added)

		return updateScratchpadOutput{
			Status:          "updated",
			TotalEvidence:   len(pad.Evidence),
			TotalHypotheses: len(pad.Hypotheses),
			OpenQuestions:   countOpen(pad.OpenQuestions),
			Contradictions:  len(pad.Contradictions),
			NovelThisUpdate: added,
		}, nil
	})
}

// ── read_scratchpad ─────────────────────────────────────────────────────────

type readScratchpadInput struct {
	Section string `json:"section"`
}

func newReadScratchpadTool(pad *Scratchpad) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "read_scratchpad",
		Description: "Read current analysis state (token-budgeted). " +
			"Sections: all, evidence, questions, hypotheses, action_log, convergence",
	}, func(ctx tool.Context, input readScratchpadInput) (string, error) {
		section := input.Section
		if section == "" {
			section = "all"
		}
		return pad.BudgetedView(section), nil
	})
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

func newCheckRedundancyTool(pad *Scratchpad) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "check_redundancy",
		Description: "Check if a proposed action has already been tried or if the answer " +
			"is already in evidence. Call BEFORE delegating or using a tool.",
	}, func(ctx tool.Context, input checkRedundancyInput) (checkRedundancyOutput, error) {
		redundant, similar := pad.AlreadyTried(input.ProposedTool, input.ProposedArgs)

		var existingEvidence []string
		for _, e := range pad.Evidence {
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
		return out, nil
	})
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

func newPlanAnalysisTool(pad *Scratchpad, store domain.GraphStore, memMgr *memory.Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "plan_analysis",
		Description: "Get repo context before starting investigation. " +
			"Returns node count, endpoint count, languages. Call FIRST.",
	}, func(ctx tool.Context, input planAnalysisInput) (planAnalysisOutput, error) {
		if input.Goal != "" {
			pad.Goal = input.Goal
		}

		repoSlug := getRepoSlug(ctx)
		out := planAnalysisOutput{
			Repo:       repoSlug,
			Goal:       pad.Goal,
			Suggestion: "Start with delegate(search, ...) to discover relevant entities, then delegate(trace, ...) to map structure.",
		}

		if store != nil && repoSlug != "" {
			bgCtx := context.Background()

			if repos, err := store.ListRepos(bgCtx); err == nil {
				for _, r := range repos {
					if r.Slug == repoSlug {
						out.Path = r.Path
						out.Languages = r.Languages
						break
					}
				}
			}

			if ids, err := store.ListNodeIDs(bgCtx, repoSlug); err == nil {
				out.NodeCount = len(ids)
			}

			if routes, err := store.ListRoutes(bgCtx, repoSlug); err == nil {
				out.EndpointCount = len(routes)
			}
		}

		out.Suggestion = "Start with delegate(search, ...) to discover relevant entities, then delegate(trace, ...) to map structure."

		// Load prior knowledge from persistent memory.
		if memMgr != nil && repoSlug != "" && pad.Goal != "" {
			memCtx := context.Background()
			prior, err := memMgr.BuildContext(memCtx, "", repoSlug, pad.Goal)
			if err == nil && prior != "" {
				out.PriorKnowledge = prior
			}
		}

		return out, nil
	})
}

// ── persist_findings ────────────────────────────────────────────────────────

type persistFindingsInput struct {
	Summary string `json:"summary"` // optional summary of the analysis
}

type persistFindingsOutput struct {
	Stored  int    `json:"stored"`
	Message string `json:"message"`
}

func newPersistFindingsTool(pad *Scratchpad, memMgr *memory.Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "persist_findings",
		Description: "Store key findings as persistent memories for future investigations. " +
			"Call AFTER write_report to save what you learned about this codebase.",
	}, func(ctx tool.Context, input persistFindingsInput) (persistFindingsOutput, error) {
		if memMgr == nil {
			return persistFindingsOutput{Message: "Memory persistence not available."}, nil
		}

		repoSlug := getRepoSlug(ctx)
		findings := pad.PersistableFindings()
		concepts := pad.ConceptsFromGoal()

		stored := 0
		bgCtx := context.Background()
		for _, f := range findings {
			content := f.Content
			if input.Summary != "" && f.Kind == "strategy" {
				content = fmt.Sprintf("%s | Summary: %s", content, input.Summary)
			}
			if err := memMgr.StorePersistentMemory(bgCtx, repoSlug, content, concepts); err != nil {
				continue
			}
			stored++
		}

		return persistFindingsOutput{
			Stored:  stored,
			Message: fmt.Sprintf("Stored %d findings as persistent memories for repo '%s'.", stored, repoSlug),
		}, nil
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
