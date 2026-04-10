// Package openrouter provides an ADK-compatible model adapter for OpenRouter,
// translating between Google's genai types and OpenAI's chat completion format.
package openrouter

// ── OpenAI Chat Completion Request ──────────────────────────────────────────

// ChatRequest is an OpenAI-compatible chat completion request.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	ToolChoice  any       `json:"tool_choice,omitempty"` // "auto", "none", or specific
	Stream      bool      `json:"stream,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
}

// Message is a single message in the conversation.
type Message struct {
	Role       string     `json:"role"`                  // system, user, assistant, tool
	Content    any        `json:"content"`               // string or nil (for tool calls)
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`  // assistant → tool invocations
	ToolCallID string     `json:"tool_call_id,omitempty"` // tool → response to specific call
}

// ToolCall represents a function call in an assistant message.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall is the function name + arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ToolDef defines a tool available to the model.
type ToolDef struct {
	Type     string      `json:"type"` // "function"
	Function FunctionDef `json:"function"`
}

// FunctionDef is a tool's function schema.
type FunctionDef struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"` // JSON Schema object
}

// ── OpenAI Chat Completion Response ─────────────────────────────────────────

// ChatResponse is an OpenAI-compatible chat completion response.
type ChatResponse struct {
	ID      string   `json:"id"` // generation ID (for OpenRouter cost lookup)
	Object  string   `json:"object"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice is a single completion choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"` // stop, tool_calls, length, content_filter
}

// Usage tracks token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ── Streaming Types ─────────────────────────────────────────────────────────

// ChatStreamChunk is a single SSE chunk in a streaming response.
type ChatStreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
	Usage   *Usage         `json:"usage,omitempty"` // present on final chunk
}

// StreamChoice is a streaming delta choice.
type StreamChoice struct {
	Index        int          `json:"index"`
	Delta        StreamDelta  `json:"delta"`
	FinishReason *string      `json:"finish_reason"` // nil until final chunk
}

// StreamDelta is the incremental content in a streaming chunk.
type StreamDelta struct {
	Role      string            `json:"role,omitempty"`
	Content   string            `json:"content,omitempty"`
	ToolCalls []StreamToolCall  `json:"tool_calls,omitempty"`
}

// StreamToolCall is a tool call delta in a streaming response (has Index field).
type StreamToolCall struct {
	Index    int          `json:"index"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}
