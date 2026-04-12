// Package eino implements the AgentRunnerPort using CloudWeGo Eino.
package eino

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"

	agent "github.com/commit0-dev/commit0/server/internal/app/agent"
)

// Runner implements agent.AgentRunnerPort using Eino's ChatModelAgent + Runner.
type Runner struct {
	chatModel model.ToolCallingChatModel
	log       *slog.Logger
}

// NewRunner creates an Eino-backed agent runner.
func NewRunner(chatModel model.ToolCallingChatModel) *Runner {
	return &Runner{
		chatModel: chatModel,
		log:       slog.Default().With("adapter", "eino-runner"),
	}
}

// Run implements agent.AgentRunnerPort. It creates a ChatModelAgent from the
// config, wraps our AgentTools as Eino InvokableTools, and streams events.
func (r *Runner) Run(ctx context.Context, config agent.AgentConfig, userMessage string,
	state map[string]any) (<-chan agent.RunnerEvent, error) {

	// Convert our AgentTools → Eino InvokableTools.
	einoTools := make([]tool.BaseTool, 0, len(config.Tools))
	for _, t := range config.Tools {
		einoTools = append(einoTools, newToolAdapter(t))
	}

	// Build Eino ChatModelAgent.
	einoAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        config.Name,
		Description: config.Description,
		Instruction: config.Instruction,
		Model:       r.chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: einoTools,
			},
		},
		MaxIterations: 30,
	})
	if err != nil {
		return nil, fmt.Errorf("create eino agent: %w", err)
	}

	// Create Eino Runner with streaming enabled.
	// Streaming is required for Unsloth Studio compatibility: its API defaults
	// to SSE when the "stream" field is omitted (go-openai omits false values),
	// causing JSON parse errors. Using streaming mode makes the agent call
	// ChatModel.Stream() which properly handles SSE responses.
	einoRunner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           einoAgent,
		EnableStreaming: true,
	})

	// Inject state into context for tools.
	toolCtx := ctx
	if slug, ok := state["repo_slug"].(string); ok {
		toolCtx = agent.WithRepoSlug(toolCtx, slug)
	}
	if path, ok := state["repo_path"].(string); ok {
		toolCtx = agent.WithRepoPath(toolCtx, path)
	}

	// Stream events via channel.
	ch := make(chan agent.RunnerEvent, 32)

	go func() {
		defer close(ch)

		iter := einoRunner.Query(toolCtx, userMessage)

		for {
			event, ok := iter.Next()
			if !ok {
				break
			}

			if event.Err != nil {
				ch <- agent.RunnerEvent{
					Type:    "error",
					Content: event.Err.Error(),
				}
				return
			}

			if event.Output == nil || event.Output.MessageOutput == nil {
				continue
			}

			msg, err := event.Output.MessageOutput.GetMessage()
			if err != nil {
				r.log.Warn("failed to get message from event", "err", err)
				continue
			}
			if msg == nil {
				continue
			}

			role := event.Output.MessageOutput.Role

			// Tool call request (assistant asking to call tools).
			if role == schema.Assistant && len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					ch <- agent.RunnerEvent{
						Type:     "tool_call",
						ToolName: tc.Function.Name,
						Content:  tc.Function.Arguments,
					}
				}
				continue
			}

			// Tool result (tool returning output).
			if role == schema.Tool {
				ch <- agent.RunnerEvent{
					Type:     "tool_result",
					ToolName: msg.ToolName,
					Content:  msg.Content,
				}
				continue
			}

			// Text message from assistant.
			if msg.Content != "" {
				ch <- agent.RunnerEvent{
					Type:    "message",
					Content: msg.Content,
				}
			}

			// Usage metadata.
			if msg.ResponseMeta != nil && msg.ResponseMeta.Usage != nil {
				u := msg.ResponseMeta.Usage
				ch <- agent.RunnerEvent{
					Type: "usage",
					Usage: &agent.UsageInfo{
						PromptTokens:     u.PromptTokens,
						CompletionTokens: u.CompletionTokens,
						TotalTokens:      u.TotalTokens,
					},
				}
			}
		}
	}()

	return ch, nil
}

// compile-time check
var _ agent.AgentRunnerPort = (*Runner)(nil)

// NewSubRunnerFactory creates a SubRunnerFactory that produces isolated runners.
func NewSubRunnerFactory(chatModel model.ToolCallingChatModel) agent.SubRunnerFactory {
	return func(config agent.AgentConfig) (agent.AgentRunnerPort, error) {
		return NewRunner(chatModel), nil
	}
}

// ---------------------------------------------------------------------------
// toolAdapter wraps agent.AgentTool as Eino's tool.InvokableTool
// ---------------------------------------------------------------------------

type toolAdapter struct {
	inner    agent.AgentTool
	toolInfo *schema.ToolInfo
}

func newToolAdapter(t agent.AgentTool) *toolAdapter {
	def := t.Def()

	// Generate parameter schema from InputExample using Eino's jsonschema reflector.
	var params *schema.ParamsOneOf
	if def.InputExample != nil {
		r := &jsonschema.Reflector{
			Anonymous:      true,
			DoNotReference: true,
		}
		js := r.Reflect(def.InputExample)
		if js != nil {
			js.Version = ""
			params = schema.NewParamsOneOfByJSONSchema(js)
		}
	}

	return &toolAdapter{
		inner: t,
		toolInfo: &schema.ToolInfo{
			Name:        def.Name,
			Desc:        def.Description,
			ParamsOneOf: params,
		},
	}
}

func (a *toolAdapter) Info(_ context.Context) (*schema.ToolInfo, error) {
	return a.toolInfo, nil
}

func (a *toolAdapter) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	return a.inner.Invoke(ctx, argumentsInJSON)
}

// compile-time checks
var _ tool.InvokableTool = (*toolAdapter)(nil)
var _ tool.BaseTool = (*toolAdapter)(nil)
