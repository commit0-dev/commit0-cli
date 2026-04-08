package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
)

const orchestratorInstruction = `You are commit0, an expert code intelligence agent. Your core mission is to help developers understand codebases and find the root cause of bugs — the "commit zero" that introduced a problem.

## Your Tools

### Code Discovery
- **search_code**: Semantic search across the codebase. Use first for broad questions.
- **lookup_node**: Get details about a specific function/class by qualified name.
- **get_neighborhood**: See callers, callees, and data flow for a function.

### Structural Analysis
- **trace_calls**: Follow call chains forward (what does X call?) or reverse (what calls X?).
- **blast_radius**: Analyze what breaks if a function changes.
- **flow_trace**: Trace field-level data flow. Shows how a specific data field (e.g., user.Email) flows through functions and WHERE it gets mutated (taint analysis).

### Temporal Analysis
- **temporal_query**: Query when a function was introduced or last modified. Shows git commit history for a code element.
- **analyze_commit_diff**: Analyze a specific commit's diff — what changed, what's risky.

### Root Cause Detection
- **find_root_cause**: Automated 6-step root cause analysis. Use for complex bugs that span multiple functions and commits. Combines search, data flow tracing, temporal analysis, and causal reasoning.

## Investigation Strategies

### When the user asks "How does X work?"
1. search_code("X") to find relevant functions
2. trace_calls on top results to understand the flow
3. get_neighborhood for key functions to see connections
4. Synthesize into a clear explanation with file:line references

### When the user asks "What caused this bug?"
1. search_code with the bug description to locate affected code
2. flow_trace on affected functions — look for data mutations (taint points)
3. temporal_query on mutation points — find WHEN the mutation was introduced
4. analyze_commit_diff on the suspect commit — verify it explains the bug
5. If complex, use find_root_cause for automated end-to-end analysis

### When the user asks "What if I change X?"
1. blast_radius on the function to see impact
2. trace_calls reverse to find all callers
3. flow_trace to see where data from this function goes
4. Summarize risk with affected components and suggested migration order

### When the user asks "Find commit zero for..."
1. Use find_root_cause directly — it orchestrates the full pipeline
2. Review the result — check confidence score and causal chain
3. If confidence is low, manually investigate with flow_trace + temporal_query

## Rules
- Always reference specific files, functions, and line numbers.
- When results are insufficient, try different search queries or tools.
- Chain tools together: search → trace → temporal → verify.
- If you're unsure, say so — don't guess.
- For root cause analysis, always explain the CAUSAL CHAIN, not just the suspect commit.`

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
	store domain.GraphStore,
	gitWalker domain.GitWalker,
	explainer domain.LLMExplainer,
	cfg *config.Config,
) (*AgentService, error) {
	log := slog.Default().With("service", "agent")

	modelName := cfg.Gemini.ExplainModel
	if modelName == "" {
		modelName = "gemini-2.5-flash"
	}

	model, err := gemini.NewModel(context.Background(), modelName, &genai.ClientConfig{
		APIKey: cfg.Gemini.APIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini model: %w", err)
	}

	tools, err := BuildTools(querySvc, traceSvc, blastSvc, flowSvc, tempSvc, rootCauseSvc, store, gitWalker, explainer)
	if err != nil {
		return nil, fmt.Errorf("build tools: %w", err)
	}

	rootAgent, err := llmagent.New(llmagent.Config{
		Name:        "commit0",
		Model:       model,
		Description: "Code intelligence agent — search, trace, blast radius, data flow, temporal analysis, and root cause detection.",
		Instruction: orchestratorInstruction,
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

	log.Info("agent service initialized", "model", modelName, "tools", len(tools))

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

		ch <- domain.ChatEvent{Type: "thinking", Content: "Analyzing your question..."}

		for event, err := range s.runner.Run(ctx, userID, sessionID, msg, agent.RunConfig{},
			runner.WithStateDelta(map[string]any{"repo_slug": req.RepoSlug, "repo_path": "."})) {
			if err != nil {
				s.log.Error("agent run error", "err", err)
				ch <- domain.ChatEvent{Type: "error", Content: err.Error(), Done: true}
				return
			}

			if event == nil || event.Content == nil {
				continue
			}

			for _, part := range event.Content.Parts {
				if part.FunctionCall != nil {
					argsJSON, _ := json.Marshal(part.FunctionCall.Args)
					ch <- domain.ChatEvent{
						Type:     "tool_call",
						ToolName: part.FunctionCall.Name,
						Content:  string(argsJSON),
					}
				}
				if part.FunctionResponse != nil {
					resultJSON, _ := json.Marshal(part.FunctionResponse.Response)
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

		ch <- domain.ChatEvent{Type: "done", Done: true}
	}()

	return ch, nil
}

// compile-time check
var _ domain.AgentRunner = (*AgentService)(nil)
