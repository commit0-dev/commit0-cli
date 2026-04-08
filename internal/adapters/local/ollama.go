package local

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/commit0-dev/commit0/internal/domain"
)

// OllamaExplainer implements domain.LLMExplainer using a local Ollama instance.
// It calls the Ollama chat API at http://localhost:11434/api/chat.
type OllamaExplainer struct {
	baseURL string
	model   string
	client  *http.Client
	log     *slog.Logger
}

// Compile-time check.
var _ domain.LLMExplainer = (*OllamaExplainer)(nil)

// NewOllamaExplainer creates an explainer backed by a local Ollama model.
func NewOllamaExplainer(baseURL, model string, log *slog.Logger) *OllamaExplainer {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "gemma3:4b"
	}
	return &OllamaExplainer{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		client:  &http.Client{},
		log:     log.With("adapter", "ollama", "model", model),
	}
}

// ollamaChatRequest is the Ollama /api/chat request body.
type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   json.RawMessage `json:"format,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message ollamaMessage `json:"message"`
}

// Explain streams a natural-language explanation via Ollama's chat API.
func (o *OllamaExplainer) Explain(ctx context.Context, req domain.ExplainRequest) (<-chan domain.ExplainChunk, error) {
	if req.UserQuery == "" {
		return nil, domain.Validation("Explain: UserQuery must not be empty")
	}

	prompt := buildOllamaPrompt(req)
	ch := make(chan domain.ExplainChunk, 16)

	go func() {
		defer close(ch)

		body, _ := json.Marshal(ollamaChatRequest{
			Model:    o.model,
			Messages: []ollamaMessage{{Role: "user", Content: prompt}},
			Stream:   false, // non-streaming for simplicity
		})

		httpReq, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/api/chat", bytes.NewReader(body))
		if err != nil {
			ch <- domain.ExplainChunk{Error: err, Done: true}
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := o.client.Do(httpReq)
		if err != nil {
			ch <- domain.ExplainChunk{Error: fmt.Errorf("ollama: %w", err), Done: true}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			ch <- domain.ExplainChunk{Error: fmt.Errorf("ollama: %d %s", resp.StatusCode, string(bodyBytes)), Done: true}
			return
		}

		var chatResp ollamaChatResponse
		if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
			ch <- domain.ExplainChunk{Error: fmt.Errorf("ollama decode: %w", err), Done: true}
			return
		}

		ch <- domain.ExplainChunk{Text: chatResp.Message.Content}
		ch <- domain.ExplainChunk{Done: true}
	}()

	return ch, nil
}

// ExplainStructured returns structured JSON from Ollama using the format parameter.
func (o *OllamaExplainer) ExplainStructured(ctx context.Context, req domain.ExplainRequest) ([]byte, error) {
	if req.UserQuery == "" {
		return nil, domain.Validation("ExplainStructured: UserQuery must not be empty")
	}

	prompt := buildOllamaPrompt(req)
	prompt += "\n\nRespond ONLY with valid JSON. No markdown, no explanation outside the JSON."

	body, _ := json.Marshal(ollamaChatRequest{
		Model:    o.model,
		Messages: []ollamaMessage{{Role: "user", Content: prompt}},
		Stream:   false,
		Format:   json.RawMessage(`"json"`),
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama: %d %s", resp.StatusCode, string(bodyBytes))
	}

	var chatResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("ollama decode: %w", err)
	}

	content := strings.TrimSpace(chatResp.Message.Content)
	if content == "" {
		return nil, fmt.Errorf("ollama: empty response")
	}

	o.log.Debug("ExplainStructured complete", "bytes", len(content), "queryType", req.QueryType)
	return []byte(content), nil
}

// Ping checks if Ollama is running and the model is available.
func (o *OllamaExplainer) Ping(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", o.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := o.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ollama not reachable at %s: %w", o.baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned %d", resp.StatusCode)
	}
	return nil
}

// buildOllamaPrompt reuses the same prompt structure as Gemini explainer.
func buildOllamaPrompt(req domain.ExplainRequest) string {
	var sb strings.Builder

	sb.WriteString("You are commit0, a senior software engineer and code intelligence agent. ")
	sb.WriteString("Be direct, technically precise, and reference specific files and line numbers.\n\n")

	switch req.QueryType {
	case "search":
		sb.WriteString("Answer the question using the code excerpts below. Lead with a direct answer.\n\n")
	case "trace":
		sb.WriteString("Explain the execution flow shown below. Walk through each hop.\n\n")
	case "blast":
		sb.WriteString("Assess the blast radius. Organize by distance from the change.\n\n")
	case "summarize":
		sb.WriteString("For each function, write a one-paragraph summary and 3-5 concept tags.\n\n")
	case "summarize-single":
		sb.WriteString("Write a one-paragraph summary and 3-5 concept tags for this code.\n\n")
	}

	sb.WriteString(req.UserQuery)

	if len(req.CodeContext) > 0 {
		sb.WriteString("\n\n## Code\n")
		for _, exc := range req.CodeContext {
			fmt.Fprintf(&sb, "### %s\n", exc.Qualified)
			if exc.FilePath != "" {
				fmt.Fprintf(&sb, "File: %s\n", exc.FilePath)
			}
			if exc.Snippet != "" {
				sb.WriteString("```\n")
				sb.WriteString(exc.Snippet)
				sb.WriteString("\n```\n")
			}
		}
	}

	if req.GraphContext != "" {
		sb.WriteString("\n## Graph Context\n")
		sb.WriteString(req.GraphContext)
	}

	return sb.String()
}
