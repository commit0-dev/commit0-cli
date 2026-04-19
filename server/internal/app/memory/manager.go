package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// Budgets controls the token allocation per memory tier.
type Budgets struct {
	WorkingTokens    int // default 8000
	SessionTokens    int // default 4000
	PersistentTokens int // default 2000
}

// DefaultBudgets returns the default memory budget allocation.
func DefaultBudgets() Budgets {
	return Budgets{
		WorkingTokens:    8000,
		SessionTokens:    4000,
		PersistentTokens: 2000,
	}
}

// Manager implements three-tier context memory management.
//
//	Tier 1: Working Memory   — current turn + recent tool results (in-context)
//	Tier 2: Session Memory   — compressed history of this investigation (SurrealDB)
//	Tier 3: Persistent Memory — cross-session knowledge (SurrealDB + vector retrieval)
type Manager struct {
	store      domain.MemoryStore
	embedder   domain.Embedder
	compressor domain.Compressor
	budgets    Budgets
	log        *slog.Logger
}

// NewManager creates a memory manager.
// Any dependency can be nil — features degrade gracefully.
func NewManager(
	store domain.MemoryStore,
	embedder domain.Embedder,
	compressor domain.Compressor,
	budgets Budgets,
) *Manager {
	if budgets.WorkingTokens <= 0 {
		budgets = DefaultBudgets()
	}
	return &Manager{
		store:      store,
		embedder:   embedder,
		compressor: compressor,
		budgets:    budgets,
		log:        slog.Default().With("component", "memory"),
	}
}

// BuildContext assembles the full context from all three tiers for an agent turn.
// Returns a formatted string ready to prepend to the system prompt.
func (m *Manager) BuildContext(ctx context.Context, sessionID, repoSlug, currentTurn string) (string, error) {
	var sb strings.Builder

	// Tier 1: Working memory (current turn — passed in, not stored)
	// The caller manages working memory directly (last N tool results).
	// We don't duplicate it here.

	// Tier 2: Session memory (compressed previous turns)
	if m.store != nil && sessionID != "" {
		sessionMems, err := m.store.ListSessionMemories(ctx, sessionID)
		if err != nil {
			m.log.Debug("failed to list session memories", "err", err)
		} else if len(sessionMems) > 0 {
			sb.WriteString("## Previous Investigation Context\n")
			tokenBudget := m.budgets.SessionTokens
			for _, mem := range sessionMems {
				if tokenBudget <= 0 {
					break
				}
				sb.WriteString(mem.Content)
				sb.WriteByte('\n')
				tokenBudget -= mem.TokenCount
			}
			sb.WriteByte('\n')
		}
	}

	// Tier 3: Persistent memory (cross-session knowledge, retrieved by relevance)
	if m.store != nil && m.embedder != nil && currentTurn != "" {
		vec, err := m.embedder.EmbedQuery(ctx, currentTurn)
		if err == nil && len(vec) > 0 {
			memories, err := m.store.RetrieveMemories(ctx, repoSlug, vec, 5)
			if err != nil {
				m.log.Debug("failed to retrieve persistent memories", "err", err)
			} else if len(memories) > 0 {
				sb.WriteString("## Known Patterns (from previous sessions)\n")
				tokenBudget := m.budgets.PersistentTokens
				for _, mem := range memories {
					if tokenBudget <= 0 {
						break
					}
					sb.WriteString("- ")
					sb.WriteString(mem.Content)
					sb.WriteByte('\n')
					tokenBudget -= mem.TokenCount
				}
				sb.WriteByte('\n')
			}
		}
	}

	return sb.String(), nil
}

// AfterTurn compresses the completed turn and stores it as session memory.
// Call this after each agent turn to maintain the compressed history.
func (m *Manager) AfterTurn(ctx context.Context, sessionID string, role, content string, toolCalls []string) error {
	if m.store == nil || sessionID == "" {
		return nil
	}

	// Compress the turn
	compressed := content
	if m.compressor != nil {
		var err error
		compressed, err = m.compressor.CompressTurn(ctx, role, content, toolCalls)
		if err != nil {
			m.log.Debug("compression failed, storing raw", "err", err)
			// Fallback: truncate to ~200 chars
			if len(compressed) > 200 {
				compressed = compressed[:200] + "..."
			}
		}
	} else {
		// No compressor — simple truncation
		if len(compressed) > 200 {
			compressed = compressed[:200] + "..."
		}
	}

	entry := &types.MemoryEntry{
		Tier:       types.MemorySession,
		SessionID:  sessionID,
		Content:    fmt.Sprintf("[%s] %s", role, compressed),
		TokenCount: estimateTokens(compressed),
	}

	if err := m.store.StoreMemory(ctx, entry); err != nil {
		return fmt.Errorf("store session memory: %w", err)
	}

	// Check if session memory exceeds budget — ultra-compress oldest entries
	return m.compactSessionIfNeeded(ctx, sessionID)
}

// StorePersistentMemory stores a cross-session insight with vector embedding.
func (m *Manager) StorePersistentMemory(ctx context.Context, repoSlug, content string, concepts []string) error {
	if m.store == nil {
		return nil
	}

	entry := &types.MemoryEntry{
		Tier:       types.MemoryPersistent,
		RepoSlug:   repoSlug,
		Content:    content,
		Concepts:   concepts,
		TokenCount: estimateTokens(content),
	}

	// Embed for vector retrieval
	if m.embedder != nil {
		vec, err := m.embedder.EmbedQuery(ctx, content)
		if err == nil {
			entry.Embedding = vec
		}
	}

	return m.store.StoreMemory(ctx, entry)
}

// compactSessionIfNeeded checks if session memory exceeds the budget
// and ultra-compresses oldest entries if so.
func (m *Manager) compactSessionIfNeeded(ctx context.Context, sessionID string) error {
	if m.store == nil || m.compressor == nil {
		return nil
	}

	memories, err := m.store.ListSessionMemories(ctx, sessionID)
	if err != nil {
		return nil //nolint:nilerr // non-fatal: best-effort memory, degrade gracefully
	}

	// Calculate total tokens
	totalTokens := 0
	for _, mem := range memories {
		totalTokens += mem.TokenCount
	}

	if totalTokens <= m.budgets.SessionTokens {
		return nil // within budget
	}

	// Over budget — compress oldest half into a single ultra-summary
	halfIdx := len(memories) / 2
	if halfIdx < 2 {
		return nil // too few to compact
	}

	oldTurns := make([]string, halfIdx)
	for i := 0; i < halfIdx; i++ {
		oldTurns[i] = memories[i].Content
	}

	ultraCompressed, err := m.compressor.CompressSession(ctx, oldTurns)
	if err != nil {
		m.log.Debug("session compression failed", "err", err)
		return nil
	}

	// Delete old entries and store the ultra-compressed version
	// For now, we don't delete old entries (append-only) — the budget check
	// in BuildContext will naturally skip entries that exceed the budget.
	// TODO: implement deletion of oldest entries and replace with ultra-compressed.
	_ = ultraCompressed

	return nil
}

// estimateTokens provides a rough token count (~4 chars per token for English).
func estimateTokens(s string) int {
	return len(s) / 4
}
