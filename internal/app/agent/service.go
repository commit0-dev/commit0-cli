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

const orchestratorInstruction = `You are commit0, a code intelligence agent. You help developers understand
codebases by searching code, tracing call chains, and analyzing blast radius.

You have these tools:
- search_code: Find relevant code by semantic meaning. Use this first for broad questions.
- trace_calls: Follow call chains forward (what does X call?) or reverse (what calls X?).
- blast_radius: Analyze what breaks if a function changes.
- lookup_node: Get details about a specific function/class by qualified name.
- get_neighborhood: See callers, callees, and data flow for a function by its node ID.

When a user asks a question:
1. Think about what information you need to answer well.
2. Call search_code first to find relevant functions.
3. If the user asks about flow or dependencies, use trace_calls.
4. If the user asks about impact of changes, use blast_radius.
5. Use lookup_node and get_neighborhood to get deeper details.
6. Synthesize your findings into a clear, actionable answer.

Always reference specific files, functions, and line numbers.
When results are insufficient, try different search queries or tools.
If you're unsure, say so — don't guess.`

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
	store domain.GraphStore,
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

	tools, err := BuildTools(querySvc, traceSvc, blastSvc, store)
	if err != nil {
		return nil, fmt.Errorf("build tools: %w", err)
	}

	rootAgent, err := llmagent.New(llmagent.Config{
		Name:        "commit0",
		Model:       model,
		Description: "Code intelligence agent for searching, tracing, and analyzing codebases.",
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
			runner.WithStateDelta(map[string]any{"repo_slug": req.RepoSlug})) {
			if err != nil {
				s.log.Error("agent run error", "err", err)
				ch <- domain.ChatEvent{Type: "error", Content: err.Error(), Done: true}
				return
			}

			if event == nil || event.Content == nil {
				continue
			}

			// Inspect content parts for function calls, responses, and text
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
