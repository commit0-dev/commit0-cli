package cmd

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/genai"

	"github.com/commit0-dev/commit0/internal/adapters/gemini"
	"github.com/commit0-dev/commit0/internal/adapters/surreal"
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
	explainer *gemini.GeminiExplainer
	parser    *treesitter.TreeSitterParser
	walker    *walker.FSWalker
}

// wireDeps constructs all adapters from cfg. The returned cleanup function
// must be called (via defer) to release resources.
func wireDeps(ctx context.Context, cfg *config.Config) (*deps, func(), error) {
	log := slog.Default()

	// 1. SurrealDB — pass the embed dimension so ApplySchema creates HNSW
	// indexes matching the configured embedding provider.
	embedDim := cfg.Gemini.EmbedDimension
	if cfg.EmbedProvider == "voyage" {
		embedDim = cfg.Voyage.EmbedDimension
	}
	db, err := surreal.NewSurrealAdapter(ctx, &cfg.Surreal, embedDim)
	if err != nil {
		return nil, nil, fmt.Errorf("surreal: %w", err)
	}

	// Apply schema DDL only when the stored version is behind the binary version.
	currentVersion, err := db.GetSchemaVersion(ctx)
	if err != nil || currentVersion < surreal.SchemaVersion() {
		if err := db.ApplySchema(ctx); err != nil {
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

	// 4. Explainer (always Gemini).
	var exp *gemini.GeminiExplainer
	if genaiClient != nil {
		exp, err = gemini.NewGeminiExplainer(genaiClient, &cfg.Gemini, log)
		if err != nil {
			db.Close(ctx)
			return nil, nil, fmt.Errorf("gemini explainer: %w", err)
		}
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
	svc := app.NewIndexService(d.walker, d.parser, d.embedder, d.db, cfg)
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
	index   *app.IndexService
	query   *app.QueryService
	trace   *app.TraceService
	blast   *app.BlastService
	repo    *app.RepoService
	cleanup func()
}

// wireServeServices builds all services from one shared deps instance so the
// serve command opens only a single SurrealDB connection.
func wireServeServices(ctx context.Context, cfg *config.Config) (*serveServices, error) {
	d, cleanup, err := wireDeps(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &serveServices{
		index:   app.NewIndexService(d.walker, d.parser, d.embedder, d.db, cfg),
		query:   app.NewQueryService(d.embedder, d.db.AsVectorIndex(), d.db.AsTextIndex(), d.db, d.explainer, cfg),
		trace:   app.NewTraceService(d.db, d.embedder, d.db.AsVectorIndex(), d.explainer, cfg),
		blast:   app.NewBlastService(d.db, d.explainer, cfg),
		repo:    app.NewRepoService(d.db),
		cleanup: cleanup,
	}, nil
}
