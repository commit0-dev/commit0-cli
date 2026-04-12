package local

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
	"resty.dev/v3"
)

// OllamaModel implements the ADK model.LLM interface using a local Ollama instance.
// Ollama's /api/chat endpoint is OpenAI-compatible with tool calling support.
type OllamaModel struct {
	client  *resty.Client
	name    string
	maxToks int
	log     *slog.Logger
}

// Compile-time interface check.
var _ model.LLM = (*OllamaModel)(nil)

// NewOllamaModel creates an ADK-compatible model backed by a local Ollama instance.
func NewOllamaModel(baseURL, modelName string, maxTokens int) *OllamaModel {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	rc := resty.New().
		SetBaseURL(strings.TrimRight(baseURL, "/")).
		SetTimeout(120 * time.Second)

	return &OllamaModel{
		client:  rc,
		name:    modelName,
		maxToks: maxTokens,
		log:     slog.Default().With("adapter", "ollama-model", "model", modelName),
	}
}

// Name returns the model identifier.
func (m *OllamaModel) Name() string { return m.name }

// GenerateContent implements model.LLM. Translates ADK request to Ollama's
// OpenAI-compatible format, calls /api/chat, translates response back.
func (m *OllamaModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	tracker := newIDTracker()

	messages := contentsToOllamaMessages(req.Contents, req.Config.SystemInstruction, tracker)
	tools := toolsToOllamaDefs(req.Config)

	chatReq := ollamaModelRequest{
		Model:    m.name,
		Messages: messages,
		Stream:   stream,
		Options: ollamaOptions{
			NumPredict: m.maxToks,
		},
	}
	if len(tools) > 0 {
		chatReq.Tools = tools
	}

	if stream {
		return m.generateStream(ctx, chatReq)
	}
	return m.generate(ctx, chatReq)
}

// generate handles non-streaming requests.
func (m *OllamaModel) generate(ctx context.Context, req ollamaModelRequest) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		req.Stream = false

		var resp ollamaModelResponse
		httpResp, err := m.client.R().
			SetContext(ctx).
			SetBody(req).
			SetResult(&resp).
			Post("/api/chat")
		if err != nil {
			yield(nil, fmt.Errorf("ollama chat: %w", err))
			return
		}
		if httpResp.IsError() {
			yield(nil, fmt.Errorf("ollama chat: %d %s", httpResp.StatusCode(), httpResp.String()))
			return
		}

		llmResp := ollamaRespToLLM(&resp)
		yield(llmResp, nil)
	}
}

// generateStream handles streaming requests with line-delimited JSON.
func (m *OllamaModel) generateStream(ctx context.Context, req ollamaModelRequest) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		req.Stream = true

		// Use raw request for streaming (Resty doesn't natively handle NDJSON).
		body, _ := json.Marshal(req)
		httpReq, err := m.client.R().
			SetContext(ctx).
			SetHeader("Content-Type", "application/json").
			SetDoNotParseResponse(true).
			SetBody(body).
			Post("/api/chat")
		if err != nil {
			yield(nil, fmt.Errorf("ollama stream: %w", err))
			return
		}
		defer httpReq.Body.Close()

		var accumulated strings.Builder
		var lastResp *ollamaModelResponse

		scanner := bufio.NewScanner(httpReq.Body)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var chunk ollamaModelResponse
			if err := json.Unmarshal(line, &chunk); err != nil {
				continue
			}

			lastResp = &chunk
			if chunk.Message.Content != "" {
				accumulated.WriteString(chunk.Message.Content)
			}

			if chunk.Done {
				// Final chunk — build complete response.
				finalMsg := chunk.Message
				if finalMsg.Content == "" {
					finalMsg.Content = accumulated.String()
				}
				finalResp := &ollamaModelResponse{
					Model:   chunk.Model,
					Message: finalMsg,
					Done:    true,
				}
				llmResp := ollamaRespToLLM(finalResp)
				yield(llmResp, nil)
				return
			}

			// Emit partial for text streaming.
			if chunk.Message.Content != "" {
				partial := &model.LLMResponse{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: accumulated.String()}},
					},
					Partial:      true,
					TurnComplete: false,
					ModelVersion: chunk.Model,
				}
				if !yield(partial, nil) {
					return
				}
			}
		}

		// Stream ended without done=true — emit what we have.
		if lastResp != nil && accumulated.Len() > 0 {
			lastResp.Message.Content = accumulated.String()
			llmResp := ollamaRespToLLM(lastResp)
			yield(llmResp, nil)
		}
	}
}

// ── Types ──────────────────────────────────────────────────────────────────

type ollamaModelRequest struct {
	Model    string             `json:"model"`
	Messages []ollamaModelMsg   `json:"messages"`
	Stream   bool               `json:"stream"`
	Tools    []ollamaToolDef    `json:"tools,omitempty"`
	Options  ollamaOptions      `json:"options,omitempty"`
}

type ollamaOptions struct {
	NumPredict int `json:"num_predict,omitempty"`
}

type ollamaModelMsg struct {
	Role      string              `json:"role"`
	Content   string              `json:"content"`
	ToolCalls []ollamaToolCall    `json:"tool_calls,omitempty"`
	ToolID    string              `json:"tool_call_id,omitempty"`
}

type ollamaToolCall struct {
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type"`
	Function ollamaFunctionCall `json:"function"`
}

type ollamaFunctionCall struct {
	Name      string `json:"name"`
	Arguments any    `json:"arguments"` // map or JSON string
}

type ollamaToolDef struct {
	Type     string             `json:"type"`
	Function ollamaFunctionDef  `json:"function"`
}

type ollamaFunctionDef struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type ollamaModelResponse struct {
	Model   string         `json:"model"`
	Message ollamaModelMsg `json:"message"`
	Done    bool           `json:"done"`
}

// ── Translation ────────────────────────────────────────────────────────────

type idTracker struct {
	counter  int
	nameToID map[string]string
}

func newIDTracker() *idTracker {
	return &idTracker{nameToID: make(map[string]string)}
}

func (t *idTracker) generate(name string) string {
	t.counter++
	id := fmt.Sprintf("call_%d", t.counter)
	t.nameToID[name] = id
	return id
}

func (t *idTracker) lastID(name string) string {
	if id, ok := t.nameToID[name]; ok {
		return id
	}
	return t.generate(name)
}

// contentsToOllamaMessages converts genai.Content to Ollama messages.
func contentsToOllamaMessages(contents []*genai.Content, system *genai.Content, tracker *idTracker) []ollamaModelMsg {
	var msgs []ollamaModelMsg

	if system != nil {
		text := extractGenaiText(system)
		if text != "" {
			msgs = append(msgs, ollamaModelMsg{Role: "system", Content: text})
		}
	}

	for _, c := range contents {
		if c == nil {
			continue
		}

		role := "user"
		if c.Role == "model" {
			role = "assistant"
		}

		var textParts []string
		var toolCalls []ollamaToolCall
		var toolResps []ollamaModelMsg

		for _, p := range c.Parts {
			switch {
			case p.Text != "":
				textParts = append(textParts, p.Text)
			case p.FunctionCall != nil:
				argsJSON, _ := json.Marshal(p.FunctionCall.Args)
				id := tracker.generate(p.FunctionCall.Name)
				toolCalls = append(toolCalls, ollamaToolCall{
					ID:   id,
					Type: "function",
					Function: ollamaFunctionCall{
						Name:      p.FunctionCall.Name,
						Arguments: json.RawMessage(argsJSON),
					},
				})
			case p.FunctionResponse != nil:
				respJSON, _ := json.Marshal(p.FunctionResponse.Response)
				callID := tracker.lastID(p.FunctionResponse.Name)
				toolResps = append(toolResps, ollamaModelMsg{
					Role:    "tool",
					Content: string(respJSON),
					ToolID:  callID,
				})
			}
		}

		if len(toolCalls) > 0 {
			msgs = append(msgs, ollamaModelMsg{
				Role:      "assistant",
				Content:   strings.Join(textParts, ""),
				ToolCalls: toolCalls,
			})
		} else if len(textParts) > 0 {
			msgs = append(msgs, ollamaModelMsg{
				Role:    role,
				Content: strings.Join(textParts, ""),
			})
		}

		msgs = append(msgs, toolResps...)
	}

	return msgs
}

// toolsToOllamaDefs converts genai tools to Ollama function definitions.
func toolsToOllamaDefs(config *genai.GenerateContentConfig) []ollamaToolDef {
	if config == nil {
		return nil
	}
	var defs []ollamaToolDef
	for _, tool := range config.Tools {
		if tool == nil {
			continue
		}
		for _, fd := range tool.FunctionDeclarations {
			if fd == nil {
				continue
			}
			defs = append(defs, ollamaToolDef{
				Type: "function",
				Function: ollamaFunctionDef{
					Name:        fd.Name,
					Description: fd.Description,
					Parameters:  schemaToMap(fd.Parameters),
				},
			})
		}
	}
	return defs
}

// schemaToMap converts genai.Schema to a JSON Schema map.
func schemaToMap(s *genai.Schema) any {
	if s == nil {
		return nil
	}
	m := map[string]any{"type": strings.ToLower(string(s.Type))}
	if s.Description != "" {
		m["description"] = s.Description
	}
	if len(s.Enum) > 0 {
		m["enum"] = s.Enum
	}
	if s.Items != nil {
		m["items"] = schemaToMap(s.Items)
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any)
		for k, v := range s.Properties {
			props[k] = schemaToMap(v)
		}
		m["properties"] = props
	}
	if len(s.Required) > 0 {
		m["required"] = s.Required
	}
	return m
}

// ollamaRespToLLM converts an Ollama response to an ADK LLMResponse.
func ollamaRespToLLM(resp *ollamaModelResponse) *model.LLMResponse {
	var parts []*genai.Part

	if resp.Message.Content != "" {
		parts = append(parts, &genai.Part{Text: resp.Message.Content})
	}

	for _, tc := range resp.Message.ToolCalls {
		args := make(map[string]any)
		switch v := tc.Function.Arguments.(type) {
		case map[string]any:
			args = v
		case json.RawMessage:
			_ = json.Unmarshal(v, &args)
		case string:
			_ = json.Unmarshal([]byte(v), &args)
		}
		parts = append(parts, &genai.Part{
			FunctionCall: &genai.FunctionCall{
				Name: tc.Function.Name,
				Args: args,
			},
		})
	}

	finishReason := genai.FinishReasonStop
	if !resp.Done {
		finishReason = genai.FinishReasonUnspecified
	}

	return &model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: parts,
		},
		TurnComplete: resp.Done,
		FinishReason: finishReason,
		ModelVersion: resp.Model,
	}
}

func extractGenaiText(c *genai.Content) string {
	if c == nil {
		return ""
	}
	var parts []string
	for _, p := range c.Parts {
		if p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// Ensure unused imports don't cause errors.
var (
	_ = io.EOF
	_ = bytes.NewReader
)
