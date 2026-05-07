package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---------------------------------------------------------------------------
// Stub domain implementations for constructing app services in tests
// ---------------------------------------------------------------------------

type httpTestGraphStore struct {
	repo      *types.Repo
	repos     []types.Repo
	node      *types.CodeNode
	upsertErr error
	getErr    error
	listErr   error
	deleteErr error
	blastErr  error
	traceHops []types.TraceHop
}

func (s *httpTestGraphStore) UpsertNode(_ context.Context, _ *types.CodeNode) error {
	return s.upsertErr
}
func (s *httpTestGraphStore) GetNode(_ context.Context, _ string) (*types.CodeNode, error) {
	return s.node, s.getErr
}
func (s *httpTestGraphStore) GetNodeByQualified(_ context.Context, _, _ string) (*types.CodeNode, error) {
	if s.node != nil {
		return s.node, nil
	}
	return nil, domain.NotFound("not found")
}
func (s *httpTestGraphStore) DeleteNode(_ context.Context, _ string) error { return s.deleteErr }
func (s *httpTestGraphStore) DeleteNodesByRepo(_ context.Context, _ string) error {
	return s.deleteErr
}
func (s *httpTestGraphStore) DeleteNodesByFile(_ context.Context, _, _ string) error { return nil }
func (s *httpTestGraphStore) UpsertEdge(_ context.Context, _ *types.CodeEdge) error  { return nil }
func (s *httpTestGraphStore) DeleteEdgesForNode(_ context.Context, _ string) error   { return nil }

func (s *httpTestGraphStore) UpsertFileBatch(_ context.Context, _ []types.CodeNode, _ []types.CodeEdge) error {
	return nil
}
func (s *httpTestGraphStore) UpsertRepo(_ context.Context, _ *types.Repo) error {
	return s.upsertErr
}
func (s *httpTestGraphStore) GetRepo(_ context.Context, _ string) (*types.Repo, error) {
	if s.repo != nil {
		return s.repo, s.getErr
	}
	return nil, domain.NotFound("repo not found")
}
func (s *httpTestGraphStore) ListRepos(_ context.Context) ([]types.Repo, error) {
	return s.repos, s.listErr
}
func (s *httpTestGraphStore) ApplySchema(_ context.Context) error             { return nil }
func (s *httpTestGraphStore) GetSchemaVersion(_ context.Context) (int, error) { return 1, nil }
func (s *httpTestGraphStore) GetNeighborhood(_ context.Context, _ string) (*domain.Neighborhood, error) {
	return &domain.Neighborhood{}, nil
}
func (s *httpTestGraphStore) TraverseGraph(_ context.Context, _ string, _ []string, _ string, _ int) ([]types.TraceHop, error) {
	if s.blastErr != nil {
		return nil, s.blastErr
	}
	return s.traceHops, nil
}
func (s *httpTestGraphStore) TraceDataFlow(_ context.Context, _ string, _ int, _ string) ([]types.TraceHop, error) {
	return nil, nil
}
func (s *httpTestGraphStore) ListNodeIDs(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (s *httpTestGraphStore) ListAllNodes(_ context.Context, _ string) ([]types.CodeNode, error) {
	return nil, nil
}
func (s *httpTestGraphStore) ListFilePaths(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (s *httpTestGraphStore) ListAllEdges(_ context.Context, _ string) ([]types.CodeEdge, error) {
	return nil, nil
}
func (s *httpTestGraphStore) ListNodesByFile(_ context.Context, _, _ string) ([]types.CodeNode, error) {
	return nil, nil
}
func (s *httpTestGraphStore) ListNodesByConcepts(_ context.Context, _ string, _ []string, _ int) ([]types.CodeNode, error) {
	return nil, nil
}
func (s *httpTestGraphStore) UpdateRepoIndexedAt(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (s *httpTestGraphStore) FindRepoByRemoteURL(_ context.Context, _ string) (*types.Repo, error) {
	return nil, nil
}
func (s *httpTestGraphStore) ListRoutes(_ context.Context, _ string) ([]types.CodeEdge, error) {
	return nil, nil
}
func (s *httpTestGraphStore) TraceFieldFlow(_ context.Context, _ string, _ string, _ int, _ string) ([]types.FieldFlowHop, error) {
	return nil, nil
}
func (s *httpTestGraphStore) FindMutations(_ context.Context, _ string, _ string) ([]types.FieldFlowHop, error) {
	return nil, nil
}

// ── OpenCodeGraph adapter methods ──
func (s *httpTestGraphStore) PutNode(ctx context.Context, node *types.CodeNode) error {
	return nil
}
func (s *httpTestGraphStore) FindNode(ctx context.Context, repo, q string) (*types.CodeNode, error) {
	return s.GetNodeByQualified(ctx, repo, q)
}
func (s *httpTestGraphStore) PutEdge(_ context.Context, _ *types.CodeEdge) error { return nil }
func (s *httpTestGraphStore) DeleteEdgesFrom(_ context.Context, _ string) error  { return nil }
func (s *httpTestGraphStore) PutBatch(_ context.Context, _ []types.CodeNode, _ []types.CodeEdge) error {
	return nil
}
func (s *httpTestGraphStore) DeleteByRepo(_ context.Context, _ string) error    { return nil }
func (s *httpTestGraphStore) DeleteByFile(_ context.Context, _, _ string) error { return nil }
func (s *httpTestGraphStore) Neighbors(_ context.Context, _ string) (*domain.Neighborhood, error) {
	return &domain.Neighborhood{}, nil
}
func (s *httpTestGraphStore) GetNodeEmbedding(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}
func (s *httpTestGraphStore) VectorSearch(_ context.Context, _ []float32, _ domain.VectorSearchOpts) ([]types.ScoredNode, error) {
	return nil, nil
}
func (s *httpTestGraphStore) TextSearch(_ context.Context, _ string, _ domain.TextSearchOpts) ([]types.ScoredNode, error) {
	return nil, nil
}
func (s *httpTestGraphStore) ListNodes(_ context.Context, _ string, _ domain.ListOpts) ([]types.CodeNode, error) {
	return nil, nil
}
func (s *httpTestGraphStore) ListEdges(_ context.Context, _ string, _ []string) ([]types.CodeEdge, error) {
	return nil, nil
}
func (s *httpTestGraphStore) PutRepo(ctx context.Context, repo *types.Repo) error {
	return s.UpsertRepo(ctx, repo)
}
func (s *httpTestGraphStore) DeleteRepo(_ context.Context, _ string) error { return nil }

type httpTestEmbedder struct {
	err error
	vec []float32
}

func (s *httpTestEmbedder) EmbedBatch(_ context.Context, _ []domain.EmbedInput) ([]domain.EmbedResult, error) {
	return nil, s.err
}
func (s *httpTestEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return s.vec, s.err
}

type httpTestExplainer struct {
	err    error
	chunks []domain.ExplainChunk
}

func (s *httpTestExplainer) Explain(_ context.Context, _ domain.ExplainRequest) (<-chan domain.ExplainChunk, error) {
	if s.err != nil {
		return nil, s.err
	}
	ch := make(chan domain.ExplainChunk, len(s.chunks)+1)
	for _, c := range s.chunks {
		ch <- c
	}
	ch <- domain.ExplainChunk{Done: true}
	close(ch)
	return ch, nil
}

func (s *httpTestExplainer) ExplainStructured(_ context.Context, _ domain.ExplainRequest) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []byte(`{"overview":"test","evidence":[],"insights":[]}`), nil
}

type httpTestParser struct{}

func (s *httpTestParser) Parse(_ context.Context, _ domain.FileEntry) (*domain.ParsedFile, error) {
	return &domain.ParsedFile{}, nil
}
func (s *httpTestParser) SupportedLanguages() []string { return nil }

type httpTestWalker struct{}

func (s *httpTestWalker) Walk(_ context.Context, _ string, _ domain.WalkOpts) (<-chan domain.FileEntry, <-chan error) {
	ch := make(chan domain.FileEntry)
	errCh := make(chan error, 1)
	close(ch)
	errCh <- nil
	return ch, errCh
}

// ---------------------------------------------------------------------------
// Test server builder
// ---------------------------------------------------------------------------

func newTestServer(store *httpTestGraphStore, embedder *httpTestEmbedder, explainer *httpTestExplainer) *Server {
	cfg := &config.Config{
		Query:     config.QueryConfig{DefaultTopK: 10, RRFKConstant: 60},
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}
	indexSvc := app.NewIndexService(&httpTestWalker{}, &httpTestParser{}, embedder, store, nil, cfg)
	querySvc := app.NewQueryService(embedder, store, explainer, cfg)
	traceSvc := app.NewTraceService(store, embedder, explainer, cfg)
	blastSvc := app.NewBlastService(store, explainer, cfg)
	repoSvc := app.NewRepoService(store)

	serverCfg := &config.ServerConfig{
		Port:            8080,
		CORSOrigins:     []string{"*"},
		ReadTimeoutSec:  30,
		WriteTimeoutSec: 120,
	}
	return NewServer(indexSvc, querySvc, traceSvc, blastSvc, repoSvc, store, nil, nil, nil, nil, nil, serverCfg)
}

func defaultTestServer() *Server {
	store := &httpTestGraphStore{}
	embedder := &httpTestEmbedder{vec: []float32{0.1, 0.2}}
	explainer := &httpTestExplainer{}
	return newTestServer(store, embedder, explainer)
}

// ginCtx creates a Gin test context with the given request.
func ginCtx(req *http.Request) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	return c, rec
}

// ginCtxWithBody creates a Gin test context for a JSON POST/PUT/DELETE request.
func ginCtxWithBody(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return ginCtx(req)
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func TestHandleHealth(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/health", nil))

	srv.handleHealth(c)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ok") {
		t.Errorf("body missing 'ok': %s", rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Query
// ---------------------------------------------------------------------------

func TestHandleQueryMissingQuestion(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/query", `{"repo_slug":"r"}`)

	srv.handleQuery(c)
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestHandleQueryInvalidBody(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/query", "{bad json")

	srv.handleQuery(c)
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestHandleQueryServiceError(t *testing.T) {
	store := &httpTestGraphStore{}
	embedder := &httpTestEmbedder{err: errors.New("embed fail")}
	explainer := &httpTestExplainer{}
	srv := newTestServer(store, embedder, explainer)
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/query", `{"question":"where is auth?","repo_slug":"r"}`)

	srv.handleQuery(c)

	if rec.Code == http.StatusOK {
		t.Error("expected error status from service, got 200")
	}
}

func TestHandleQuerySuccess(t *testing.T) {
	store := &httpTestGraphStore{}
	embedder := &httpTestEmbedder{vec: []float32{0.1}}
	explainer := &httpTestExplainer{}
	srv := newTestServer(store, embedder, explainer)
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/query", `{"question":"where is auth?","repo_slug":"r","top_k":5}`)

	srv.handleQuery(c)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Trace
// ---------------------------------------------------------------------------

func TestHandleTraceMissingSymbol(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/trace", `{"direction":"forward"}`)

	srv.handleTrace(c)
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestHandleTraceInvalidBody(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/trace", "{bad")

	srv.handleTrace(c)
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestHandleTraceServiceError(t *testing.T) {
	store := &httpTestGraphStore{getErr: domain.NotFound("sym not found")}
	embedder := &httpTestEmbedder{err: errors.New("embed fail")}
	srv := newTestServer(store, embedder, &httpTestExplainer{})
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/trace", `{"symbol":"pkg.F","repo_slug":"r"}`)

	srv.handleTrace(c)

	if rec.Code == http.StatusOK {
		t.Error("expected error status from service")
	}
}

func TestHandleTraceSuccess(t *testing.T) {
	store := &httpTestGraphStore{
		node: &types.CodeNode{ID: "function:pkg⋅F", Qualified: "pkg.F"},
	}
	embedder := &httpTestEmbedder{vec: []float32{0.1}}
	explainer := &httpTestExplainer{chunks: []domain.ExplainChunk{{Text: "explanation"}}}
	srv := newTestServer(store, embedder, explainer)
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/trace", `{"symbol":"pkg.F","repo_slug":"r","direction":"forward","depth":2}`)

	srv.handleTrace(c)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleTraceDefaultsApplied(t *testing.T) {
	store := &httpTestGraphStore{
		node: &types.CodeNode{ID: "function:pkg⋅G", Qualified: "pkg.G"},
	}
	srv := newTestServer(store, &httpTestEmbedder{vec: []float32{0.1}}, &httpTestExplainer{})
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/trace", `{"symbol":"pkg.G","repo_slug":"r"}`)

	srv.handleTrace(c)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Blast
// ---------------------------------------------------------------------------

func TestHandleBlastMissingSymbol(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/blast", `{"repo_slug":"r"}`)

	srv.handleBlast(c)
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestHandleBlastInvalidBody(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/blast", "{bad")

	srv.handleBlast(c)
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestHandleBlastSuccess(t *testing.T) {
	store := &httpTestGraphStore{
		node: &types.CodeNode{ID: "function:pkg⋅F", Qualified: "pkg.F"},
	}
	srv := newTestServer(store, &httpTestEmbedder{vec: []float32{0.1}}, &httpTestExplainer{})
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/blast", `{"symbol":"pkg.F","repo_slug":"r","max_depth":3}`)

	srv.handleBlast(c)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Repos
// ---------------------------------------------------------------------------

func TestHandleListRepos_Empty(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil))

	srv.handleListRepos(c)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "[") {
		t.Errorf("expected JSON array, got: %s", rec.Body.String())
	}
}

func TestHandleListRepos_Error(t *testing.T) {
	store := &httpTestGraphStore{listErr: errors.New("db error")}
	srv := newTestServer(store, &httpTestEmbedder{}, &httpTestExplainer{})
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil))

	srv.handleListRepos(c)

	if rec.Code == http.StatusOK {
		t.Error("expected error status")
	}
}

func TestHandleCreateRepo_MissingSlug(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/repos", `{"path":"/repo"}`)

	srv.handleCreateRepo(c)
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestHandleCreateRepo_MissingPath(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/repos", `{"slug":"my-repo"}`)

	srv.handleCreateRepo(c)
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestHandleCreateRepo_InvalidBody(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/repos", "{bad")

	srv.handleCreateRepo(c)
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestHandleCreateRepo_Success(t *testing.T) {
	store := &httpTestGraphStore{}
	srv := newTestServer(store, &httpTestEmbedder{}, &httpTestExplainer{})
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/repos", `{"slug":"my-repo","path":"/tmp/repo"}`)

	srv.handleCreateRepo(c)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
}

func TestHandleCreateRepo_ConflictError(t *testing.T) {
	store := &httpTestGraphStore{upsertErr: domain.Conflict("already exists")}
	srv := newTestServer(store, &httpTestEmbedder{}, &httpTestExplainer{})
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/repos", `{"slug":"dup","path":"/tmp"}`)

	srv.handleCreateRepo(c)
	assertStatus(t, rec, http.StatusConflict)
}

func TestHandleGetRepo_Success(t *testing.T) {
	store := &httpTestGraphStore{repo: &types.Repo{Slug: "my-repo", Path: "/tmp"}}
	srv := newTestServer(store, &httpTestEmbedder{}, &httpTestExplainer{})
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/api/v1/repos/my-repo", nil))
	c.AddParam("slug", "my-repo")

	srv.handleGetRepo(c)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleGetRepo_NotFound(t *testing.T) {
	store := &httpTestGraphStore{getErr: domain.NotFound("not found")}
	srv := newTestServer(store, &httpTestEmbedder{}, &httpTestExplainer{})
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/api/v1/repos/missing", nil))
	c.AddParam("slug", "missing")

	srv.handleGetRepo(c)
	assertStatus(t, rec, http.StatusNotFound)
}

func TestHandleDeleteRepo_Success(t *testing.T) {
	store := &httpTestGraphStore{repo: &types.Repo{Slug: "bye-repo", Path: "/tmp"}}
	srv := newTestServer(store, &httpTestEmbedder{}, &httpTestExplainer{})
	c, rec := ginCtx(httptest.NewRequest(http.MethodDelete, "/api/v1/repos/bye-repo", nil))
	c.AddParam("slug", "bye-repo")

	srv.handleDeleteRepo(c)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleDeleteRepo_NotFound(t *testing.T) {
	store := &httpTestGraphStore{}
	srv := newTestServer(store, &httpTestEmbedder{}, &httpTestExplainer{})
	c, rec := ginCtx(httptest.NewRequest(http.MethodDelete, "/api/v1/repos/ghost", nil))
	c.AddParam("slug", "ghost")

	srv.handleDeleteRepo(c)
	assertStatus(t, rec, http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// writeError mapping
// ---------------------------------------------------------------------------

func TestWriteErrorNotFound(t *testing.T) {
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/", nil))
	writeError(c, domain.NotFound("missing thing"))
	assertStatus(t, rec, http.StatusNotFound)
}

func TestWriteErrorValidation(t *testing.T) {
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/", nil))
	writeError(c, domain.Validation("bad input"))
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestWriteErrorConflict(t *testing.T) {
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/", nil))
	writeError(c, domain.Conflict("already exists"))
	assertStatus(t, rec, http.StatusConflict)
}

func TestWriteErrorGeneric(t *testing.T) {
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/", nil))
	writeError(c, errors.New("internal failure"))
	assertStatus(t, rec, http.StatusInternalServerError)
}

func TestWriteErrorRateLimit(t *testing.T) {
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/", nil))
	writeError(c, domain.RateLimit("too fast"))
	assertStatus(t, rec, http.StatusInternalServerError)
}

// ---------------------------------------------------------------------------
// writeSSE
// ---------------------------------------------------------------------------

func TestWriteSSE(t *testing.T) {
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/", nil))

	writeSSE(c, "hop", map[string]string{"id": "n1"})
	body := rec.Body.String()
	if !strings.Contains(body, "event: hop") {
		t.Errorf("missing event: hop in: %q", body)
	}
	if !strings.Contains(body, "data:") {
		t.Errorf("missing data: in: %q", body)
	}
}

// ---------------------------------------------------------------------------
// Server construction + middleware
// ---------------------------------------------------------------------------

func TestNewServer_RegistersRoutes(t *testing.T) {
	srv := defaultTestServer()
	if srv.router == nil {
		t.Fatal("router is nil")
	}
	if srv.trackers == nil {
		t.Fatal("trackers store is nil")
	}
}

func TestSlogMiddleware(t *testing.T) {
	r := gin.New()
	r.Use(SlogMiddleware(slog.Default()))
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Index job handlers
// ---------------------------------------------------------------------------

func TestHandleStartIndex_MissingRepoPath(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/index", `{"repo_slug":"r"}`)

	srv.handleStartIndex(c)
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestHandleStartIndex_MissingRepoSlug(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/index", `{"repo_path":"/tmp/repo"}`)

	srv.handleStartIndex(c)
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestHandleStartIndex_InvalidBody(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/index", "{bad")

	srv.handleStartIndex(c)
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestHandleStartIndex_Success(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/index", `{"repo_path":"/tmp/repo","repo_slug":"test-repo"}`)

	srv.handleStartIndex(c)

	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", rec.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["job_id"] == "" {
		t.Error("job_id should not be empty")
	}
}

func TestHandleIndexStatus_NotFound(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/api/v1/index/nonexistent", nil))
	c.AddParam("job_id", "nonexistent")

	srv.handleIndexStatus(c)
	assertStatus(t, rec, http.StatusNotFound)
}

func TestHandleIndexStatus_Found(t *testing.T) {
	srv := defaultTestServer()

	// First start an index job to get a valid job_id.
	startC, startRec := ginCtxWithBody(http.MethodPost, "/api/v1/index", `{"repo_path":"/tmp/x","repo_slug":"s"}`)
	srv.handleStartIndex(startC)

	var startResp map[string]string
	json.NewDecoder(startRec.Body).Decode(&startResp)
	jobID := startResp["job_id"]

	// Now poll status.
	statusC, statusRec := ginCtx(httptest.NewRequest(http.MethodGet, "/api/v1/index/"+jobID, nil))
	statusC.AddParam("job_id", jobID)

	srv.handleIndexStatus(statusC)

	if statusRec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", statusRec.Code)
	}
}

// ---------------------------------------------------------------------------
// indexTrackerStore
// ---------------------------------------------------------------------------

func TestIndexTrackerStore_SetGet(t *testing.T) {
	store := newIndexTrackerStore()

	tracker := app.NewIndexTracker("j1", "repo/test", types.IndexConfig{EmbedProvider: "ollama"})
	store.set("j1", tracker)

	got, ok := store.get("j1")
	if !ok {
		t.Fatal("tracker not found")
	}
	snap := got.Snapshot()
	if snap.Status != "indexing" {
		t.Errorf("status = %q, want indexing", snap.Status)
	}
	if snap.Config.EmbedProvider != "ollama" {
		t.Errorf("embed_provider = %q, want ollama", snap.Config.EmbedProvider)
	}
}

func TestIndexTrackerStore_GetMissing(t *testing.T) {
	store := newIndexTrackerStore()
	_, ok := store.get("missing")
	if ok {
		t.Error("get should return false for missing tracker")
	}
}

func TestNewJobID(t *testing.T) {
	id1, err := newJobID()
	if err != nil {
		t.Fatalf("newJobID: %v", err)
	}
	if len(id1) == 0 {
		t.Error("job ID should not be empty")
	}

	id2, _ := newJobID()
	if id1 == id2 {
		t.Error("two job IDs should not be identical (crypto/rand)")
	}
}

func TestHandleBlastServiceError(t *testing.T) {
	store := &httpTestGraphStore{
		node:     &types.CodeNode{ID: "function:pkg⋅F", Qualified: "pkg.F"},
		blastErr: domain.NotFound("symbol not found"),
	}
	srv := newTestServer(store, &httpTestEmbedder{vec: []float32{0.1}}, &httpTestExplainer{})
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/blast", `{"symbol":"pkg.F","repo_slug":"r"}`)

	srv.handleBlast(c)

	if rec.Code == http.StatusOK {
		t.Error("expected error from blast service")
	}
}

func TestHandleTraceSSEHopsAndExplanation(t *testing.T) {
	store := &httpTestGraphStore{
		node: &types.CodeNode{ID: "function:pkg⋅F", Qualified: "pkg.F"},
		traceHops: []types.TraceHop{
			{Depth: 1, Node: types.CodeNode{ID: "function:pkg⋅G", Qualified: "pkg.G"}},
		},
	}
	explainer := &httpTestExplainer{chunks: []domain.ExplainChunk{{Text: "walk through"}}}
	srv := newTestServer(store, &httpTestEmbedder{vec: []float32{0.1}}, explainer)
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/trace", `{"symbol":"pkg.F","repo_slug":"r","direction":"forward","depth":1}`)

	srv.handleTrace(c)

	out := rec.Body.String()
	if !strings.Contains(out, "hop") {
		t.Errorf("missing 'hop' event in SSE output: %q", out)
	}
}

func TestHandleTraceSSEExplanation(t *testing.T) {
	store := &httpTestGraphStore{
		node: &types.CodeNode{ID: "function:pkg⋅F", Qualified: "pkg.F"},
	}
	explainer := &httpTestExplainer{chunks: []domain.ExplainChunk{{Text: "step1"}, {Text: "step2"}}}
	srv := newTestServer(store, &httpTestEmbedder{vec: []float32{0.1}}, explainer)
	c, rec := ginCtxWithBody(http.MethodPost, "/api/v1/trace", `{"symbol":"pkg.F","repo_slug":"r","direction":"forward","depth":1}`)

	srv.handleTrace(c)

	bodyOut := rec.Body.String()
	if !strings.Contains(bodyOut, "done") {
		t.Errorf("missing 'done' event in SSE output: %q", bodyOut)
	}
}

// ---------------------------------------------------------------------------
// Server Start / Shutdown
// ---------------------------------------------------------------------------

func TestServerShutdown(t *testing.T) {
	srv := defaultTestServer()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func TestServerStart_InvalidPort(t *testing.T) {
	srv := defaultTestServer()
	srv.cfg = &config.ServerConfig{Port: -1}
	err := srv.Start()
	if err == nil {
		t.Error("Start with invalid port should fail")
	}
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

func TestSlogMiddlewareReturnNonNil(t *testing.T) {
	mw := SlogMiddleware(nil)
	if mw == nil {
		t.Error("SlogMiddleware(nil) returned nil")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// assertStatus checks that the response recorder has the expected HTTP status code.
func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, code int) {
	t.Helper()
	if rec.Code != code {
		t.Errorf("HTTP status = %d, want %d; body: %s", rec.Code, code, rec.Body.String())
	}
}

// Ensure json import is used.
var _ = json.Marshal
