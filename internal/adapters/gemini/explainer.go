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
		model = "gemini-2.5-flash"
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

// ExplainStructured returns a structured JSON explanation using Gemini's
// response_json_schema feature. The returned bytes are valid JSON matching the
// schema for the given QueryType.
func (e *GeminiExplainer) ExplainStructured(ctx context.Context, req domain.ExplainRequest) ([]byte, error) {
	if req.UserQuery == "" {
		return nil, domain.Validation("ExplainStructured: UserQuery must not be empty")
	}

	prompt := buildExplainPrompt(req)
	schema := schemaForQueryType(req.QueryType)

	contents := []*genai.Content{
		genai.NewContentFromText(prompt, genai.RoleUser),
	}

	cfg := &genai.GenerateContentConfig{
		ResponseMIMEType:   "application/json",
		ResponseJsonSchema: schema,
	}

	resp, err := e.client.Models.GenerateContent(ctx, e.model, contents, cfg)
	if err != nil {
		return nil, classifyError(err)
	}

	if resp == nil || len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("gemini: ExplainStructured: empty response")
	}

	var result strings.Builder
	for _, cand := range resp.Candidates {
		if cand.Content == nil {
			continue
		}
		for _, part := range cand.Content.Parts {
			if part.Text != "" {
				result.WriteString(part.Text)
			}
		}
	}

	raw := result.String()
	if raw == "" {
		return nil, fmt.Errorf("gemini: ExplainStructured: no text in response")
	}

	e.log.DebugContext(ctx, "ExplainStructured complete",
		slog.String("model", e.model),
		slog.String("queryType", req.QueryType),
		slog.Int("bytes", len(raw)),
	)

	return []byte(raw), nil
}

// buildExplainPrompt constructs a structured prompt tailored to the query type.
func buildExplainPrompt(req domain.ExplainRequest) string {
	var sb strings.Builder

	// --- System preamble ---
	sb.WriteString("You are commit0, a senior software engineer and code intelligence agent. ")
	sb.WriteString("You analyze codebases deeply — understanding not just what the code does, ")
	sb.WriteString("but why it exists, how it fits into the architecture, and what a developer ")
	sb.WriteString("needs to know to work with it effectively.\n\n")
	sb.WriteString("Your audience is experienced engineers who need actionable insights, not tutorials. ")
	sb.WriteString("Be direct, technically precise, and opinionated when the code warrants it. ")
	sb.WriteString("Reference specific files, functions, and line numbers.\n\n")

	switch req.QueryType {
	case "search":
		sb.WriteString("Answer the developer's question using the code excerpts and graph context below.\n")
		sb.WriteString("- Lead with a direct answer in 2-3 sentences under '## Overview'.\n")
		sb.WriteString("- Then provide supporting evidence: relevant code paths, signatures, and architectural context.\n")
		sb.WriteString("- Highlight call relationships and data flow when they clarify behavior.\n")
		sb.WriteString("- Note design decisions or trade-offs if they are relevant to the question.\n")
		sb.WriteString("- Do NOT end with a summary, conclusion, or offer to help further.\n\n")
	case "trace":
		sb.WriteString("Explain the execution flow shown in the code excerpts below.\n")
		sb.WriteString("- Start with '## Overview' (2-3 sentences) summarizing the call chain end-to-end.\n")
		sb.WriteString("- Walk through each hop in execution order, explaining what it does and why control flows there.\n")
		sb.WriteString("- Note where data is transformed, validated, or persisted along the path.\n")
		sb.WriteString("- Call out branching points, error handling, and side effects.\n")
		sb.WriteString("- Do NOT end with a summary, conclusion, or offer to help further.\n\n")
	case "blast":
		sb.WriteString("Assess the blast radius of the change indicated by the code excerpts below.\n")
		sb.WriteString("- Start with '## Overview' (2-3 sentences) on impact scope and severity.\n")
		sb.WriteString("- Organize affected components by distance: direct callers → transitive dependents.\n")
		sb.WriteString("- For each affected area, explain what could break and how.\n")
		sb.WriteString("- Suggest a safe migration order if the change is non-trivial.\n")
		sb.WriteString("- Do NOT end with a summary, conclusion, or offer to help further.\n\n")
	default:
		sb.WriteString("Analyze the code excerpts below and answer the developer's question.\n")
		sb.WriteString("- Start with '## Overview', then provide details with file references.\n")
		sb.WriteString("- Do NOT end with a summary, conclusion, or offer to help further.\n\n")
	}

	// --- User question ---
	sb.WriteString("## Question\n")
	sb.WriteString(req.UserQuery)
	sb.WriteString("\n\n")

	// --- Code excerpts ---
	if len(req.CodeContext) > 0 {
		sb.WriteString("## Relevant Code\n")
		for _, exc := range req.CodeContext {
			fmt.Fprintf(&sb, "### %s\n", exc.Qualified)
			if exc.FilePath != "" {
				if exc.Lines != "" {
					fmt.Fprintf(&sb, "*File:* `%s` *Lines:* %s\n", exc.FilePath, exc.Lines)
				} else {
					fmt.Fprintf(&sb, "*File:* `%s`\n", exc.FilePath)
				}
			}
			if exc.Score > 0 {
				fmt.Fprintf(&sb, "*Relevance score:* %.3f\n", exc.Score)
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
