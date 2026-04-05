package app

import (
	"context"
	"testing"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

func TestIndexServiceIndexHappyPath(t *testing.T) {
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go", Content: []byte("func main() {}")},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:     "main.go",
			Language: "go",
			Nodes: []types.CodeNode{
				{ID: "f1", Qualified: "main", Kind: types.NodeFunction},
			},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{
			{ID: "f1", Vector: []float32{0.1, 0.2}},
		},
	}

	store := newStubGraphStore()

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 1,
			MaxWorkersStore: 1,
			BatchSize:       1,
		},
	}

	svc := NewIndexService(walker, parser, embedder, store, cfg)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	if result.FilesIndexed != 1 {
		t.Errorf("FilesIndexed = %d, want 1", result.FilesIndexed)
	}

	if result.NodesCreated != 1 {
		t.Errorf("NodesCreated = %d, want 1", result.NodesCreated)
	}
}

func TestIndexServiceIndexWalkerError(t *testing.T) {
	walker := &stubFileWalker{
		files: []domain.FileEntry{},
		err:   domain.Validation("invalid path"),
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:     "main.go",
			Language: "go",
			Nodes:    []types.CodeNode{},
			Edges:    []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{}
	store := newStubGraphStore()

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 1,
			MaxWorkersStore: 1,
		},
	}

	svc := NewIndexService(walker, parser, embedder, store, cfg)

	_, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/invalid",
		RepoSlug: "my-repo",
	})

	if err == nil {
		t.Errorf("Index should fail with walker error")
	}
}

func TestIndexServiceIndexParseFailure(t *testing.T) {
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "bad.go", Language: "go"},
		},
	}

	parser := &stubParser{
		err: domain.Validation("syntax error"),
	}

	embedder := &stubEmbedder{}
	store := newStubGraphStore()

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 1,
			MaxWorkersStore: 1,
		},
	}

	svc := NewIndexService(walker, parser, embedder, store, cfg)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	// Parse failures are non-fatal
	if err != nil {
		t.Errorf("Index should not fail on parse error (non-fatal), got: %v", err)
	}

	if result.FilesIndexed != 0 {
		t.Errorf("FilesIndexed = %d, want 0 (file wasn't parsed)", result.FilesIndexed)
	}
}

func TestIndexServiceZeroFiles(t *testing.T) {
	walker := &stubFileWalker{
		files: []domain.FileEntry{},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Nodes: []types.CodeNode{},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{}
	store := newStubGraphStore()

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 1,
			MaxWorkersStore: 1,
		},
	}

	svc := NewIndexService(walker, parser, embedder, store, cfg)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	if result.FilesIndexed != 0 {
		t.Errorf("FilesIndexed = %d, want 0", result.FilesIndexed)
	}

	if result.Timing.TotalMS < 0 {
		t.Errorf("Timing.TotalMS should be non-negative, got %d", result.Timing.TotalMS)
	}
}
