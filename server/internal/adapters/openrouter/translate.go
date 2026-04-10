package openrouter

import (
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/genai"

	"google.golang.org/adk/model"
)

// ── genai → OpenAI (request direction) ──────────────────────────────────────

// toolCallIDTracker generates and tracks tool call IDs for mapping between
// Gemini (no IDs) and OpenAI (requires IDs) function calling formats.
type toolCallIDTracker struct {
	// nameToID maps function call name+index to generated ID.
	// Multiple calls to the same function get unique IDs.
	counter int
	nameToID map[string]string // "funcName:N" → "call_N"
	idToName map[string]string // "call_N" → "funcName"
}

func newToolCallIDTracker() *toolCallIDTracker {
	return &toolCallIDTracker{
		nameToID: make(map[string]string),
		idToName: make(map[string]string),
	}
}

func (t *toolCallIDTracker) generateID(funcName string) string {
	t.counter++
	id := fmt.Sprintf("call_%d", t.counter)
	key := fmt.Sprintf("%s:%d", funcName, t.counter)
	t.nameToID[key] = id
	t.idToName[id] = funcName
	return id
}

// lastIDForName returns the most recently generated ID for a function name.
func (t *toolCallIDTracker) lastIDForName(funcName string) string {
	// Search backwards for the most recent ID matching this function name.
	for i := t.counter; i > 0; i-- {
		key := fmt.Sprintf("%s:%d", funcName, i)
		if id, ok := t.nameToID[key]; ok {
			return id
		}
	}
	// Fallback: generate a new one.
	return t.generateID(funcName)
}

// contentsToMessages converts genai.Content slice to OpenAI messages.
// Also handles system instruction injection.
func contentsToMessages(contents []*genai.Content, systemInstruction *genai.Content, tracker *toolCallIDTracker) []Message {
	var messages []Message

	// System instruction → first message.
	if systemInstruction != nil {
		text := extractText(systemInstruction)
		if text != "" {
			messages = append(messages, Message{Role: "system", Content: text})
		}
	}

	for _, content := range contents {
		if content == nil {
			continue
		}

		role := translateRole(content.Role)

		// Collect text parts and function call parts separately.
		var textParts []string
		var toolCalls []ToolCall
		var toolResponses []Message

		for _, part := range content.Parts {
			switch {
			case part.Text != "":
				textParts = append(textParts, part.Text)

			case part.FunctionCall != nil:
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				id := tracker.generateID(part.FunctionCall.Name)
				toolCalls = append(toolCalls, ToolCall{
					ID:   id,
					Type: "function",
					Function: FunctionCall{
						Name:      part.FunctionCall.Name,
						Arguments: string(argsJSON),
					},
				})

			case part.FunctionResponse != nil:
				responseJSON, _ := json.Marshal(part.FunctionResponse.Response)
				callID := tracker.lastIDForName(part.FunctionResponse.Name)
				toolResponses = append(toolResponses, Message{
					Role:       "tool",
					Content:    string(responseJSON),
					ToolCallID: callID,
				})
			}
		}

		// Build the message(s).
		if len(toolCalls) > 0 {
			// Assistant message with tool calls.
			var content any
			if len(textParts) > 0 {
				content = strings.Join(textParts, "")
			}
			messages = append(messages, Message{
				Role:      "assistant",
				Content:   content,
				ToolCalls: toolCalls,
			})
		} else if len(textParts) > 0 {
			messages = append(messages, Message{
				Role:    role,
				Content: strings.Join(textParts, ""),
			})
		}

		// Tool responses are separate messages.
		messages = append(messages, toolResponses...)
	}

	return messages
}

// toolsToOpenAI converts genai tools to OpenAI function definitions.
func toolsToOpenAI(config *genai.GenerateContentConfig) []ToolDef {
	if config == nil {
		return nil
	}

	var tools []ToolDef
	for _, tool := range config.Tools {
		if tool == nil {
			continue
		}
		for _, fd := range tool.FunctionDeclarations {
			if fd == nil {
				continue
			}
			tools = append(tools, ToolDef{
				Type: "function",
				Function: FunctionDef{
					Name:        fd.Name,
					Description: fd.Description,
					Parameters:  schemaToJSONSchema(fd.Parameters),
				},
			})
		}
	}
	return tools
}

// schemaToJSONSchema converts genai.Schema to a JSON Schema-compatible map.
func schemaToJSONSchema(s *genai.Schema) any {
	if s == nil {
		return nil
	}

	schema := map[string]any{
		"type": strings.ToLower(string(s.Type)),
	}

	if s.Description != "" {
		schema["description"] = s.Description
	}

	if len(s.Enum) > 0 {
		schema["enum"] = s.Enum
	}

	if s.Items != nil {
		schema["items"] = schemaToJSONSchema(s.Items)
	}

	if len(s.Properties) > 0 {
		props := make(map[string]any)
		for name, prop := range s.Properties {
			props[name] = schemaToJSONSchema(prop)
		}
		schema["properties"] = props
	}

	if len(s.Required) > 0 {
		schema["required"] = s.Required
	}

	return schema
}

// ── OpenAI → genai (response direction) ─────────────────────────────────────

// responseToLLMResponse converts an OpenAI chat response to an ADK LLMResponse.
func responseToLLMResponse(resp *ChatResponse) *model.LLMResponse {
	if resp == nil || len(resp.Choices) == 0 {
		return &model.LLMResponse{}
	}

	choice := resp.Choices[0]
	content := messageToContent(choice.Message)

	return &model.LLMResponse{
		Content:      content,
		UsageMetadata: usageToMetadata(resp.Usage),
		FinishReason: translateFinishReason(choice.FinishReason),
		TurnComplete: true,
		ModelVersion: resp.Model,
	}
}

// messageToContent converts an OpenAI message to genai.Content.
func messageToContent(msg Message) *genai.Content {
	var parts []*genai.Part

	// Text content.
	if msg.Content != nil {
		if text, ok := msg.Content.(string); ok && text != "" {
			parts = append(parts, &genai.Part{Text: text})
		}
	}

	// Tool calls → FunctionCall parts.
	for _, tc := range msg.ToolCalls {
		var args map[string]any
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		parts = append(parts, &genai.Part{
			FunctionCall: &genai.FunctionCall{
				Name: tc.Function.Name,
				Args: args,
			},
		})
	}

	role := "model"
	if msg.Role == "user" {
		role = "user"
	}

	return &genai.Content{
		Role:  role,
		Parts: parts,
	}
}

// usageToMetadata converts OpenAI usage to genai usage metadata.
func usageToMetadata(usage Usage) *genai.GenerateContentResponseUsageMetadata {
	return &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:     int32(usage.PromptTokens),
		CandidatesTokenCount: int32(usage.CompletionTokens),
		TotalTokenCount:      int32(usage.TotalTokens),
	}
}

// ── Streaming ───────────────────────────────────────────────────────────────

// streamState accumulates streaming deltas into complete parts.
type streamState struct {
	textContent  strings.Builder
	toolCalls    map[int]*ToolCall // index → accumulated tool call
	finishReason string
	usage        *Usage
}

func newStreamState() *streamState {
	return &streamState{
		toolCalls: make(map[int]*ToolCall),
	}
}

// applyDelta accumulates a streaming delta into the state.
// Returns true if the stream is complete (finish_reason is set).
func (s *streamState) applyDelta(chunk *ChatStreamChunk) bool {
	if chunk == nil || len(chunk.Choices) == 0 {
		return false
	}

	choice := chunk.Choices[0]
	delta := choice.Delta

	// Accumulate text.
	if delta.Content != "" {
		s.textContent.WriteString(delta.Content)
	}

	// Accumulate tool calls (streaming sends partial deltas with index).
	for _, tc := range delta.ToolCalls {
		existing, ok := s.toolCalls[tc.Index]
		if !ok {
			existing = &ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: FunctionCall{
					Name: tc.Function.Name,
				},
			}
			s.toolCalls[tc.Index] = existing
		}
		existing.Function.Arguments += tc.Function.Arguments
		if tc.ID != "" {
			existing.ID = tc.ID
		}
		if tc.Function.Name != "" {
			existing.Function.Name = tc.Function.Name
		}
	}

	// Check for usage on final chunk.
	if chunk.Usage != nil {
		s.usage = chunk.Usage
	}

	// Check finish reason.
	if choice.FinishReason != nil {
		s.finishReason = *choice.FinishReason
		return true
	}

	return false
}

// toLLMResponse converts the accumulated state to an ADK LLMResponse.
func (s *streamState) toLLMResponse(partial bool) *model.LLMResponse {
	var parts []*genai.Part

	text := s.textContent.String()
	if text != "" {
		parts = append(parts, &genai.Part{Text: text})
	}

	for i := 0; i < len(s.toolCalls); i++ {
		tc := s.toolCalls[i]
		if tc == nil {
			continue
		}
		var args map[string]any
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		parts = append(parts, &genai.Part{
			FunctionCall: &genai.FunctionCall{
				Name: tc.Function.Name,
				Args: args,
			},
		})
	}

	resp := &model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: parts,
		},
		Partial:      partial,
		TurnComplete: !partial,
		FinishReason: translateFinishReason(s.finishReason),
	}

	if s.usage != nil {
		resp.UsageMetadata = usageToMetadata(*s.usage)
	}

	return resp
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func translateRole(geminiRole string) string {
	switch geminiRole {
	case "model":
		return "assistant"
	case "user":
		return "user"
	default:
		return geminiRole
	}
}

func translateFinishReason(reason string) genai.FinishReason {
	switch reason {
	case "stop":
		return genai.FinishReasonStop
	case "tool_calls":
		return genai.FinishReasonStop // tool calls are normal completion
	case "length":
		return genai.FinishReasonMaxTokens
	case "content_filter":
		return genai.FinishReasonSafety
	default:
		return genai.FinishReasonStop
	}
}

func extractText(content *genai.Content) string {
	if content == nil {
		return ""
	}
	var parts []string
	for _, p := range content.Parts {
		if p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// ToolCall Index field for streaming delta accumulation.
func init() {
	// The ToolCall struct doesn't have an Index field in types.go,
	// but the streaming delta uses index for ordering. We handle
	// this via the map[int]*ToolCall in streamState.
}
