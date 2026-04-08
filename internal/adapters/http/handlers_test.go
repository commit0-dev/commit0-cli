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

	"github.com/labstack/echo/v4"

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

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
	affected  []types.AffectedNode
}

func (s *httpTestGraphStore) UpsertNode(ctx context.Context, n *types.CodeNode) error {
	return s.upsertErr
}
func (s *httpTestGraphStore) GetNode(ctx context.Context, id string) (*types.CodeNode, error) {
	return s.node, s.getErr
}
func (s *httpTestGraphStore) GetNodeByQualified(ctx context.Context, repo, q string) (*types.CodeNode, error) {
	if s.node != nil {
		return s.node, nil
	}
	return nil, domain.NotFound("not found")
}
func (s *httpTestGraphStore) DeleteNode(ctx context.Context, id string) error { return s.deleteErr }
func (s *httpTestGraphStore) DeleteNodesByRepo(ctx context.Context, r string) error {
	return s.deleteErr
}
func (s *httpTestGraphStore) UpsertEdge(ctx context.Context, e *types.CodeEdge) error { return nil }
func (s *httpTestGraphStore) DeleteEdgesForNode(ctx context.Context, id string) error { return nil }
func (s *httpTestGraphStore) TraceForward(ctx context.Context, id string, d int) ([]types.TraceHop, error) {
	return s.traceHops, nil
}
func (s *httpTestGraphStore) TraceReverse(ctx context.Context, id string, d int) ([]types.TraceHop, error) {
	return s.traceHops, nil
}
func (s *httpTestGraphStore) BlastRadius(ctx context.Context, id string, depth int) ([]types.AffectedNode, error) {
	return s.affected, s.blastErr
}
func (s *httpTestGraphStore) UpsertFileBatch(ctx context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error {
	return nil
}
func (s *httpTestGraphStore) UpsertRepo(ctx context.Context, r *types.Repo) error {
	return s.upsertErr
}
func (s *httpTestGraphStore) GetRepo(ctx context.Context, slug string) (*types.Repo, error) {
	if s.repo != nil {
		return s.repo, s.getErr
	}
	return nil, domain.NotFound("repo not found")
}
func (s *httpTestGraphStore) ListRepos(ctx context.Context) ([]types.Repo, error) {
	return s.repos, s.listErr
}
func (s *httpTestGraphStore) ApplySchema(ctx context.Context) error             { return nil }
func (s *httpTestGraphStore) GetSchemaVersion(ctx context.Context) (int, error) { return 1, nil }
func (s *httpTestGraphStore) GetNeighborhood(_ context.Context, _ string) (*domain.Neighborhood, error) {
	return &domain.Neighborhood{}, nil
}
func (s *httpTestGraphStore) TraceDataFlow(_ context.Context, _ string, _ int, _ string) ([]types.TraceHop, error) {
	return nil, nil
}
func (s *httpTestGraphStore) ListNodeIDs(_ context.Context, _ string) ([]string, error) {
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

func (s *httpTestGraphStore) TraceFieldFlow(_ context.Context, _ string, _ string, _ int, _ string) ([]types.FieldFlowHop, error) {
	return nil, nil
}

func (s *httpTestGraphStore) FindMutations(_ context.Context, _ string, _ string) ([]types.FieldFlowHop, error) {
	return nil, nil
}

type httpTestVectorIndex struct {
	err     error
	results []types.ScoredNode
}

func (s *httpTestVectorIndex) Search(ctx context.Context, q []float32, opts domain.VectorSearchOpts) ([]types.ScoredNode, error) {
	return s.results, s.err
}

type httpTestTextIndex struct {
	err     error
	results []types.ScoredNode
}

func (s *httpTestTextIndex) Search(ctx context.Context, q string, opts domain.TextSearchOpts) ([]types.ScoredNode, error) {
	return s.results, s.err
}

type httpTestEmbedder struct {
	err error
	vec []float32
}

func (s *httpTestEmbedder) EmbedBatch(ctx context.Context, inputs []domain.EmbedInput) ([]domain.EmbedResult, error) {
	return nil, s.err
}
func (s *httpTestEmbedder) EmbedQuery(ctx context.Context, q string) ([]float32, error) {
	return s.vec, s.err
}

type httpTestExplainer struct {
	err    error
	chunks []domain.ExplainChunk
}

func (s *httpTestExplainer) Explain(ctx context.Context, req domain.ExplainRequest) (<-chan domain.ExplainChunk, error) {
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

func (s *httpTestParser) Parse(ctx context.Context, f domain.FileEntry) (*domain.ParsedFile, error) {
	return &domain.ParsedFile{}, nil
}
func (s *httpTestParser) SupportedLanguages() []string { return nil }

type httpTestWalker struct{}

func (s *httpTestWalker) Walk(ctx context.Context, p string, opts domain.WalkOpts) (<-chan domain.FileEntry, <-chan error) {
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
		Query: config.QueryConfig{DefaultTopK: 10, RRFKConstant: 60},
		Index: config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1, BatchSize: 10},
	}
	vi := &httpTestVectorIndex{}
	ti := &httpTestTextIndex{}

	indexSvc := app.NewIndexService(&httpTestWalker{}, &httpTestParser{}, embedder, store, nil, cfg)
	querySvc := app.NewQueryService(embedder, vi, ti, store, explainer, cfg)
	traceSvc := app.NewTraceService(store, embedder, vi, explainer, cfg)
	blastSvc := app.NewBlastService(store, explainer, cfg)
	repoSvc := app.NewRepoService(store)

	serverCfg := &config.ServerConfig{
		Port:            8080,
		CORSOrigins:     []string{"*"},
		ReadTimeoutSec:  30,
		WriteTimeoutSec: 120,
	}
	return NewServer(indexSvc, querySvc, traceSvc, blastSvc, repoSvc, store, nil, nil, nil, nil, serverCfg)
}

func defaultTestServer() *Server {
	store := &httpTestGraphStore{}
	embedder := &httpTestEmbedder{vec: []float32{0.1, 0.2}}
	explainer := &httpTestExplainer{}
	return newTestServer(store, embedder, explainer)
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func TestHandleHealth(t *testing.T) {
	srv := defaultTestServer()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := srv.handleHealth(c); err != nil {
		t.Fatalf("handleHealth: %v", err)
	}
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
	e := echo.New()
	body := `{"repo_slug":"r"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query", strings.NewReader(body))
	req.Header.Set(echo.MIMEApplicationJSON, "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Request().Header.Set("Content-Type", "application/json")

	err := srv.handleQuery(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleQueryInvalidBody(t *testing.T) {
	srv := defaultTestServer()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleQuery(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleQueryServiceError(t *testing.T) {
	store := &httpTestGraphStore{}
	embedder := &httpTestEmbedder{err: errors.New("embed fail")}
	explainer := &httpTestExplainer{}
	srv := newTestServer(store, embedder, explainer)
	e := echo.New()
	body := `{"question":"where is auth?","repo_slug":"r"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleQuery(c)
	if err == nil {
		t.Error("expected error from service, got nil")
	}
}

func TestHandleQuerySuccess(t *testing.T) {
	store := &httpTestGraphStore{}
	embedder := &httpTestEmbedder{vec: []float32{0.1}}
	explainer := &httpTestExplainer{}
	srv := newTestServer(store, embedder, explainer)
	e := echo.New()
	body := `{"question":"where is auth?","repo_slug":"r","top_k":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := srv.handleQuery(c); err != nil {
		t.Fatalf("handleQuery: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Trace
// ---------------------------------------------------------------------------

func TestHandleTraceMissingSymbol(t *testing.T) {
	srv := defaultTestServer()
	e := echo.New()
	body := `{"direction":"forward"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trace", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleTrace(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleTraceInvalidBody(t *testing.T) {
	srv := defaultTestServer()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trace", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleTrace(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleTraceServiceError(t *testing.T) {
	store := &httpTestGraphStore{getErr: domain.NotFound("sym not found")}
	embedder := &httpTestEmbedder{err: errors.New("embed fail")}
	srv := newTestServer(store, embedder, &httpTestExplainer{})
	e := echo.New()
	body := `{"symbol":"pkg.F","repo_slug":"r"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trace", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleTrace(c)
	if err == nil {
		t.Error("expected error from service")
	}
}

func TestHandleTraceSuccess(t *testing.T) {
	store := &httpTestGraphStore{
		node: &types.CodeNode{ID: "function:pkg⋅F", Qualified: "pkg.F"},
	}
	embedder := &httpTestEmbedder{vec: []float32{0.1}}
	explainer := &httpTestExplainer{chunks: []domain.ExplainChunk{{Text: "explanation"}}}
	srv := newTestServer(store, embedder, explainer)
	e := echo.New()
	body := `{"symbol":"pkg.F","repo_slug":"r","direction":"forward","depth":2}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trace", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := srv.handleTrace(c); err != nil {
		t.Fatalf("handleTrace: %v", err)
	}
	// SSE response: status 200 and event-stream content type
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleTraceDefaultsApplied(t *testing.T) {
	// direction="" → "forward", depth=0 → 5
	store := &httpTestGraphStore{
		node: &types.CodeNode{ID: "function:pkg⋅G", Qualified: "pkg.G"},
	}
	srv := newTestServer(store, &httpTestEmbedder{vec: []float32{0.1}}, &httpTestExplainer{})
	e := echo.New()
	body := `{"symbol":"pkg.G","repo_slug":"r"}` // no direction, no depth
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trace", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := srv.handleTrace(c); err != nil {
		t.Fatalf("handleTrace with defaults: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Blast
// ---------------------------------------------------------------------------

func TestHandleBlastMissingSymbol(t *testing.T) {
	srv := defaultTestServer()
	e := echo.New()
	body := `{"repo_slug":"r"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/blast", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleBlast(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleBlastInvalidBody(t *testing.T) {
	srv := defaultTestServer()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/blast", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleBlast(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleBlastSuccess(t *testing.T) {
	store := &httpTestGraphStore{
		node: &types.CodeNode{ID: "function:pkg⋅F", Qualified: "pkg.F"},
	}
	srv := newTestServer(store, &httpTestEmbedder{vec: []float32{0.1}}, &httpTestExplainer{})
	e := echo.New()
	body := `{"symbol":"pkg.F","repo_slug":"r","max_depth":3}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/blast", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := srv.handleBlast(c); err != nil {
		t.Fatalf("handleBlast: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Repos
// ---------------------------------------------------------------------------

func TestHandleListRepos_Empty(t *testing.T) {
	srv := defaultTestServer()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := srv.handleListRepos(c); err != nil {
		t.Fatalf("handleListRepos: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	// Empty list → "[]" not null
	if !strings.Contains(rec.Body.String(), "[") {
		t.Errorf("expected JSON array, got: %s", rec.Body.String())
	}
}

func TestHandleListRepos_Error(t *testing.T) {
	store := &httpTestGraphStore{listErr: errors.New("db error")}
	srv := newTestServer(store, &httpTestEmbedder{}, &httpTestExplainer{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleListRepos(c)
	if err == nil {
		t.Error("expected error")
	}
}

func TestHandleCreateRepo_MissingSlug(t *testing.T) {
	srv := defaultTestServer()
	e := echo.New()
	body := `{"path":"/repo"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleCreateRepo(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleCreateRepo_MissingPath(t *testing.T) {
	srv := defaultTestServer()
	e := echo.New()
	body := `{"slug":"my-repo"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleCreateRepo(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleCreateRepo_InvalidBody(t *testing.T) {
	srv := defaultTestServer()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleCreateRepo(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleCreateRepo_Success(t *testing.T) {
	store := &httpTestGraphStore{}
	srv := newTestServer(store, &httpTestEmbedder{}, &httpTestExplainer{})
	e := echo.New()
	body := `{"slug":"my-repo","path":"/tmp/repo"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := srv.handleCreateRepo(c); err != nil {
		t.Fatalf("handleCreateRepo: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
}

func TestHandleCreateRepo_ConflictError(t *testing.T) {
	store := &httpTestGraphStore{upsertErr: domain.Conflict("already exists")}
	srv := newTestServer(store, &httpTestEmbedder{}, &httpTestExplainer{})
	e := echo.New()
	body := `{"slug":"dup","path":"/tmp"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleCreateRepo(c)
	assertHTTPError(t, err, http.StatusConflict)
}

func TestHandleGetRepo_Success(t *testing.T) {
	store := &httpTestGraphStore{repo: &types.Repo{Slug: "my-repo", Path: "/tmp"}}
	srv := newTestServer(store, &httpTestEmbedder{}, &httpTestExplainer{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos/my-repo", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("slug")
	c.SetParamValues("my-repo")

	if err := srv.handleGetRepo(c); err != nil {
		t.Fatalf("handleGetRepo: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleGetRepo_NotFound(t *testing.T) {
	store := &httpTestGraphStore{getErr: domain.NotFound("not found")}
	srv := newTestServer(store, &httpTestEmbedder{}, &httpTestExplainer{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos/missing", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("slug")
	c.SetParamValues("missing")

	err := srv.handleGetRepo(c)
	assertHTTPError(t, err, http.StatusNotFound)
}

func TestHandleDeleteRepo_Success(t *testing.T) {
	store := &httpTestGraphStore{repo: &types.Repo{Slug: "bye-repo", Path: "/tmp"}}
	srv := newTestServer(store, &httpTestEmbedder{}, &httpTestExplainer{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/repos/bye-repo", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("slug")
	c.SetParamValues("bye-repo")

	if err := srv.handleDeleteRepo(c); err != nil {
		t.Fatalf("handleDeleteRepo: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleDeleteRepo_NotFound(t *testing.T) {
	store := &httpTestGraphStore{}
	srv := newTestServer(store, &httpTestEmbedder{}, &httpTestExplainer{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/repos/ghost", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("slug")
	c.SetParamValues("ghost")

	err := srv.handleDeleteRepo(c)
	assertHTTPError(t, err, http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// httpError mapping
// ---------------------------------------------------------------------------

func TestHttpErrorNotFound(t *testing.T) {
	he := httpError(domain.NotFound("missing thing"))
	if he.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", he.Code)
	}
}

func TestHttpErrorValidation(t *testing.T) {
	he := httpError(domain.Validation("bad input"))
	if he.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", he.Code)
	}
}

func TestHttpErrorConflict(t *testing.T) {
	he := httpError(domain.Conflict("already exists"))
	if he.Code != http.StatusConflict {
		t.Errorf("code = %d, want 409", he.Code)
	}
}

func TestHttpErrorGeneric(t *testing.T) {
	he := httpError(errors.New("internal failure"))
	if he.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", he.Code)
	}
}

func TestHttpErrorRateLimit(t *testing.T) {
	he := httpError(domain.RateLimit("too fast"))
	if he.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500 for rate_limit (no special mapping)", he.Code)
	}
}

// ---------------------------------------------------------------------------
// writeSSE
// ---------------------------------------------------------------------------

func TestWriteSSE(t *testing.T) {
	e := echo.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := e.NewContext(req, rec)

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
	if srv.echo == nil {
		t.Fatal("echo is nil")
	}
	if srv.jobs == nil {
		t.Fatal("jobs store is nil")
	}
}

func TestSlogMiddleware(t *testing.T) {
	e := echo.New()
	e.Use(SlogMiddleware(slog.Default()))
	e.GET("/ping", func(c echo.Context) error {
		return c.String(http.StatusOK, "pong")
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Index job handlers
// ---------------------------------------------------------------------------

func TestHandleStartIndex_MissingRepoPath(t *testing.T) {
	srv := defaultTestServer()
	e := echo.New()
	body := `{"repo_slug":"r"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/index", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleStartIndex(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleStartIndex_MissingRepoSlug(t *testing.T) {
	srv := defaultTestServer()
	e := echo.New()
	body := `{"repo_path":"/tmp/repo"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/index", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleStartIndex(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleStartIndex_InvalidBody(t *testing.T) {
	srv := defaultTestServer()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/index", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleStartIndex(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleStartIndex_Success(t *testing.T) {
	srv := defaultTestServer()
	e := echo.New()
	body := `{"repo_path":"/tmp/repo","repo_slug":"test-repo"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/index", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := srv.handleStartIndex(c); err != nil {
		t.Fatalf("handleStartIndex: %v", err)
	}
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
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/index/nonexistent", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("job_id")
	c.SetParamValues("nonexistent")

	err := srv.handleIndexStatus(c)
	assertHTTPError(t, err, http.StatusNotFound)
}

func TestHandleIndexStatus_Found(t *testing.T) {
	srv := defaultTestServer()
	e := echo.New()

	// First start an index job to get a valid job_id
	bodyReq := `{"repo_path":"/tmp/x","repo_slug":"s"}`
	startReq := httptest.NewRequest(http.MethodPost, "/api/v1/index", strings.NewReader(bodyReq))
	startReq.Header.Set("Content-Type", "application/json")
	startRec := httptest.NewRecorder()
	startC := e.NewContext(startReq, startRec)
	if err := srv.handleStartIndex(startC); err != nil {
		t.Fatalf("handleStartIndex: %v", err)
	}

	var startResp map[string]string
	json.NewDecoder(startRec.Body).Decode(&startResp)
	jobID := startResp["job_id"]

	// Now poll status
	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/index/"+jobID, nil)
	statusRec := httptest.NewRecorder()
	statusC := e.NewContext(statusReq, statusRec)
	statusC.SetParamNames("job_id")
	statusC.SetParamValues(jobID)

	if err := srv.handleIndexStatus(statusC); err != nil {
		t.Fatalf("handleIndexStatus: %v", err)
	}
	if statusRec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", statusRec.Code)
	}
}

// ---------------------------------------------------------------------------
// indexJobStore
// ---------------------------------------------------------------------------

func TestIndexJobStore_SetGetUpdate(t *testing.T) {
	store := newIndexJobStore()

	job := &IndexJob{ID: "j1", Status: "indexing", RepoSlug: "r", StartedAt: time.Now()}
	store.set(job)

	got, ok := store.get("j1")
	if !ok {
		t.Fatal("job not found")
	}
	if got.Status != "indexing" {
		t.Errorf("status = %q, want indexing", got.Status)
	}

	// Update
	ok = store.update("j1", func(j *IndexJob) { j.Status = "completed" })
	if !ok {
		t.Fatal("update returned false")
	}

	got, _ = store.get("j1")
	if got.Status != "completed" {
		t.Errorf("status = %q, want completed", got.Status)
	}
}

func TestIndexJobStore_UpdateMissing(t *testing.T) {
	store := newIndexJobStore()
	ok := store.update("nonexistent", func(j *IndexJob) { j.Status = "x" })
	if ok {
		t.Error("update should return false for missing job")
	}
}

func TestIndexJobStore_GetMissing(t *testing.T) {
	store := newIndexJobStore()
	_, ok := store.get("missing")
	if ok {
		t.Error("get should return false for missing job")
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
	e := echo.New()
	body := `{"symbol":"pkg.F","repo_slug":"r"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/blast", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := srv.handleBlast(c)
	if err == nil {
		t.Error("expected error from blast service")
	}
}

func TestHandleTraceSSEHopsAndExplanation(t *testing.T) {
	// Exercises the hop-loop writeSSE AND `if result.Explanation != ""` SSE branch.
	store := &httpTestGraphStore{
		node: &types.CodeNode{ID: "function:pkg⋅F", Qualified: "pkg.F"},
		traceHops: []types.TraceHop{
			{Depth: 1, Node: types.CodeNode{ID: "function:pkg⋅G", Qualified: "pkg.G"}},
		},
	}
	explainer := &httpTestExplainer{chunks: []domain.ExplainChunk{{Text: "walk through"}}}
	srv := newTestServer(store, &httpTestEmbedder{vec: []float32{0.1}}, explainer)
	e := echo.New()
	body := `{"symbol":"pkg.F","repo_slug":"r","direction":"forward","depth":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trace", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := srv.handleTrace(c); err != nil {
		t.Fatalf("handleTrace with hops: %v", err)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "hop") {
		t.Errorf("missing 'hop' event in SSE output: %q", out)
	}
}

func TestHandleTraceSSEExplanation(t *testing.T) {
	// Exercises the `if result.Explanation != ""` SSE branch in handleTrace.
	store := &httpTestGraphStore{
		node: &types.CodeNode{ID: "function:pkg⋅F", Qualified: "pkg.F"},
	}
	explainer := &httpTestExplainer{chunks: []domain.ExplainChunk{{Text: "step1"}, {Text: "step2"}}}
	srv := newTestServer(store, &httpTestEmbedder{vec: []float32{0.1}}, explainer)
	e := echo.New()
	body := `{"symbol":"pkg.F","repo_slug":"r","direction":"forward","depth":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trace", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := srv.handleTrace(c); err != nil {
		t.Fatalf("handleTrace: %v", err)
	}
	// The explain SSE event should appear in the response body
	body_out := rec.Body.String()
	if !strings.Contains(body_out, "done") {
		t.Errorf("missing 'done' event in SSE output: %q", body_out)
	}
}

// ---------------------------------------------------------------------------
// Server Start / Shutdown
// ---------------------------------------------------------------------------

func TestServerShutdown(t *testing.T) {
	srv := defaultTestServer()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Shutdown without Start should succeed (no-op or immediate)
	_ = srv.Shutdown(ctx)
}

func TestServerStart_InvalidPort(t *testing.T) {
	srv := defaultTestServer()
	// Override to an invalid port string that will fail immediately
	srv.cfg = &config.ServerConfig{Port: -1}
	err := srv.Start()
	// Should return error — invalid port won't bind
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

// assertHTTPError checks that err is a *echo.HTTPError with the expected code.
func assertHTTPError(t *testing.T, err error, code int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected HTTP error %d, got nil", code)
	}
	he := &echo.HTTPError{}
	if !errors.As(err, &he) {
		t.Fatalf("expected *echo.HTTPError, got %T: %v", err, err)
	}
	if he.Code != code {
		t.Errorf("HTTP status = %d, want %d", he.Code, code)
	}
}

// Ensure json import is used.
var _ = json.Marshal
