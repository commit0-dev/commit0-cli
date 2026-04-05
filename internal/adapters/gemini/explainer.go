package gemini

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/genai"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
)

// GeminiExplainer implements domain.LLMExplainer using Gemini 2.0 Flash with
// server-sent streaming via GenerateContentStream.
type GeminiExplainer struct {
	client *genai.Client
	log    *slog.Logger
	model  string
}

// Compile-time interface check.
var _ domain.LLMExplainer = (*GeminiExplainer)(nil)

// NewGeminiExplainer constructs a GeminiExplainer that shares the provided
// *genai.Client with GeminiEmbedder (no extra connection needed).
func NewGeminiExplainer(client *genai.Client, cfg *config.GeminiConfig, log *slog.Logger) (*GeminiExplainer, error) {
	if client == nil {
		return nil, domain.Validation("GeminiExplainer: client must not be nil")
	}
	model := cfg.ExplainModel
	if model == "" {
		model = "gemini-2.0-flash"
	}
	return &GeminiExplainer{
		client: client,
		model:  model,
		log:    log,
	}, nil
}

// Explain streams a natural-language explanation for the given request. The
// returned channel is closed after the final chunk (Done==true) or on error.
// The caller must drain the channel to avoid goroutine leaks.
func (e *GeminiExplainer) Explain(ctx context.Context, req domain.ExplainRequest) (<-chan domain.ExplainChunk, error) {
	if req.UserQuery == "" {
		return nil, domain.Validation("Explain: UserQuery must not be empty")
	}

	prompt := buildExplainPrompt(req)
	ch := make(chan domain.ExplainChunk, 16)

	go func() {
		defer close(ch)

		contents := []*genai.Content{
			genai.NewContentFromText(prompt, genai.RoleUser),
		}

		stream := e.client.Models.GenerateContentStream(ctx, e.model, contents, nil)

		for resp, err := range stream {
			if err != nil {
				mapped := classifyError(err)
				e.log.ErrorContext(ctx, "GenerateContentStream error",
					slog.String("model", e.model),
					slog.Any("err", mapped),
				)
				select {
				case ch <- domain.ExplainChunk{Error: mapped, Done: true}:
				case <-ctx.Done():
				}
				return
			}

			for _, cand := range resp.Candidates {
				if cand.Content == nil {
					continue
				}
				for _, part := range cand.Content.Parts {
					if part.Text == "" {
						continue
					}
					select {
					case ch <- domain.ExplainChunk{Text: part.Text}:
					case <-ctx.Done():
						select {
						case ch <- domain.ExplainChunk{
							Error: fmt.Errorf("gemini: Explain: context canceled: %w", ctx.Err()),
							Done:  true,
						}:
						default:
						}
						return
					}
				}
			}
		}

		// Stream exhausted — signal completion.
		select {
		case ch <- domain.ExplainChunk{Done: true}:
		case <-ctx.Done():
		}

		e.log.DebugContext(ctx, "Explain stream complete",
			slog.String("model", e.model),
			slog.String("queryType", req.QueryType),
		)
	}()

	return ch, nil
}

// buildExplainPrompt constructs a structured prompt tailored to the query type.
func buildExplainPrompt(req domain.ExplainRequest) string {
	var sb strings.Builder

	// --- System preamble ---
	sb.WriteString("You are commit0, an expert code analyst. ")
	switch req.QueryType {
	case "search":
		sb.WriteString("Answer the developer's question using the code excerpts below. ")
		sb.WriteString("Be precise, cite file paths and line ranges where relevant.\n\n")
	case "trace":
		sb.WriteString("Explain the call chain shown in the code excerpts below. ")
		sb.WriteString("Walk through each hop in execution order and summarize what each step does.\n\n")
	case "blast":
		sb.WriteString("Describe the blast radius of the change indicated by the code excerpts below. ")
		sb.WriteString("List affected components, explain why each is impacted, and suggest safe migration steps.\n\n")
	default:
		sb.WriteString("Analyze the code excerpts below and answer the developer's question.\n\n")
	}

	// --- User question ---
	sb.WriteString("## Question\n")
	sb.WriteString(req.UserQuery)
	sb.WriteString("\n\n")

	// --- Code excerpts ---
	if len(req.CodeContext) > 0 {
		sb.WriteString("## Relevant Code\n")
		for _, exc := range req.CodeContext {
			sb.WriteString(fmt.Sprintf("### %s\n", exc.Qualified))
			if exc.FilePath != "" {
				if exc.Lines != "" {
					sb.WriteString(fmt.Sprintf("*File:* `%s` *Lines:* %s\n", exc.FilePath, exc.Lines))
				} else {
					sb.WriteString(fmt.Sprintf("*File:* `%s`\n", exc.FilePath))
				}
			}
			if exc.Score > 0 {
				sb.WriteString(fmt.Sprintf("*Relevance score:* %.3f\n", exc.Score))
			}
			if exc.Snippet != "" {
				sb.WriteString("```\n")
				sb.WriteString(exc.Snippet)
				if !strings.HasSuffix(exc.Snippet, "\n") {
					sb.WriteByte('\n')
				}
				sb.WriteString("```\n")
			}
			sb.WriteByte('\n')
		}
	}

	// --- Graph context ---
	if req.GraphContext != "" {
		sb.WriteString("## Graph Context\n")
		sb.WriteString(req.GraphContext)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Answer\n")
	return sb.String()
}
