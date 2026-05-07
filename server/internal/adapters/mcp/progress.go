package mcp

import (
	"context"
	"log/slog"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ProgressEmitter sends notifications/progress to the connected MCP client
// during long-running tool calls. Emitting progress keeps the client's
// spinner alive and avoids premature timeouts for tools like find_root_cause
// (planned in a future PR).
//
// Usage:
//
//	p := NewProgressEmitter(session, progressToken, totalSteps)
//	p.Emit(ctx, 1, "Locating entry points…")
//	// … do work …
//	p.Emit(ctx, 2, "Tracing call graph…")
type ProgressEmitter struct {
	session       *mcpsdk.ServerSession
	progressToken any // the token from the original request, or a synthetic one
	total         float64
	log           *slog.Logger
}

// NewProgressEmitter creates a ProgressEmitter bound to the server session.
// If session is nil (e.g. in in-process tests), Emit() is a no-op.
func NewProgressEmitter(session *mcpsdk.ServerSession, progressToken any, total int) *ProgressEmitter {
	return &ProgressEmitter{
		session:       session,
		progressToken: progressToken,
		total:         float64(total),
		log:           slog.Default().With("component", "mcp.progress"),
	}
}

// Emit sends a progress notification for step number `step` with a
// human-readable message. Steps are 1-indexed.
func (p *ProgressEmitter) Emit(ctx context.Context, step int, message string) {
	if p == nil || p.session == nil || p.progressToken == nil {
		return
	}

	params := &mcpsdk.ProgressNotificationParams{
		ProgressToken: p.progressToken,
		Progress:      float64(step),
		Total:         p.total,
		Message:       message,
	}

	if err := p.session.NotifyProgress(ctx, params); err != nil {
		// Non-fatal — the tool result still lands correctly without progress.
		p.log.Debug("progress notification failed", "step", step, "err", err)
	}
}
