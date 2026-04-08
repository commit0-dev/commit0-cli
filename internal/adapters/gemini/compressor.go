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

// GeminiCompressor implements domain.Compressor using Gemini for context compression.
type GeminiCompressor struct {
	client *genai.Client
	model  string
	log    *slog.Logger
}

var _ domain.Compressor = (*GeminiCompressor)(nil)

// NewGeminiCompressor creates a compressor using the existing Gemini client.
func NewGeminiCompressor(client *genai.Client, cfg *config.GeminiConfig, log *slog.Logger) (*GeminiCompressor, error) {
	if client == nil {
		return nil, domain.Validation("GeminiCompressor: client must not be nil")
	}
	model := cfg.ExplainModel
	if model == "" {
		model = "gemini-2.5-flash"
	}
	return &GeminiCompressor{client: client, model: model, log: log}, nil
}

// CompressTurn reduces a full conversation turn to a 1-2 sentence summary.
func (c *GeminiCompressor) CompressTurn(ctx context.Context, role, content string, toolCalls []string) (string, error) {
	var sb strings.Builder
	sb.WriteString("Summarize this investigation turn in 1-2 sentences. ")
	sb.WriteString("Preserve file names, line numbers, function names, and key findings. ")
	sb.WriteString("Drop verbose code snippets and keep only conclusions.\n\n")
	fmt.Fprintf(&sb, "Role: %s\n", role)
	if len(toolCalls) > 0 {
		fmt.Fprintf(&sb, "Tools called: %s\n", strings.Join(toolCalls, ", "))
	}
	fmt.Fprintf(&sb, "Content:\n%s\n", content)

	resp, err := c.client.Models.GenerateContent(ctx, c.model,
		[]*genai.Content{genai.NewContentFromText(sb.String(), genai.RoleUser)}, nil)
	if err != nil {
		return "", fmt.Errorf("compress turn: %w", err)
	}

	return extractText(resp), nil
}

// CompressSession ultra-compresses a sequence of already-compressed turns.
func (c *GeminiCompressor) CompressSession(ctx context.Context, turns []string) (string, error) {
	var sb strings.Builder
	sb.WriteString("Create a 3-sentence summary of this investigation session. ")
	sb.WriteString("Preserve the causal chain, key evidence, and conclusions. ")
	sb.WriteString("Drop intermediate steps.\n\n")
	for i, turn := range turns {
		fmt.Fprintf(&sb, "Turn %d: %s\n", i+1, turn)
	}

	resp, err := c.client.Models.GenerateContent(ctx, c.model,
		[]*genai.Content{genai.NewContentFromText(sb.String(), genai.RoleUser)}, nil)
	if err != nil {
		return "", fmt.Errorf("compress session: %w", err)
	}

	return extractText(resp), nil
}

func extractText(resp *genai.GenerateContentResponse) string {
	if resp == nil || len(resp.Candidates) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, cand := range resp.Candidates {
		if cand.Content == nil {
			continue
		}
		for _, part := range cand.Content.Parts {
			if part.Text != "" {
				sb.WriteString(part.Text)
			}
		}
	}
	return strings.TrimSpace(sb.String())
}
