package agent

import "context"

// ---------------------------------------------------------------------------
// Context helpers — replace ADK's tool.Context.ReadonlyState()
// ---------------------------------------------------------------------------

type ctxKey int

const (
	ctxKeyRepoSlug ctxKey = iota
	ctxKeyRepoPath
)

// WithRepoSlug stores the repo slug in context for tool invocations.
func WithRepoSlug(ctx context.Context, slug string) context.Context {
	return context.WithValue(ctx, ctxKeyRepoSlug, slug)
}

// RepoSlugFrom extracts the repo slug from context.
func RepoSlugFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRepoSlug).(string); ok {
		return v
	}
	return ""
}

// WithRepoPath stores the repo path in context for tool invocations.
func WithRepoPath(ctx context.Context, path string) context.Context {
	return context.WithValue(ctx, ctxKeyRepoPath, path)
}

// RepoPathFrom extracts the repo path from context.
func RepoPathFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRepoPath).(string); ok {
		return v
	}
	return "."
}

// ---------------------------------------------------------------------------
// Tool port — replaces ADK tool.Tool + functiontool.New()
// ---------------------------------------------------------------------------

// ToolDef describes a tool for schema generation. InputExample is a
// zero-value instance of the input struct used by the adapter layer
// to auto-generate JSON Schema via reflection.
type ToolDef struct {
	Name         string
	Description  string
	InputExample any // zero-value of the input struct (e.g. searchInput{})
}

// AgentTool is a tool invokable by the agent framework.
type AgentTool interface {
	Def() ToolDef
	Invoke(ctx context.Context, argsJSON string) (string, error)
}

// ---------------------------------------------------------------------------
// Usage tracking
// ---------------------------------------------------------------------------

// UsageInfo tracks token consumption per model call.
type UsageInfo struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// ---------------------------------------------------------------------------
// Runner port — replaces ADK runner.Runner + llmagent + session
// ---------------------------------------------------------------------------

// RunnerEvent is emitted during agent execution.
type RunnerEvent struct {
	Type     string // "tool_call", "tool_result", "message", "usage", "error"
	Content  string
	ToolName string
	Usage    *UsageInfo
}

// AgentConfig holds parameters for creating an agent instance.
type AgentConfig struct {
	Name        string
	Description string
	Instruction string
	Tools       []AgentTool
}

// AgentRunnerPort creates and runs agent instances. The adapter layer
// implements this using the concrete agent framework (Eino).
type AgentRunnerPort interface {
	Run(ctx context.Context, config AgentConfig, userMessage string,
		state map[string]any) (<-chan RunnerEvent, error)
}

// SubRunnerFactory creates isolated AgentRunnerPort instances for sub-agents.
// Replaces ADK's ModelFactory. Injected from the wire layer so delegate.go
// never imports concrete adapters.
type SubRunnerFactory func(config AgentConfig) (AgentRunnerPort, error)
