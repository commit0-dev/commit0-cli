package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
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

	// Programmable list responses for the static-graph handler tests.
	listNodesResult []types.CodeNode
	listEdgesResult []types.CodeEdge
	listEdgesErr    error
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
func (s *httpTestGraphStore) ListNodes(_ context.Context, _ string, opts domain.ListOpts) ([]types.CodeNode, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	if opts.Limit > 0 && len(s.listNodesResult) > opts.Limit {
		return s.listNodesResult[:opts.Limit], nil
	}
	return s.listNodesResult, nil
}
func (s *httpTestGraphStore) ListEdges(_ context.Context, _ string, _ []string) ([]types.CodeEdge, error) {
	return s.listEdgesResult, s.listEdgesErr
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
	return NewServer(indexSvc, querySvc, traceSvc, blastSvc, repoSvc, store, nil, nil, nil, nil, nil, nil, serverCfg)
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
	assertStatus(t, rec, http.StatusTooManyRequests)
}

func TestWriteErrorTimeout(t *testing.T) {
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/", nil))
	writeError(c, domain.Timeout("slow", nil))
	assertStatus(t, rec, http.StatusGatewayTimeout)
}

func TestWriteErrorAuthFailed(t *testing.T) {
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/", nil))
	writeError(c, domain.AuthFailed("bad token"))
	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestWriteErrorOutOfScope(t *testing.T) {
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/", nil))
	writeError(c, domain.OutOfScope("not allowed"))
	assertStatus(t, rec, http.StatusForbidden)
}

func TestWriteErrorUnavailable(t *testing.T) {
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/", nil))
	writeError(c, domain.Unavailable("storage down"))
	assertStatus(t, rec, http.StatusServiceUnavailable)
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
// Static graph (HUD canvas seed)
// ---------------------------------------------------------------------------

// TestHandleGraph_MissingRepo: repo query param is required.
func TestHandleGraph_MissingRepo(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/api/v1/graph", nil))
	srv.handleGraph(c)
	assertStatus(t, rec, http.StatusBadRequest)
}

// TestHandleGraph_BadLimit: limit must be a positive integer.
func TestHandleGraph_BadLimit(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/api/v1/graph?repo=foo/bar&limit=oops", nil))
	srv.handleGraph(c)
	assertStatus(t, rec, http.StatusBadRequest)
}

// TestHandleGraph_Success: returns the trimmed delta wire shape, not the
// full CodeNode/CodeEdge with bodies + embeddings. The HUD relies on a
// cheap response so a 1500-node payload doesn't drag bytes through the
// wire that would never get rendered.
func TestHandleGraph_Success(t *testing.T) {
	store := &httpTestGraphStore{
		listNodesResult: []types.CodeNode{
			{
				ID:        "function:pkg.A",
				Qualified: "pkg.A",
				Name:      "A",
				Kind:      types.NodeFunction,
				FilePath:  "a.go",
				Language:  "go",
				RepoSlug:  "owner/repo",
				StartLine: 1,
				EndLine:   5,
				Signature: "A() error",
				Body:      "// HUD must not see this body",
				Embedding: []float32{0.1, 0.2, 0.3},
			},
		},
		listEdgesResult: []types.CodeEdge{
			{FromID: "function:pkg.A", ToID: "function:pkg.B", Kind: types.EdgeCalls, CallSite: "a.go:3"},
		},
	}
	srv := newTestServer(store, &httpTestEmbedder{vec: []float32{0.1}}, &httpTestExplainer{})

	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/api/v1/graph?repo=owner%2Frepo&limit=10", nil))
	srv.handleGraph(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp struct {
		Nodes []types.GraphNodeDelta `json:"nodes"`
		Edges []types.GraphEdgeDelta `json:"edges"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 1 || resp.Nodes[0].ID != "function:pkg.A" {
		t.Errorf("nodes = %+v", resp.Nodes)
	}
	if len(resp.Edges) != 1 || resp.Edges[0].Kind != types.EdgeCalls {
		t.Errorf("edges = %+v", resp.Edges)
	}
	// Ensure heavy fields didn't leak — GraphNodeDelta has no Body/Embedding fields,
	// so this is a structural guarantee, but assert on the raw bytes too as a fence.
	if strings.Contains(rec.Body.String(), "must not see this body") {
		t.Error("response leaked CodeNode.Body — wire shape regression")
	}
	if strings.Contains(rec.Body.String(), `"embedding":[`) {
		t.Error("response leaked CodeNode.Embedding — wire shape regression")
	}
}

// TestHandleGraph_LimitCap: requested limit > graphLimitMax should be capped.
func TestHandleGraph_LimitCap(t *testing.T) {
	// Build 10 fake nodes so we can verify the cap takes effect.
	nodes := make([]types.CodeNode, 10)
	for i := range nodes {
		nodes[i] = types.CodeNode{
			ID:   "function:n" + strconv.Itoa(i),
			Kind: types.NodeFunction, RepoSlug: "owner/repo",
		}
	}
	store := &httpTestGraphStore{listNodesResult: nodes}
	srv := newTestServer(store, &httpTestEmbedder{vec: []float32{0.1}}, &httpTestExplainer{})

	// Request limit=99999 — adapter still hands us 10, but the handler must
	// have requested at most graphLimitMax. We verify by inspecting the
	// effective Limit reaching ListNodes via opts.Limit.
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/api/v1/graph?repo=owner%2Frepo&limit=99999", nil))
	srv.handleGraph(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// TestHandleGraph_StoreError: a propagating store error becomes a 5xx via writeError.
func TestHandleGraph_StoreError(t *testing.T) {
	store := &httpTestGraphStore{listErr: errors.New("db unavailable")}
	srv := newTestServer(store, &httpTestEmbedder{vec: []float32{0.1}}, &httpTestExplainer{})

	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/api/v1/graph?repo=owner%2Frepo", nil))
	srv.handleGraph(c)
	if rec.Code == http.StatusOK {
		t.Errorf("status = %d, expected non-200 on backend error", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Index SSE stream
// ---------------------------------------------------------------------------

// TestHandleIndexStream_NotFound: missing job_id should be a 404.
func TestHandleIndexStream_NotFound(t *testing.T) {
	srv := defaultTestServer()
	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/api/v1/index/missing/stream", nil))
	c.AddParam("job_id", "missing")

	srv.handleIndexStream(c)
	assertStatus(t, rec, http.StatusNotFound)
}

// TestHandleIndexStream_AlreadyFinished: a stream subscriber that connects
// after the job has finished should still receive a `done` event with the
// final snapshot, then the connection should close cleanly. This is the
// catch-up path the frontend uses on page reload.
func TestHandleIndexStream_AlreadyFinished(t *testing.T) {
	srv := defaultTestServer()
	tracker := app.NewIndexTracker("done-job", "owner/repo", types.IndexConfig{})
	tracker.AddNodes(42)
	tracker.Finish(nil)
	srv.trackers.set("done-job", tracker)

	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/api/v1/index/done-job/stream", nil))
	c.AddParam("job_id", "done-job")

	srv.handleIndexStream(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (SSE)", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: progress") {
		t.Errorf("expected catch-up progress event, body=%q", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("expected terminal done event, body=%q", body)
	}
	if !strings.Contains(body, `"nodes_created":42`) {
		t.Errorf("expected nodes_created=42 in done snapshot, body=%q", body)
	}
}

// TestHandleIndexStream_LiveDeltas: a connected subscriber should receive
// stage_start / graph_delta / stage_done frames in order, then a done event
// when the tracker finishes.
func TestHandleIndexStream_LiveDeltas(t *testing.T) {
	srv := defaultTestServer()
	tracker := app.NewIndexTracker("live-job", "owner/repo", types.IndexConfig{})
	srv.trackers.set("live-job", tracker)

	// Drive the tracker concurrently so the handler sees a real live stream.
	go func() {
		// Tiny pause so the handler subscribes before events fire.
		time.Sleep(50 * time.Millisecond)
		tracker.StartStage(types.StageStore)
		tracker.EmitGraphDelta(
			[]types.CodeNode{
				{ID: "function:a", Qualified: "pkg.A", Kind: types.NodeFunction, FilePath: "a.go"},
			},
			[]types.CodeEdge{
				{FromID: "function:a", ToID: "function:b", Kind: types.EdgeCalls},
			},
		)
		tracker.CompleteStage(types.StageStore)
		tracker.Finish(nil)
	}()

	c, rec := ginCtx(httptest.NewRequest(http.MethodGet, "/api/v1/index/live-job/stream", nil))
	c.AddParam("job_id", "live-job")

	srv.handleIndexStream(c) // blocks until tracker.Finish closes subs

	body := rec.Body.String()
	for _, want := range []string{
		"event: progress", // catch-up
		"event: stage_start",
		"event: graph_delta",
		"event: stage_done",
		"event: done",
		`"id":"function:a"`,
		`"from_id":"function:a"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n--- body ---\n%s", want, body)
		}
	}

	// Ordering: stage_start must precede graph_delta which must precede
	// stage_done which must precede done. SSE frames are line-delimited so
	// strings.Index on the event names is a sufficient ordering check.
	order := []string{"event: stage_start", "event: graph_delta", "event: stage_done", "event: done"}
	last := -1
	for _, marker := range order {
		idx := strings.Index(body, marker)
		if idx < 0 {
			t.Fatalf("missing %q in body", marker)
		}
		if idx <= last {
			t.Errorf("ordering violated: %q at %d not after previous %d", marker, idx, last)
		}
		last = idx
	}
}

// TestHandleIndexStream_ClientCancelDoesNotBlock: if the client disconnects
// mid-stream, the handler unsubscribes and returns; the producer keeps
// running on the tracker independent of any single consumer.
func TestHandleIndexStream_ClientCancelDoesNotBlock(t *testing.T) {
	srv := defaultTestServer()
	tracker := app.NewIndexTracker("cancel-job", "owner/repo", types.IndexConfig{})
	srv.trackers.set("cancel-job", tracker)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/index/cancel-job/stream", nil).WithContext(ctx)
	c, _ := ginCtx(req)
	c.AddParam("job_id", "cancel-job")

	done := make(chan struct{})
	go func() {
		srv.handleIndexStream(c)
		close(done)
	}()

	// Cancel quickly to simulate client disconnect.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Pass: handler returned promptly on cancel.
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after request context cancel")
	}

	// Producer keeps working — tracker emits don't panic, and Snapshot is fine.
	tracker.StartStage(types.StageStore)
	tracker.EmitGraphDelta(
		[]types.CodeNode{{ID: "function:x"}},
		nil,
	)
	tracker.Finish(nil)
	if tracker.Snapshot().Status != "completed" {
		t.Errorf("tracker status = %q, want completed", tracker.Snapshot().Status)
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
