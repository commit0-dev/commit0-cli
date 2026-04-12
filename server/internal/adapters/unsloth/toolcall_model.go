package unsloth

import (
	"context"
	"log/slog"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// ToolCallModel wraps a ToolCallingChatModel for providers (like Unsloth/llama.cpp)
// that don't support structured tool_calls in the API response. Instead, it:
//   - Injects tool descriptions into the system prompt
//   - Strips the tools parameter from the API request
//   - Parses <tool_call>{...}</tool_call> blocks from the model's text output
//   - Promotes them to proper schema.ToolCall objects
//
// This keeps the Eino ChatModelAgent's tool dispatch working transparently.
type ToolCallModel struct {
	inner model.ToolCallingChatModel
	tools []*schema.ToolInfo
}

// NewToolCallModel wraps a ChatModel with prompt-injected tool calling.
func NewToolCallModel(inner model.ToolCallingChatModel) *ToolCallModel {
	return &ToolCallModel{inner: inner}
}

// Generate calls the inner model, then post-processes the response
// to extract tool calls from content text.
func (m *ToolCallModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.applyOptions(opts)
	m.captureUserQuery(in)
	in = m.injectTools(in)
	in = m.patchToolRole(in)
	slog.Info("ToolCallModel.Generate called", "tools", len(m.tools), "registered", len(registeredToolNames))
	msg, err := m.inner.Generate(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	patched := m.patchToolCalls(msg)
	slog.Info("ToolCallModel.Generate result", "content_len", len(msg.Content), "tool_calls", len(patched.ToolCalls))
	return patched, nil
}

// applyOptions reads model.Options (including WithTools) passed at call time
// by the Eino compose graph and registers tool names for parsing.
func (m *ToolCallModel) applyOptions(opts []model.Option) {
	o := model.GetCommonOptions(&model.Options{}, opts...)
	if len(o.Tools) > 0 && len(m.tools) == 0 {
		m.tools = o.Tools
		m.registerNames()
	}
}

// captureUserQuery saves the last user message for bare-name tool call fallback.
func (m *ToolCallModel) captureUserQuery(msgs []*schema.Message) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == schema.User && msgs[i].Content != "" {
			SetLastUserQuery(msgs[i].Content)
			return
		}
	}
}

// Stream calls the inner model's Stream, collects the full response,
// then patches tool calls if found.
func (m *ToolCallModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.applyOptions(opts)
	m.captureUserQuery(in)
	in = m.injectTools(in)
	in = m.patchToolRole(in)
	slog.Info("ToolCallModel.Stream called", "tools", len(m.tools), "registered", len(registeredToolNames))
	stream, err := m.inner.Stream(ctx, in, opts...)
	if err != nil {
		return nil, err
	}

	// Wrap the stream to patch tool calls from the final assembled message.
	return wrapStreamWithToolCallParsing(stream), nil
}

// BindTools stores tools but does NOT pass them to the inner model's API.
// Instead, tools are injected as text in the system prompt.
func (m *ToolCallModel) BindTools(tools []*schema.ToolInfo) error {
	m.tools = tools
	m.registerNames()
	return nil
}

// WithTools returns a new ToolCallModel with tools bound.
func (m *ToolCallModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	clone := &ToolCallModel{inner: m.inner, tools: tools}
	clone.registerNames()
	return clone, nil
}

// registerNames stores tool names for the function-style parser fallback.
func (m *ToolCallModel) registerNames() {
	names := make([]string, len(m.tools))
	for i, t := range m.tools {
		names[i] = t.Name
	}
	RegisterToolNames(names)
}

// patchToolRole converts role:"tool" messages to role:"user" with a
// formatted prefix, since some providers (Unsloth/llama.cpp) only accept
// system/user/assistant roles.
func (m *ToolCallModel) patchToolRole(msgs []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, len(msgs))
	copy(out, msgs)
	for i, msg := range out {
		if msg.Role == schema.Tool {
			patched := *msg
			patched.Role = schema.User
			toolName := msg.ToolName
			if toolName == "" {
				toolName = "tool"
			}
			patched.Content = "[Tool result from " + toolName + "]: " + msg.Content
			patched.ToolCallID = ""
			patched.ToolName = ""
			out[i] = &patched
		}
		// Strip tool_calls from assistant messages — Unsloth doesn't understand them.
		if msg.Role == schema.Assistant && len(msg.ToolCalls) > 0 {
			patched := *msg
			patched.ToolCalls = nil
			out[i] = &patched
		}
	}
	return out
}

// injectTools prepends tool descriptions to the first system message
// (or creates one if none exists).
func (m *ToolCallModel) injectTools(msgs []*schema.Message) []*schema.Message {
	if len(m.tools) == 0 {
		return msgs
	}

	toolPrompt := buildToolPrompt(m.tools)
	if toolPrompt == "" {
		return msgs
	}

	// Clone to avoid mutating the caller's slice.
	out := make([]*schema.Message, len(msgs))
	copy(out, msgs)

	// Find and augment the system message, or prepend one.
	for i, msg := range out {
		if msg.Role == schema.System {
			patched := *msg
			patched.Content = msg.Content + toolPrompt
			out[i] = &patched
			return out
		}
	}

	// No system message — prepend one.
	sysMsg := &schema.Message{
		Role:    schema.System,
		Content: toolPrompt,
	}
	return append([]*schema.Message{sysMsg}, out...)
}

// patchToolCalls checks if the message has no ToolCalls but content
// contains <tool_call> blocks, and promotes them.
func (m *ToolCallModel) patchToolCalls(msg *schema.Message) *schema.Message {
	if msg == nil || len(msg.ToolCalls) > 0 {
		return msg // Already has structured tool calls — pass through.
	}

	calls, cleaned := parseToolCalls(msg.Content)
	if len(calls) == 0 {
		return msg // No tool calls found in content.
	}

	patched := *msg
	patched.ToolCalls = calls
	patched.Content = cleaned
	patched.Role = schema.Assistant // Ensure role is set for tool dispatch.
	return &patched
}

// wrapStreamWithToolCallParsing buffers the entire stream, assembles the full
// content, parses tool calls, then emits a single patched message.
// This is necessary because tool call text may span multiple chunks.
func wrapStreamWithToolCallParsing(inner *schema.StreamReader[*schema.Message]) *schema.StreamReader[*schema.Message] {
	pr, pw := schema.Pipe[*schema.Message](1)

	go func() {
		defer pw.Close()

		// Buffer all chunks to assemble full content.
		var fullContent strings.Builder
		var lastMsg *schema.Message

		for {
			chunk, err := inner.Recv()
			if err != nil {
				break
			}
			if chunk != nil {
				fullContent.WriteString(chunk.Content)
				lastMsg = chunk
			}
		}

		if lastMsg == nil {
			return
		}

		// Assemble the full message and try to parse tool calls.
		assembled := *lastMsg
		assembled.Content = fullContent.String()

		calls, cleaned := parseToolCalls(assembled.Content)
		if len(calls) > 0 {
			assembled.ToolCalls = calls
			assembled.Content = cleaned
		}

		_ = pw.Send(&assembled, nil)
	}()

	return pr
}

// Compile-time check.
var _ model.ToolCallingChatModel = (*ToolCallModel)(nil)
