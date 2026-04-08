package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/genai"

	"github.com/commit0-dev/commit0/internal/adapters/gemini"
	gitadapter "github.com/commit0-dev/commit0/internal/adapters/git"
	localadapter "github.com/commit0-dev/commit0/internal/adapters/local"
	"github.com/commit0-dev/commit0/internal/adapters/surreal"
	agentpkg "github.com/commit0-dev/commit0/internal/app/agent"
	"github.com/commit0-dev/commit0/internal/adapters/treesitter"
	"github.com/commit0-dev/commit0/internal/adapters/voyage"
	"github.com/commit0-dev/commit0/internal/adapters/walker"
	"github.com/commit0-dev/commit0/internal/app"
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
	embedDim := cfg.Gemini.EmbedDimension
	if cfg.EmbedProvider == "voyage" {
		embedDim = cfg.Voyage.EmbedDimension
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
	if cfg.LocalModel != "" {
		ollamaExp := localadapter.NewOllamaExplainer(cfg.OllamaURL, cfg.LocalModel, log)
		if err := ollamaExp.Ping(ctx); err != nil {
			log.Warn("ollama not available, falling back to Gemini", "err", err)
		} else {
			log.Info("using local model via Ollama", "model", cfg.LocalModel, "url", cfg.OllamaURL)
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

func wireIndexService(ctx context.Context, cfg *config.Config) (*app.IndexService, func(), error) {
	d, cleanup, err := wireDeps(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	svc := app.NewIndexService(d.walker, d.parser, d.embedder, d.db, d.explainer, cfg)
	// Wire temporal service for CLI indexing
	gitW := gitadapter.NewWalker(slog.Default())
	tempSvc := app.NewTemporalService(d.db, nil, gitW, d.parser)
	svc.SetTemporalService(tempSvc)
	return svc, cleanup, nil
}

func wireQueryService(ctx context.Context, cfg *config.Config) (*app.QueryService, func(), error) {
	d, cleanup, err := wireDeps(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	svc := app.NewQueryService(d.embedder, d.db.AsVectorIndex(), d.db.AsTextIndex(), d.db, d.explainer, cfg)
	return svc, cleanup, nil
}

func wireTraceService(ctx context.Context, cfg *config.Config) (*app.TraceService, func(), error) {
	d, cleanup, err := wireDeps(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	svc := app.NewTraceService(d.db, d.embedder, d.db.AsVectorIndex(), d.explainer, cfg)
	return svc, cleanup, nil
}

func wireBlastService(ctx context.Context, cfg *config.Config) (*app.BlastService, func(), error) {
	d, cleanup, err := wireDeps(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	svc := app.NewBlastService(d.db, d.explainer, cfg)
	return svc, cleanup, nil
}

func wireRepoService(ctx context.Context, cfg *config.Config) (*app.RepoService, func(), error) {
	d, cleanup, err := wireDeps(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	svc := app.NewRepoService(d.db)
	return svc, cleanup, nil
}

// serveServices holds all services wired from a single shared deps instance.
type serveServices struct {
	index     *app.IndexService
	query     *app.QueryService
	trace     *app.TraceService
	blast     *app.BlastService
	repo      *app.RepoService
	db        domain.GraphStore
	agent     domain.AgentRunner
	flow      *app.FieldFlowService
	temporal  *app.TemporalService
	rootCause *app.RootCauseAnalysisService
	cleanup   func()
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

	// Agent service (optional — requires Gemini API key)
	var agentRunner domain.AgentRunner
	if cfg.Gemini.APIKey != "" {
		agentSvc, err := agentpkg.NewAgentService(
			querySvc, traceSvc, blastSvc, flowSvc, tempSvc, rootCauseSvc,
			d.db, gitW, d.explainer, cfg,
		)
		if err != nil {
			log.Warn("agent service unavailable", "err", err)
		} else {
			agentRunner = agentSvc
		}
	}

	indexSvc := app.NewIndexService(d.walker, d.parser, d.embedder, d.db, d.explainer, cfg)
	indexSvc.SetTemporalService(tempSvc)

	return &serveServices{
		index:     indexSvc,
		query:     querySvc,
		trace:     traceSvc,
		blast:     blastSvc,
		repo:      app.NewRepoService(d.db),
		db:        d.db,
		agent:     agentRunner,
		flow:      flowSvc,
		temporal:  tempSvc,
		rootCause: rootCauseSvc,
		cleanup:   cleanup,
	}, nil
}
