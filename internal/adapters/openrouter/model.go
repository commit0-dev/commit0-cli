package openrouter

import (
	"context"
	"fmt"
	"iter"
	"log/slog"

	"google.golang.org/adk/model"
)

// Model implements the ADK model.LLM interface using OpenRouter's API.
type Model struct {
	client  *Client
	name    string
	maxToks int
	log     *slog.Logger
}

// NewModel creates an ADK-compatible model backed by OpenRouter.
func NewModel(client *Client, modelName string, maxTokens int) *Model {
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	return &Model{
		client:  client,
		name:    modelName,
		maxToks: maxTokens,
		log:     slog.Default().With("adapter", "openrouter", "model", modelName),
	}
}

// Name returns the model identifier.
func (m *Model) Name() string { return m.name }

// GenerateContent implements model.LLM. Translates ADK request to OpenAI format,
// calls OpenRouter, translates response back.
func (m *Model) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	// Build OpenAI-compatible request.
	tracker := newToolCallIDTracker()

	var systemInstruction = req.Config.SystemInstruction
	messages := contentsToMessages(req.Contents, systemInstruction, tracker)
	tools := toolsToOpenAI(req.Config)

	chatReq := ChatRequest{
		Model:     m.name,
		Messages:  messages,
		Tools:     tools,
		MaxTokens: m.maxToks,
	}

	if len(tools) > 0 {
		chatReq.ToolChoice = "auto"
	}

	if stream {
		return m.generateStream(ctx, chatReq)
	}
	return m.generate(ctx, chatReq)
}

// generate handles non-streaming requests.
func (m *Model) generate(ctx context.Context, req ChatRequest) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		resp, err := m.client.Chat(ctx, req)
		if err != nil {
			yield(nil, fmt.Errorf("openrouter chat: %w", err))
			return
		}

		llmResp := responseToLLMResponse(resp)
		yield(llmResp, nil)
	}
}

// generateStream handles streaming requests with delta reassembly.
func (m *Model) generateStream(ctx context.Context, req ChatRequest) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		state := newStreamState()

		for chunk, err := range m.client.ChatStream(ctx, req) {
			if err != nil {
				yield(nil, fmt.Errorf("openrouter stream: %w", err))
				return
			}

			done := state.applyDelta(chunk)

			if done {
				// Final response with complete content.
				finalResp := state.toLLMResponse(false)
				yield(finalResp, nil)
				return
			}

			// Emit partial response for text streaming.
			if chunk != nil && len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				partialResp := state.toLLMResponse(true)
				if !yield(partialResp, nil) {
					return
				}
			}
		}

		// If stream ended without a finish_reason, emit what we have.
		if state.textContent.Len() > 0 || len(state.toolCalls) > 0 {
			finalResp := state.toLLMResponse(false)
			yield(finalResp, nil)
		}
	}
}

// Compile-time interface check.
var _ model.LLM = (*Model)(nil)
