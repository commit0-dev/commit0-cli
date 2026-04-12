package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"

	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/app/memory"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

const orchestratorInstruction = `You are commit0, a principal software engineer performing deep code analysis. You have access to a code graph database and must reason about code like a human expert reviewing an unfamiliar codebase.

## Core Principle

YOU analyze the code — never delegate analysis to a pre-built explanation. Tool results give you raw data: function bodies, call chains, data flow paths. Your job is to read the code, understand the architecture, and explain it with precision.

## How to Investigate

You MUST use at least 3 DIFFERENT tool types per investigation. Repeating search_code 3 times does NOT count.

### Step 1: Discover (search_code)
Search with 1-2 queries to find entry points. Read the BODY and SIGNATURE of each result.

### Step 2: Map Structure (REQUIRED — trace_calls, get_neighborhood)
This step is MANDATORY. For each key function from Step 1:
- trace_calls forward on the entry point to see the full execution flow
- trace_calls reverse on internal functions to see who triggers them
- get_neighborhood on junction points to see callers, callees, and data flow connections

Do NOT skip this step. Without structural tools, your answer will be generic.

### Step 3: Deep Dive (lookup_node, flow_trace)
For the 2-3 most important functions in the flow, call lookup_node to read their full body.
If the question involves data transformation, use flow_trace.

### Step 4: Synthesize via write_report
Combine findings into a technical analysis. Every claim must reference specific tool results — never use general knowledge about frameworks or libraries. If you didn't find it in a tool result, don't claim it.

## Investigation Depth by Question Type

### "How does X work?" (minimum 5 tool calls)
1. search_code to find entry points (1-2 calls)
2. trace_calls forward on the entry point — get the ACTUAL call chain with file:line (REQUIRED)
3. get_neighborhood on key functions in the chain — see callers, callees, data flow (REQUIRED)
4. lookup_node on 2-3 critical functions — read their actual code bodies (REQUIRED)
5. write_report with sections built from real tool results, not framework knowledge

### "What caused this bug?" / "Find commit zero"
1. search_code to locate the affected area
2. flow_trace to trace data mutations — find WHERE data gets corrupted
3. temporal_query on mutation points — find WHEN each was introduced
4. analyze_commit_diff on suspect commits — read the actual diff
5. find_root_cause for automated end-to-end analysis if manual investigation is complex
6. Explain the CAUSAL CHAIN: commit X changed function Y, which mutated field Z, which broke downstream consumer W

### "What if I change X?"
1. blast_radius to see all transitive dependents
2. trace_calls reverse to find all callers
3. flow_trace to see data propagation from this function
4. Identify high-risk dependents and suggest migration order

## Presenting Results

ALWAYS call write_report as your FINAL action to present findings. NEVER output raw text as your answer.

Structure your report with:
- A clear title describing what was analyzed
- A summary paragraph (2-3 sentences)
- Sections with headings for each aspect of the analysis
- Code snippets in sections where you show actual code
- Call chains as ordered lists showing the flow
- File references for every claim

Example write_report call:
{
  "title": "Event-Driven Signal Collection Flow",
  "summary": "The operator uses context-based cancellation propagation...",
  "sections": [
    {"heading": "Entry Point", "content": "The flow starts in main()...", "code": "func main() {\n  ctx := ctrl.SetupSignalHandler()\n  mgr.Start(ctx)\n}", "code_lang": "go", "references": ["operator/cmd/main.go:45"]},
    {"heading": "Call Chain", "call_chain": ["main (cmd/main.go:45)", "rootAction (cmd/root.go:12)", "manager.Start (pkg/manager.go:89)"]},
    {"heading": "Architecture", "content": "This follows the Context Propagation pattern...", "references": ["operator/cmd/main.go:29", "operator/cmd/main.go:45"]}
  ]
}

## Quality Standards

- NEVER say "likely", "probably", or "assumed" — use lookup_node or trace_calls to verify
- NEVER describe framework behavior from general knowledge — only describe what the CODE does based on tool results
- Minimum 5 tool calls using at least 3 different tool types for any non-trivial question
- If search results don't match the question, use trace_calls on known entry points (e.g. main) instead of searching more
- Every section in write_report must be grounded in a specific tool result`

// AgentService provides agentic code intelligence conversations.
type AgentService struct {
	runner *runner.Runner
	log    *slog.Logger
}

// NewAgentService creates the ADK-powered agent orchestrator.
func NewAgentService(
	querySvc *app.QueryService,
	traceSvc *app.TraceService,
	blastSvc *app.BlastService,
	flowSvc *app.FieldFlowService,
	tempSvc *app.TemporalService,
	rootCauseSvc *app.RootCauseAnalysisService,
	graph domain.OpenCodeGraph,
	gitWalker domain.GitWalker,
	explainer domain.LLMExplainer,
	cfg *config.Config,
	memMgr *memory.Manager,
	llmModel adkmodel.LLM, // optional: if nil, creates Gemini model from config
) (*AgentService, error) {
	log := slog.Default().With("service", "agent")

	model := llmModel
	if model == nil {
		// Fallback: create Gemini model from config.
		modelName := cfg.Gemini.ExplainModel
		if modelName == "" {
			modelName = "gemini-2.5-flash"
		}
		var err error
		model, err = gemini.NewModel(context.Background(), modelName, &genai.ClientConfig{
			APIKey: cfg.Gemini.APIKey,
		})
		if err != nil {
			return nil, fmt.Errorf("create gemini model: %w", err)
		}
	}

	// Build all analysis tools (shared by orchestrator and sub-agents).
	tools, err := BuildTools(querySvc, traceSvc, blastSvc, flowSvc, tempSvc, rootCauseSvc, graph, gitWalker, explainer)
	if err != nil {
		return nil, fmt.Errorf("build tools: %w", err)
	}

	// Create scratchpad for memory management.
	pad := NewScratchpad("")

	// Build scratchpad tools (update, read, check_redundancy, plan_analysis).
	scratchpadTools, err := BuildScratchpadTools(pad, graph, memMgr)
	if err != nil {
		return nil, fmt.Errorf("build scratchpad tools: %w", err)
	}
	tools = append(tools, scratchpadTools...)

	// Build delegate tool (sub-agent spawning with handoff contracts).
	// Wrap the model in a factory so delegate.go never imports concrete adapters.
	modelFactory := func() (adkmodel.LLM, error) { return model, nil }
	delegateTool, err := BuildDelegateTool(
		querySvc, traceSvc, blastSvc, flowSvc, tempSvc, rootCauseSvc,
		graph, gitWalker, explainer, cfg, pad, modelFactory,
	)
	if err != nil {
		return nil, fmt.Errorf("build delegate tool: %w", err)
	}
	tools = append(tools, delegateTool)

	rootAgent, err := llmagent.New(llmagent.Config{
		Name:        "commit0",
		Model:       model,
		Description: "Analyst agent — autonomous code investigation orchestrator with memory, evidence ranking, and sub-agent delegation.",
		Instruction: analystInstruction,
		Tools:       tools,
	})
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	sessionSvc := session.InMemoryService()

	r, err := runner.New(runner.Config{
		AppName:           "commit0",
		Agent:             rootAgent,
		SessionService:    sessionSvc,
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create runner: %w", err)
	}

	log.Info("agent service initialized", "model", model.Name(), "tools", len(tools))

	return &AgentService{runner: r, log: log}, nil
}

// Chat processes a user message and streams agent events.
func (s *AgentService) Chat(ctx context.Context, req domain.ChatRequest) (<-chan domain.ChatEvent, error) {
	ch := make(chan domain.ChatEvent, 32)

	userID := req.UserID
	if userID == "" {
		userID = "default"
	}
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = "default-" + req.RepoSlug
	}

	msg := genai.NewContentFromText(req.Message, genai.RoleUser)

	go func() {
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("agent panic recovered", "panic", r)
				ch <- domain.ChatEvent{Type: "error", Content: fmt.Sprintf("agent panic: %v", r), Done: true}
			}
		}()

		s.log.Info("starting agent chat", "session", sessionID, "repo", req.RepoSlug)
		ch <- domain.ChatEvent{Type: "thinking", Content: "Analyzing your question..."}

		s.log.Info("starting runner.Run")
		for event, err := range s.runner.Run(ctx, userID, sessionID, msg, agent.RunConfig{},
			runner.WithStateDelta(map[string]any{"repo_slug": req.RepoSlug, "repo_path": "."})) {
			if err != nil {
				s.log.Error("agent run error", "err", err)
				ch <- domain.ChatEvent{Type: "error", Content: err.Error(), Done: true}
				return
			}

			if event == nil {
				continue
			}

			// Emit usage metadata for cost tracking.
			if event.UsageMetadata != nil {
				ch <- domain.ChatEvent{
					Type: "usage",
					Content: fmt.Sprintf(`{"prompt_tokens":%d,"output_tokens":%d,"total_tokens":%d}`,
						event.UsageMetadata.PromptTokenCount,
						event.UsageMetadata.CandidatesTokenCount,
						event.UsageMetadata.TotalTokenCount),
				}
			}

			if event.Content == nil {
				continue
			}

			for _, part := range event.Content.Parts {
				if part.FunctionCall != nil {
					s.log.Info("agent tool_call", "tool", part.FunctionCall.Name)
					argsJSON, err := json.Marshal(part.FunctionCall.Args)
					if err != nil {
						s.log.Warn("marshal tool_call args", "tool", part.FunctionCall.Name, "err", err)
						argsJSON = []byte(`{"error":"marshal failed"}`)
					}
					ch <- domain.ChatEvent{
						Type:     "tool_call",
						ToolName: part.FunctionCall.Name,
						Content:  string(argsJSON),
					}
				}
				if part.FunctionResponse != nil {
					s.log.Info("agent tool_result", "tool", part.FunctionResponse.Name)
					resultJSON, err := json.Marshal(part.FunctionResponse.Response)
					if err != nil {
						s.log.Warn("marshal tool_result", "tool", part.FunctionResponse.Name, "err", err)
						resultJSON = []byte(`{"error":"marshal failed"}`)
					}
					ch <- domain.ChatEvent{
						Type:     "tool_result",
						ToolName: part.FunctionResponse.Name,
						Content:  string(resultJSON),
					}
				}
				if part.Text != "" {
					ch <- domain.ChatEvent{
						Type:    "message",
						Content: part.Text,
					}
				}
			}
		}

		s.log.Info("agent run complete")
		ch <- domain.ChatEvent{Type: "done", Done: true}
	}()

	return ch, nil
}

// compile-time check
var _ domain.AgentRunner = (*AgentService)(nil)
