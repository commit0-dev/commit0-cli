package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/genai"

	adkmodel "google.golang.org/adk/model"

	"github.com/commit0-dev/commit0/internal/adapters/gemini"
	"github.com/commit0-dev/commit0/internal/adapters/openrouter"
	gitadapter "github.com/commit0-dev/commit0/internal/adapters/git"
	localadapter "github.com/commit0-dev/commit0/internal/adapters/local"
	"github.com/commit0-dev/commit0/internal/adapters/surreal"
	agentpkg "github.com/commit0-dev/commit0/internal/app/agent"
	"github.com/commit0-dev/commit0/internal/adapters/treesitter"
	"github.com/commit0-dev/commit0/internal/adapters/voyage"
	"github.com/commit0-dev/commit0/internal/adapters/walker"
	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/app/memory"
	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
)

// deps holds all constructed adapter instances.
type deps struct {
	db        *surreal.SurrealAdapter
	embedder  domain.Embedder
	explainer domain.LLMExplainer
	parser    *treesitter.TreeSitterParser
	walker    *walker.FSWalker
}

// wireDeps constructs all adapters from cfg. The returned cleanup function
// must be called (via defer) to release resources.
func wireDeps(ctx context.Context, cfg *config.Config) (*deps, func(), error) {
	log := slog.Default()

	// 1. SurrealDB — connect with retry for resilience against cold starts.
	var embedDim int
	switch cfg.EmbedProvider {
	case "ollama":
		embedDim = cfg.Ollama.EmbedDim
	case "voyage":
		embedDim = cfg.Voyage.EmbedDimension
	default:
		embedDim = cfg.Gemini.EmbedDimension
	}

	maxRetries := cfg.Surreal.StartupRetries
	if maxRetries <= 0 {
		maxRetries = 5
	}

	var db *surreal.SurrealAdapter
	var err error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		db, err = surreal.NewSurrealAdapter(ctx, &cfg.Surreal, embedDim)
		if err == nil {
			break
		}
		if attempt == maxRetries {
			return nil, nil, fmt.Errorf("surreal: %w (after %d attempts)", err, maxRetries)
		}
		backoff := time.Duration(attempt*attempt) * time.Second // 1s, 4s, 9s, 16s, 25s
		log.Warn("surreal connect failed, retrying",
			"attempt", attempt, "max", maxRetries,
			"backoff", backoff, "err", err)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("surreal: startup cancelled: %w", ctx.Err())
		}
	}

	// Apply schema DDL only when the stored version is behind the binary version.
	// Use a dedicated timeout so heavy DDL doesn't inherit a short parent deadline.
	rpcTimeoutS := cfg.Surreal.RPCTimeoutS
	if rpcTimeoutS <= 0 {
		rpcTimeoutS = 300 // 5 minutes default
	}
	schemaCtx, schemaCancel := context.WithTimeout(ctx, time.Duration(rpcTimeoutS)*time.Second)
	defer schemaCancel()

	currentVersion, err := db.GetSchemaVersion(schemaCtx)
	log.Info("schema version check",
		"current", currentVersion,
		"required", surreal.SchemaVersion(),
		"needs_apply", err != nil || currentVersion < surreal.SchemaVersion(),
		"err", err)
	if err != nil || currentVersion < surreal.SchemaVersion() {
		if err := db.ApplySchema(schemaCtx); err != nil {
			db.Close(ctx)
			return nil, nil, fmt.Errorf("apply schema: %w", err)
		}
	}

	// 2. Shared Gemini client (used by explainer, and optionally by embedder).
	var genaiClient *genai.Client
	if cfg.Gemini.APIKey != "" {
		genaiClient, err = gemini.NewGeminiClient(ctx, &cfg.Gemini)
		if err != nil {
			db.Close(ctx)
			return nil, nil, fmt.Errorf("gemini client: %w", err)
		}
	}

	// 3. Embedder — provider selected by config.
	var emb domain.Embedder
	switch cfg.EmbedProvider {
	case "ollama":
		emb = localadapter.NewOllamaEmbedder(cfg.Ollama.URL, cfg.Ollama.EmbedModel, cfg.Ollama.EmbedDim, log)
		log.Info("using local embeddings via Ollama",
			"model", cfg.Ollama.EmbedModel, "dim", cfg.Ollama.EmbedDim, "url", cfg.Ollama.URL)
	case "voyage":
		emb, err = voyage.NewVoyageEmbedder(&cfg.Voyage, log)
		if err != nil {
			db.Close(ctx)
			return nil, nil, fmt.Errorf("voyage embedder: %w", err)
		}
	default: // "gemini"
		if genaiClient == nil {
			db.Close(ctx)
			return nil, nil, fmt.Errorf("gemini embedder: GEMINI_API_KEY is required")
		}
		emb, err = gemini.NewGeminiEmbedder(genaiClient, &cfg.Gemini, log)
		if err != nil {
			db.Close(ctx)
			return nil, nil, fmt.Errorf("gemini embedder: %w", err)
		}
	}

	// 4. Explainer — local Ollama or cloud Gemini.
	var exp domain.LLMExplainer
	if cfg.Ollama.Model != "" {
		ollamaExp := localadapter.NewOllamaExplainer(cfg.Ollama.URL, cfg.Ollama.Model, log)
		if err := ollamaExp.Ping(ctx); err != nil {
			log.Warn("ollama not available, falling back to Gemini", "err", err)
		} else {
			log.Info("using local model via Ollama", "model", cfg.Ollama.Model, "url", cfg.Ollama.URL)
			exp = ollamaExp
		}
	}
	if exp == nil && genaiClient != nil {
		geminiExp, err := gemini.NewGeminiExplainer(genaiClient, &cfg.Gemini, log)
		if err != nil {
			db.Close(ctx)
			return nil, nil, fmt.Errorf("gemini explainer: %w", err)
		}
		exp = geminiExp
	}

	// 5. tree-sitter parser
	p := treesitter.NewParser(log)

	// 6. filesystem walker
	w := walker.NewFSWalker(log)

	cleanup := func() {
		db.Close(ctx)
	}

	return &deps{
		db:        db,
		embedder:  emb,
		explainer: exp,
		parser:    p,
		walker:    w,
	}, cleanup, nil
}

// serveServices holds all services wired from a single shared deps instance.
type serveServices struct {
	index      *app.IndexService
	query      *app.QueryService
	trace      *app.TraceService
	blast      *app.BlastService
	repo       *app.RepoService
	db         domain.GraphStore
	agent      domain.AgentRunner
	flow       *app.FieldFlowService
	temporal   *app.TemporalService
	rootCause  *app.RootCauseAnalysisService
	apiSurface *app.APISurfaceService
	memMgr     *memory.Manager
	sessionSvc app.SessionStore
	cleanup    func()
}

// wireServeServices builds all services from one shared deps instance so the
// serve command opens only a single SurrealDB connection.
func wireServeServices(ctx context.Context, cfg *config.Config) (*serveServices, error) {
	log := slog.Default()
	d, cleanup, err := wireDeps(ctx, cfg)
	if err != nil {
		return nil, err
	}
	querySvc := app.NewQueryService(d.embedder, d.db.AsVectorIndex(), d.db.AsTextIndex(), d.db, d.explainer, cfg)
	traceSvc := app.NewTraceService(d.db, d.embedder, d.db.AsVectorIndex(), d.explainer, cfg)
	blastSvc := app.NewBlastService(d.db, d.explainer, cfg)

	// Field flow + temporal services (for root cause analysis)
	flowSvc := app.NewFieldFlowService(d.db, d.embedder, d.db.AsVectorIndex(), d.explainer, cfg)
	gitW := gitadapter.NewWalker(log)
	tempSvc := app.NewTemporalService(d.db, nil, gitW, d.parser)
	rootCauseSvc := app.NewRootCauseAnalysisService(querySvc, flowSvc, tempSvc, d.db, gitW, d.explainer, cfg)

	// Memory management (3-tier: working → session → persistent).
	memMgr := memory.NewManager(d.db.AsMemoryStore(), d.embedder, nil, memory.DefaultBudgets())
	sessionSvc := d.db.AsSessionStore() // SurrealDB-backed session persistence

	// Agent service (optional — constructor decides availability based on config).
	// Create LLM model based on provider.
	var llmModel adkmodel.LLM // nil = use Gemini fallback in AgentService
	if cfg.LLMProvider == "openrouter" && cfg.OpenRouter.APIKey != "" {
		orClient := openrouter.NewClient(cfg.OpenRouter.APIKey, cfg.OpenRouter.BaseURL)
		llmModel = openrouter.NewModel(orClient, cfg.OpenRouter.Model, cfg.OpenRouter.MaxTokens)
		log.Info("using OpenRouter LLM", "model", cfg.OpenRouter.Model)
	}

	var agentRunner domain.AgentRunner
	agentSvc, err := agentpkg.NewAgentService(
		querySvc, traceSvc, blastSvc, flowSvc, tempSvc, rootCauseSvc,
		d.db, gitW, d.explainer, cfg, memMgr, llmModel,
	)
	if err != nil {
		log.Warn("agent service unavailable", "err", err)
	} else {
		agentRunner = agentSvc
	}

	indexSvc := app.NewIndexService(d.walker, d.parser, d.embedder, d.db, d.explainer, cfg)
	if cfg.EmbedProvider == "ollama" {
		indexSvc.SetDocPrefix(localadapter.DocPrefixForModel(cfg.Ollama.EmbedModel))
	}
	indexSvc.SetTemporalService(tempSvc)

	apiSurfaceSvc := app.NewAPISurfaceService(d.db, flowSvc, d.explainer, cfg)

	return &serveServices{
		index:      indexSvc,
		query:      querySvc,
		trace:      traceSvc,
		blast:      blastSvc,
		repo:       app.NewRepoService(d.db),
		db:         d.db,
		agent:      agentRunner,
		flow:       flowSvc,
		temporal:   tempSvc,
		rootCause:  rootCauseSvc,
		apiSurface: apiSurfaceSvc,
		memMgr:     memMgr,
		sessionSvc: sessionSvc,
		cleanup:    cleanup,
	}, nil
}
