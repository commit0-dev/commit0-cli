package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ── Fakes ────────────────────────────────────────────────────────────────

type fakeStore struct {
	stored        []*types.MemoryEntry
	sessionMems   []types.MemoryEntry
	retrieveMems  []types.MemoryEntry
	storeErr      error
	listErr       error
	retrieveErr   error
	storeCalls    int
	listCalls     int
	retrieveCalls int
}

func (f *fakeStore) StoreMemory(_ context.Context, e *types.MemoryEntry) error {
	f.storeCalls++
	if f.storeErr != nil {
		return f.storeErr
	}
	f.stored = append(f.stored, e)
	return nil
}

func (f *fakeStore) RetrieveMemories(_ context.Context, _ string, _ []float32, _ int) ([]types.MemoryEntry, error) {
	f.retrieveCalls++
	return f.retrieveMems, f.retrieveErr
}

func (f *fakeStore) ListSessionMemories(_ context.Context, _ string) ([]types.MemoryEntry, error) {
	f.listCalls++
	return f.sessionMems, f.listErr
}

func (f *fakeStore) DeleteSessionMemories(_ context.Context, _ string) error { return nil }

type fakeEmbedder struct {
	vec []float32
	err error
}

func (f *fakeEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return f.vec, f.err
}

func (f *fakeEmbedder) EmbedBatch(_ context.Context, _ []domain.EmbedInput) ([]domain.EmbedResult, error) {
	return nil, nil
}

type fakeCompressor struct {
	turnOut    string
	sessionOut string
	turnErr    error
	sessionErr error
}

func (f *fakeCompressor) CompressTurn(_ context.Context, _ string, _ string, _ []string) (string, error) {
	return f.turnOut, f.turnErr
}

func (f *fakeCompressor) CompressSession(_ context.Context, _ []string) (string, error) {
	return f.sessionOut, f.sessionErr
}

// ── DefaultBudgets / NewManager ──────────────────────────────────────────

func TestDefaultBudgets(t *testing.T) {
	b := DefaultBudgets()
	if b.WorkingTokens != 8000 || b.SessionTokens != 4000 || b.PersistentTokens != 2000 {
		t.Errorf("default budgets unexpected: %+v", b)
	}
}

func TestNewManager_DefaultsBudgets(t *testing.T) {
	m := NewManager(nil, nil, nil, Budgets{})
	if m.budgets.WorkingTokens != 8000 {
		t.Errorf("zero-budget should fall back to defaults, got %+v", m.budgets)
	}
}

func TestNewManager_KeepsCustomBudgets(t *testing.T) {
	custom := Budgets{WorkingTokens: 100, SessionTokens: 50, PersistentTokens: 25}
	m := NewManager(nil, nil, nil, custom)
	if m.budgets != custom {
		t.Errorf("custom budgets dropped: %+v", m.budgets)
	}
}

// ── BuildContext ─────────────────────────────────────────────────────────

func TestBuildContext_NoStore_ReturnsEmpty(t *testing.T) {
	m := NewManager(nil, nil, nil, DefaultBudgets())
	got, err := m.BuildContext(context.Background(), "s", "r", "what")
	if err != nil || got != "" {
		t.Errorf("empty store should yield empty context, err=%v out=%q", err, got)
	}
}

func TestBuildContext_NoSessionID_SkipsSession(t *testing.T) {
	store := &fakeStore{}
	m := NewManager(store, nil, nil, DefaultBudgets())
	_, err := m.BuildContext(context.Background(), "", "r", "")
	if err != nil {
		t.Fatal(err)
	}
	if store.listCalls != 0 {
		t.Errorf("empty session id should not call ListSessionMemories")
	}
}

func TestBuildContext_SessionMemoriesIncluded(t *testing.T) {
	store := &fakeStore{
		sessionMems: []types.MemoryEntry{
			{Content: "turn-1", TokenCount: 10},
			{Content: "turn-2", TokenCount: 10},
		},
	}
	m := NewManager(store, nil, nil, DefaultBudgets())
	got, err := m.BuildContext(context.Background(), "session-x", "r", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Previous Investigation Context") {
		t.Errorf("expected session header, got %q", got)
	}
	if !strings.Contains(got, "turn-1") || !strings.Contains(got, "turn-2") {
		t.Errorf("expected both turns, got %q", got)
	}
}

func TestBuildContext_SessionListErrorIsLogged(t *testing.T) {
	store := &fakeStore{listErr: errors.New("boom")}
	m := NewManager(store, nil, nil, DefaultBudgets())
	got, err := m.BuildContext(context.Background(), "s", "r", "")
	if err != nil {
		t.Fatalf("ListSessionMemories error should be swallowed: %v", err)
	}
	if got != "" {
		t.Errorf("on list error, no context should be assembled, got %q", got)
	}
}

func TestBuildContext_SessionBudgetExhaustsLoop(t *testing.T) {
	store := &fakeStore{
		sessionMems: []types.MemoryEntry{
			{Content: "first", TokenCount: 4000},
			{Content: "second-should-be-skipped", TokenCount: 100},
		},
	}
	m := NewManager(store, nil, nil, Budgets{SessionTokens: 4000, PersistentTokens: 1000, WorkingTokens: 100})
	out, _ := m.BuildContext(context.Background(), "s", "r", "")
	if !strings.Contains(out, "first") {
		t.Errorf("first turn should be present: %q", out)
	}
	if strings.Contains(out, "second-should-be-skipped") {
		t.Errorf("second turn should be skipped after budget exhausted: %q", out)
	}
}

func TestBuildContext_PersistentMemoriesIncluded(t *testing.T) {
	store := &fakeStore{
		retrieveMems: []types.MemoryEntry{
			{Content: "pattern-A", TokenCount: 10},
			{Content: "pattern-B", TokenCount: 10},
		},
	}
	emb := &fakeEmbedder{vec: []float32{1, 2, 3}}
	m := NewManager(store, emb, nil, DefaultBudgets())
	got, _ := m.BuildContext(context.Background(), "", "r", "what is going on")
	if !strings.Contains(got, "Known Patterns") {
		t.Errorf("expected persistent header, got %q", got)
	}
	if !strings.Contains(got, "- pattern-A") {
		t.Errorf("persistent items should be bullet-prefixed: %q", got)
	}
}

func TestBuildContext_PersistentEmbedFailsSilently(t *testing.T) {
	store := &fakeStore{}
	emb := &fakeEmbedder{err: errors.New("nope")}
	m := NewManager(store, emb, nil, DefaultBudgets())
	out, err := m.BuildContext(context.Background(), "", "r", "query")
	if err != nil || out != "" {
		t.Errorf("embedder error should produce empty output: err=%v out=%q", err, out)
	}
}

func TestBuildContext_PersistentEmptyEmbed_NoCall(t *testing.T) {
	store := &fakeStore{retrieveMems: []types.MemoryEntry{{Content: "x", TokenCount: 1}}}
	emb := &fakeEmbedder{vec: nil} // empty vector — must not call retrieve
	m := NewManager(store, emb, nil, DefaultBudgets())
	_, _ = m.BuildContext(context.Background(), "", "r", "query")
	if store.retrieveCalls != 0 {
		t.Errorf("empty embedding should skip RetrieveMemories")
	}
}

func TestBuildContext_PersistentRetrieveError(t *testing.T) {
	store := &fakeStore{retrieveErr: errors.New("retrieve failed")}
	emb := &fakeEmbedder{vec: []float32{1}}
	m := NewManager(store, emb, nil, DefaultBudgets())
	out, err := m.BuildContext(context.Background(), "", "r", "query")
	if err != nil {
		t.Fatalf("retrieve error should be swallowed: %v", err)
	}
	if out != "" {
		t.Errorf("on retrieve error, no persistent block expected: %q", out)
	}
}

func TestBuildContext_PersistentBudgetExhausts(t *testing.T) {
	store := &fakeStore{
		retrieveMems: []types.MemoryEntry{
			{Content: "p1", TokenCount: 2000},
			{Content: "p2-should-be-dropped", TokenCount: 1},
		},
	}
	emb := &fakeEmbedder{vec: []float32{1}}
	m := NewManager(store, emb, nil, Budgets{WorkingTokens: 1, SessionTokens: 1, PersistentTokens: 2000})
	out, _ := m.BuildContext(context.Background(), "", "r", "query")
	if !strings.Contains(out, "p1") {
		t.Errorf("expected p1 in output: %q", out)
	}
	if strings.Contains(out, "p2-should-be-dropped") {
		t.Errorf("p2 should be skipped after budget exhausted: %q", out)
	}
}

// ── AfterTurn ────────────────────────────────────────────────────────────

func TestAfterTurn_NoStore(t *testing.T) {
	m := NewManager(nil, nil, nil, DefaultBudgets())
	if err := m.AfterTurn(context.Background(), "s", "user", "hello", nil); err != nil {
		t.Errorf("AfterTurn with no store should be no-op, got %v", err)
	}
}

func TestAfterTurn_NoSessionID(t *testing.T) {
	store := &fakeStore{}
	m := NewManager(store, nil, nil, DefaultBudgets())
	if err := m.AfterTurn(context.Background(), "", "user", "hello", nil); err != nil {
		t.Fatal(err)
	}
	if store.storeCalls != 0 {
		t.Errorf("empty session id should not call StoreMemory")
	}
}

func TestAfterTurn_CompressorPath(t *testing.T) {
	store := &fakeStore{}
	cmp := &fakeCompressor{turnOut: "compressed-text"}
	m := NewManager(store, nil, cmp, DefaultBudgets())
	if err := m.AfterTurn(context.Background(), "s", "user", "very long content", []string{"tool1"}); err != nil {
		t.Fatal(err)
	}
	if len(store.stored) != 1 {
		t.Fatalf("expected 1 stored entry, got %d", len(store.stored))
	}
	if !strings.Contains(store.stored[0].Content, "compressed-text") {
		t.Errorf("compressed content should be stored: %q", store.stored[0].Content)
	}
	if !strings.HasPrefix(store.stored[0].Content, "[user]") {
		t.Errorf("role prefix expected: %q", store.stored[0].Content)
	}
}

func TestAfterTurn_CompressorErrorTruncatesPartialOutput(t *testing.T) {
	// On error, the code keeps whatever the compressor returned (turnOut)
	// and truncates it if longer than 200 chars. We model that exact behavior.
	store := &fakeStore{}
	cmp := &fakeCompressor{turnOut: strings.Repeat("p", 500), turnErr: errors.New("comp fail")}
	m := NewManager(store, nil, cmp, DefaultBudgets())
	if err := m.AfterTurn(context.Background(), "s", "user", "anything", nil); err != nil {
		t.Fatal(err)
	}
	got := store.stored[0].Content
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncated content should end with ...: %q", got)
	}
	if len(got) >= 500 {
		t.Errorf("content should be truncated, got len=%d", len(got))
	}
}

func TestAfterTurn_CompressorErrorShortOutputNoTruncate(t *testing.T) {
	// If the compressor returned a short value alongside its error, no truncation suffix.
	store := &fakeStore{}
	cmp := &fakeCompressor{turnOut: "short", turnErr: errors.New("comp fail")}
	m := NewManager(store, nil, cmp, DefaultBudgets())
	if err := m.AfterTurn(context.Background(), "s", "user", "x", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(store.stored[0].Content, "short") {
		t.Errorf("short compressor output should pass through: %q", store.stored[0].Content)
	}
}

func TestAfterTurn_NoCompressorTruncates(t *testing.T) {
	store := &fakeStore{}
	long := strings.Repeat("y", 500)
	m := NewManager(store, nil, nil, DefaultBudgets())
	if err := m.AfterTurn(context.Background(), "s", "assistant", long, nil); err != nil {
		t.Fatal(err)
	}
	if len(store.stored[0].Content) >= 500 {
		t.Errorf("untruncated content stored: len=%d", len(store.stored[0].Content))
	}
}

func TestAfterTurn_NoCompressorShortPasses(t *testing.T) {
	store := &fakeStore{}
	m := NewManager(store, nil, nil, DefaultBudgets())
	if err := m.AfterTurn(context.Background(), "s", "u", "short", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(store.stored[0].Content, "short") {
		t.Errorf("short content should pass through: %q", store.stored[0].Content)
	}
}

func TestAfterTurn_StoreErrorPropagates(t *testing.T) {
	store := &fakeStore{storeErr: errors.New("disk full")}
	m := NewManager(store, nil, nil, DefaultBudgets())
	err := m.AfterTurn(context.Background(), "s", "u", "x", nil)
	if err == nil || !strings.Contains(err.Error(), "store session memory") {
		t.Errorf("expected wrapped store error, got %v", err)
	}
}

// ── StorePersistentMemory ────────────────────────────────────────────────

func TestStorePersistentMemory_NoStore(t *testing.T) {
	m := NewManager(nil, nil, nil, DefaultBudgets())
	if err := m.StorePersistentMemory(context.Background(), "r", "x", nil); err != nil {
		t.Errorf("no-store path should be no-op, got %v", err)
	}
}

func TestStorePersistentMemory_StoresWithEmbedding(t *testing.T) {
	store := &fakeStore{}
	emb := &fakeEmbedder{vec: []float32{0.1, 0.2}}
	m := NewManager(store, emb, nil, DefaultBudgets())
	if err := m.StorePersistentMemory(context.Background(), "repo", "an insight", []string{"caching"}); err != nil {
		t.Fatal(err)
	}
	if len(store.stored) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(store.stored))
	}
	got := store.stored[0]
	if got.Tier != types.MemoryPersistent {
		t.Errorf("tier should be persistent, got %q", got.Tier)
	}
	if got.RepoSlug != "repo" || got.Content != "an insight" {
		t.Errorf("fields not set correctly: %+v", got)
	}
	if len(got.Embedding) != 2 {
		t.Errorf("embedding should be set: %v", got.Embedding)
	}
}

func TestStorePersistentMemory_EmbedderErrorIsTolerated(t *testing.T) {
	store := &fakeStore{}
	emb := &fakeEmbedder{err: errors.New("embed fail")}
	m := NewManager(store, emb, nil, DefaultBudgets())
	if err := m.StorePersistentMemory(context.Background(), "repo", "insight", nil); err != nil {
		t.Fatalf("embedder error should not block storage, got %v", err)
	}
	if len(store.stored[0].Embedding) != 0 {
		t.Errorf("embedding should be empty on embedder error")
	}
}

// ── compactSessionIfNeeded ───────────────────────────────────────────────

func TestCompactSession_NoStoreOrCompressor(t *testing.T) {
	m1 := NewManager(nil, nil, &fakeCompressor{}, DefaultBudgets())
	if err := m1.compactSessionIfNeeded(context.Background(), "s"); err != nil {
		t.Errorf("nil store should be no-op: %v", err)
	}
	store := &fakeStore{}
	m2 := NewManager(store, nil, nil, DefaultBudgets())
	if err := m2.compactSessionIfNeeded(context.Background(), "s"); err != nil {
		t.Errorf("nil compressor should be no-op: %v", err)
	}
}

func TestCompactSession_ListErrorSwallowed(t *testing.T) {
	store := &fakeStore{listErr: errors.New("list fail")}
	m := NewManager(store, nil, &fakeCompressor{}, DefaultBudgets())
	if err := m.compactSessionIfNeeded(context.Background(), "s"); err != nil {
		t.Errorf("list error should be swallowed: %v", err)
	}
}

func TestCompactSession_WithinBudget(t *testing.T) {
	store := &fakeStore{
		sessionMems: []types.MemoryEntry{
			{Content: "a", TokenCount: 100},
			{Content: "b", TokenCount: 100},
		},
	}
	cmp := &fakeCompressor{}
	m := NewManager(store, nil, cmp, Budgets{WorkingTokens: 1, SessionTokens: 1000, PersistentTokens: 1})
	if err := m.compactSessionIfNeeded(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
}

func TestCompactSession_TooFewToCompact(t *testing.T) {
	// halfIdx = 1 → returns early.
	store := &fakeStore{
		sessionMems: []types.MemoryEntry{
			{Content: "a", TokenCount: 5000},
			{Content: "b", TokenCount: 5000},
		},
	}
	cmp := &fakeCompressor{sessionOut: "compressed"}
	m := NewManager(store, nil, cmp, Budgets{WorkingTokens: 1, SessionTokens: 1, PersistentTokens: 1})
	if err := m.compactSessionIfNeeded(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
}

func TestCompactSession_CompressOldestHalf(t *testing.T) {
	mems := make([]types.MemoryEntry, 6)
	for i := range mems {
		mems[i] = types.MemoryEntry{Content: "x", TokenCount: 100}
	}
	store := &fakeStore{sessionMems: mems}
	cmp := &fakeCompressor{sessionOut: "ultra"}
	m := NewManager(store, nil, cmp, Budgets{WorkingTokens: 1, SessionTokens: 1, PersistentTokens: 1})
	if err := m.compactSessionIfNeeded(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
}

func TestCompactSession_CompressorError(t *testing.T) {
	mems := make([]types.MemoryEntry, 6)
	for i := range mems {
		mems[i] = types.MemoryEntry{Content: "x", TokenCount: 100}
	}
	store := &fakeStore{sessionMems: mems}
	cmp := &fakeCompressor{sessionErr: errors.New("comp fail")}
	m := NewManager(store, nil, cmp, Budgets{WorkingTokens: 1, SessionTokens: 1, PersistentTokens: 1})
	if err := m.compactSessionIfNeeded(context.Background(), "s"); err != nil {
		t.Errorf("compressor error should be swallowed: %v", err)
	}
}

// ── estimateTokens ───────────────────────────────────────────────────────

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abcd", 1},
		{"abcdefgh", 2},
		{"abcde", 1}, // 5/4 = 1
	}
	for _, c := range cases {
		if got := estimateTokens(c.in); got != c.want {
			t.Errorf("estimateTokens(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
