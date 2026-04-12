package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/genai"

	adkmodel "google.golang.org/adk/model"

	"github.com/commit0-dev/commit0/server/internal/adapters/gemini"
	gitadapter "github.com/commit0-dev/commit0/server/internal/adapters/git"
	localadapter "github.com/commit0-dev/commit0/server/internal/adapters/local"
	"github.com/commit0-dev/commit0/server/internal/adapters/openrouter"
	consuladapter "github.com/commit0-dev/commit0/server/internal/adapters/consul"
	mdnsadapter "github.com/commit0-dev/commit0/server/internal/adapters/mdns"
	quicadapter "github.com/commit0-dev/commit0/server/internal/adapters/quic"
	syncadapter "github.com/commit0-dev/commit0/server/internal/adapters/sync"
	"github.com/commit0-dev/commit0/server/internal/adapters/surreal"
	"github.com/commit0-dev/commit0/server/internal/adapters/treesitter"
	"github.com/commit0-dev/commit0/server/internal/adapters/voyage"
	"github.com/commit0-dev/commit0/server/internal/adapters/walker"
	"github.com/commit0-dev/commit0/server/internal/app"
	agentpkg "github.com/commit0-dev/commit0/server/internal/app/agent"
	"github.com/commit0-dev/commit0/server/internal/app/linkers"
	"github.com/commit0-dev/commit0/server/internal/app/memory"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
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
	// Use normalized index dimension for HNSW — all providers output at this dim.
	embedDim := cfg.EmbedDim
	if embedDim <= 0 {
		embedDim = 1024
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

	// 3. Embedder — all providers output at the normalized index dimension.
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	var emb domain.Embedder
	switch cfg.EmbedProvider {
	case "ollama":
		emb = localadapter.NewOllamaEmbedder(cfg.Ollama.URL, cfg.Ollama.EmbedModel, embedDim, log)
		log.Info("using local embeddings via Ollama",
			"model", cfg.Ollama.EmbedModel, "dim", embedDim, "url", cfg.Ollama.URL)
	case "voyage":
		emb, err = voyage.NewVoyageEmbedder(cfg.Voyage.APIKey, cfg.Voyage.Model, cfg.Voyage.BaseURL, embedDim, batchSize, log)
		if err != nil {
			db.Close(ctx)
			return nil, nil, fmt.Errorf("voyage embedder: %w", err)
		}
	default: // "gemini"
		if genaiClient == nil {
			db.Close(ctx)
			return nil, nil, fmt.Errorf("gemini embedder: GEMINI_API_KEY is required")
		}
		emb, err = gemini.NewGeminiEmbedder(genaiClient, cfg.Gemini.EmbedModel, embedDim, batchSize, log)
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
	graph      domain.OpenCodeGraph
	agent      domain.AgentRunner
	flow       *app.FieldFlowService
	temporal   *app.TemporalService
	rootCause  *app.RootCauseAnalysisService
	apiSurface *app.APISurfaceService
	syncSvc    *app.SyncService
	transport  domain.PeerTransport
	discovery  domain.PeerDiscovery
	peerStore  domain.PeerStore
	scopeStore domain.ScopeStore
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
	// OpenCodeGraph: single graph interface for all services (OpenCodeGraph §3).
	graph := d.db.AsOpenCodeGraph()

	querySvc := app.NewQueryService(d.embedder, graph, d.explainer, cfg)
	traceSvc := app.NewTraceService(graph, d.embedder, d.explainer, cfg)
	blastSvc := app.NewBlastService(graph, d.explainer, cfg)

	// Field flow + temporal services (for root cause analysis)
	flowSvc := app.NewFieldFlowService(graph, d.embedder, d.explainer, cfg)
	gitW := gitadapter.NewWalker(log)
	tempSvc := app.NewTemporalService(graph, d.db, gitW, d.parser)
	rootCauseSvc := app.NewRootCauseAnalysisService(querySvc, flowSvc, tempSvc, graph, gitW, d.explainer, cfg)

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
		graph, gitW, d.explainer, cfg, memMgr, llmModel,
	)
	if err != nil {
		log.Warn("agent service unavailable", "err", err)
	} else {
		agentRunner = agentSvc
	}

	indexSvc := app.NewIndexService(d.walker, d.parser, d.embedder, graph, d.explainer, cfg)
	if cfg.EmbedProvider == "ollama" {
		indexSvc.SetDocPrefix(localadapter.DocPrefixForModel(cfg.Ollama.EmbedModel))
	}
	indexSvc.SetTemporalService(tempSvc)
	indexSvc.SetLinkers([]domain.EdgeLinker{
		&linkers.DefinesLinker{},
		&linkers.CallLinker{},
		&linkers.DataFlowLinker{},
		&linkers.FieldAccessLinker{},
		&linkers.RouteLinker{},
	})

	apiSurfaceSvc := app.NewAPISurfaceService(graph, flowSvc, d.explainer, cfg)

	// Sync service (P2P graph sync).
	var syncSvc *app.SyncService
	var quicTransport domain.PeerTransport
	codec, err := syncadapter.NewCBORCodec()
	if err != nil {
		log.Warn("sync: CBOR codec init failed", "err", err)
	} else {
		var auth domain.SyncAuth
		if cfg.Sync.Passphrase != "" {
			auth = syncadapter.NewPassphraseAuth(cfg.Sync.Passphrase)
			log.Info("sync enabled with passphrase auth")
		} else {
			log.Info("sync enabled without auth (no SYNC_PASSPHRASE set)")
		}
		syncSvc = app.NewSyncService(d.db, d.db, codec, auth)
		syncSvc.SetIndexService(indexSvc)

		// QUIC transport for P2P data plane.
		if qt, qErr := quicadapter.NewTransport(cfg.Sync.Passphrase, codec); qErr != nil {
			log.Warn("sync: QUIC transport init failed", "err", qErr)
		} else {
			quicTransport = qt
		}
	}

	// Discovery (Consul or mDNS based on config).
	var disc domain.PeerDiscovery
	switch cfg.Sync.DiscoveryMode {
	case "mdns":
		disc = mdnsadapter.New(cfg.Sync.InstanceName)
		log.Info("sync discovery: mDNS (LAN only)")
	default: // "consul"
		if cd, cdErr := consuladapter.New(cfg.Sync.ConsulAddr, cfg.Sync.ConsulToken); cdErr != nil {
			log.Warn("consul discovery unavailable, falling back to mDNS", "err", cdErr)
			disc = mdnsadapter.New(cfg.Sync.InstanceName)
		} else {
			disc = cd
			log.Info("sync discovery: Consul", "addr", cfg.Sync.ConsulAddr)
		}
	}

	return &serveServices{
		index:      indexSvc,
		query:      querySvc,
		trace:      traceSvc,
		blast:      blastSvc,
		repo:       app.NewRepoService(graph),
		graph:       graph,
		agent:      agentRunner,
		flow:       flowSvc,
		temporal:   tempSvc,
		rootCause:  rootCauseSvc,
		apiSurface: apiSurfaceSvc,
		syncSvc:    syncSvc,
		transport:  quicTransport,
		discovery:  disc,
		peerStore:  d.db.AsPeerStore(),
		scopeStore: d.db.AsScopeStore(),
		memMgr:     memMgr,
		sessionSvc: sessionSvc,
		cleanup:    cleanup,
	}, nil
}
