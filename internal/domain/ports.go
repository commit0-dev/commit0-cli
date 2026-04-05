package domain

import (
	"context"

	"github.com/commit0-dev/commit0/pkg/types"
)

// VectorSearchOpts configures vector similarity search
type VectorSearchOpts struct {
	RepoSlug  string
	TopK      int
	MinScore  float64
	NodeKinds []types.NodeKind
	Effort    int // effort level for ANN search
}

// TextSearchOpts configures full-text search
type TextSearchOpts struct {
	RepoSlug  string
	TopK      int
	Fields    []string
	NodeKinds []types.NodeKind
}

// EmbedInput represents data to be embedded
type EmbedInput struct {
	ID          string
	Text        string
	Images      [][]byte
	ImageMIMEs  []string
	ContentHash string
}

// EmbedResult represents the result of embedding operation
type EmbedResult struct {
	ID     string
	Vector []float32 // 3072-dimensional
	Cached bool
}

// CodeExcerpt represents a code snippet with relevance info
type CodeExcerpt struct {
	Qualified string
	FilePath  string
	Lines     string
	Snippet   string
	Score     float64
}

// ExplainRequest configures an explanation request
type ExplainRequest struct {
	QueryType    string        // "search" | "trace" | "blast"
	UserQuery    string
	CodeContext  []CodeExcerpt
	GraphContext string
}

// ExplainChunk represents a streamed chunk of explanation
type ExplainChunk struct {
	Text  string
	Done  bool
	Error error
}

// FileEntry represents a single file in the walk
type FileEntry struct {
	Path     string
	AbsPath  string
	Language string
	Content  []byte
}

// ParsedFile represents the result of parsing a source file
type ParsedFile struct {
	Path        string
	Language    string
	ContentHash string
	Nodes       []types.CodeNode
	Edges       []types.CodeEdge
	LineCount   int
	SizeBytes   int
}

// WalkOpts configures file system walking
type WalkOpts struct {
	Languages []string
	Exclude   []string
	MaxFileKB int
}

// GraphStore provides CRUD operations and graph traversal
type GraphStore interface {
	UpsertNode(ctx context.Context, node *types.CodeNode) error
	GetNode(ctx context.Context, id string) (*types.CodeNode, error)
	GetNodeByQualified(ctx context.Context, repo, qualified string) (*types.CodeNode, error)
	DeleteNode(ctx context.Context, id string) error
	DeleteNodesByRepo(ctx context.Context, repo string) error
	UpsertEdge(ctx context.Context, edge *types.CodeEdge) error
	DeleteEdgesForNode(ctx context.Context, nodeID string) error
	TraceForward(ctx context.Context, startID string, depth int) ([]types.TraceHop, error)
	TraceReverse(ctx context.Context, startID string, depth int) ([]types.TraceHop, error)
	BlastRadius(ctx context.Context, targetID string, maxDepth int) ([]types.AffectedNode, error)
	UpsertFileBatch(ctx context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error
	UpsertRepo(ctx context.Context, repo *types.Repo) error
	GetRepo(ctx context.Context, slug string) (*types.Repo, error)
	ListRepos(ctx context.Context) ([]types.Repo, error)
	ApplySchema(ctx context.Context) error
	GetSchemaVersion(ctx context.Context) (int, error)
}

// VectorIndex provides approximate nearest neighbor search over embeddings
type VectorIndex interface {
	Search(ctx context.Context, query []float32, opts VectorSearchOpts) ([]types.ScoredNode, error)
}

// TextIndex provides full-text search capabilities
type TextIndex interface {
	Search(ctx context.Context, query string, opts TextSearchOpts) ([]types.ScoredNode, error)
}

// Embedder converts text and images to embeddings
type Embedder interface {
	EmbedBatch(ctx context.Context, inputs []EmbedInput) ([]EmbedResult, error)
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
}

// LLMExplainer generates natural language explanations
type LLMExplainer interface {
	Explain(ctx context.Context, req ExplainRequest) (<-chan ExplainChunk, error)
}

// Parser extracts code structure from source files
type Parser interface {
	Parse(ctx context.Context, file FileEntry) (*ParsedFile, error)
	SupportedLanguages() []string
}

// FileWalker traverses a repository file system
type FileWalker interface {
	Walk(ctx context.Context, repoPath string, opts WalkOpts) (<-chan FileEntry, <-chan error)
}
