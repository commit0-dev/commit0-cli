package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/app/memory"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// AgentService provides agentic code intelligence conversations.
type AgentService struct {
	runnerPort AgentRunnerPort
	config     AgentConfig
	log        *slog.Logger
}

// NewAgentService creates the agent orchestrator.
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
	runnerPort AgentRunnerPort,
	subRunnerFactory SubRunnerFactory,
) (*AgentService, error) {
	log := slog.Default().With("service", "agent")

	// Build all analysis tools.
	tools := BuildTools(querySvc, traceSvc, blastSvc, flowSvc, tempSvc, rootCauseSvc, graph, gitWalker, explainer)

	// Create scratchpad for memory management.
	pad := NewScratchpad("")

	// Build scratchpad tools.
	scratchpadTools := BuildScratchpadTools(pad, graph, memMgr)
	tools = append(tools, scratchpadTools...)

	// Build delegate tool.
	delegateTool := BuildDelegateTool(
		querySvc, traceSvc, blastSvc, flowSvc, tempSvc, rootCauseSvc,
		graph, gitWalker, explainer, cfg, pad, subRunnerFactory,
	)
	tools = append(tools, delegateTool)

	agentCfg := AgentConfig{
		Name:        "commit0",
		Description: "Analyst agent — autonomous code investigation orchestrator with memory, evidence ranking, and sub-agent delegation.",
		Instruction: analystInstruction,
		Tools:       tools,
	}

	log.Info("agent service initialized", "tools", len(tools))

	return &AgentService{runnerPort: runnerPort, config: agentCfg, log: log}, nil
}

// Chat processes a user message and streams agent events.
func (s *AgentService) Chat(ctx context.Context, req domain.ChatRequest) (<-chan domain.ChatEvent, error) {
	ch := make(chan domain.ChatEvent, 32)

	state := map[string]any{
		"repo_slug": req.RepoSlug,
		"repo_path": ".",
	}

	go func() {
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("agent panic recovered", "panic", r)
				ch <- domain.ChatEvent{Type: "error", Content: fmt.Sprintf("agent panic: %v", r), Done: true}
			}
		}()

		s.log.Info("starting agent chat", "repo", req.RepoSlug)
		ch <- domain.ChatEvent{Type: "thinking", Content: "Analyzing your question..."}

		events, err := s.runnerPort.Run(ctx, s.config, req.Message, state)
		if err != nil {
			s.log.Error("agent run error", "err", err)
			ch <- domain.ChatEvent{Type: "error", Content: err.Error(), Done: true}
			return
		}

		for event := range events {
			switch event.Type {
			case "tool_call":
				s.log.Info("agent tool_call", "tool", event.ToolName)
				ch <- domain.ChatEvent{
					Type:     "tool_call",
					ToolName: event.ToolName,
					Content:  event.Content,
				}
			case "tool_result":
				s.log.Info("agent tool_result", "tool", event.ToolName)
				ch <- domain.ChatEvent{
					Type:     "tool_result",
					ToolName: event.ToolName,
					Content:  event.Content,
				}
			case "message":
				ch <- domain.ChatEvent{
					Type:    "message",
					Content: event.Content,
				}
			case "usage":
				if event.Usage != nil {
					ch <- domain.ChatEvent{
						Type: "usage",
						Content: fmt.Sprintf(`{"prompt_tokens":%d,"output_tokens":%d,"total_tokens":%d}`,
							event.Usage.PromptTokens,
							event.Usage.CompletionTokens,
							event.Usage.TotalTokens),
					}
				}
			case "error":
				s.log.Error("agent run error", "err", event.Content)
				ch <- domain.ChatEvent{Type: "error", Content: event.Content, Done: true}
				return
			}
		}

		s.log.Info("agent run complete")
		ch <- domain.ChatEvent{Type: "done", Done: true}
	}()

	return ch, nil
}

// compile-time check.
var _ domain.AgentRunner = (*AgentService)(nil)
