package domain

import (
	"context"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
)

// VectorSearchOpts configures vector similarity search.
type VectorSearchOpts struct {
	RepoSlug  string
	NodeKinds []types.NodeKind
	TopK      int
	MinScore  float64
	Effort    int
}

// TextSearchOpts configures full-text search.
type TextSearchOpts struct {
	RepoSlug  string
	Fields    []string
	NodeKinds []types.NodeKind
	TopK      int
}

// EmbedInput represents data to be embedded.
type EmbedInput struct {
	ID          string
	Text        string
	ContentHash string
	Images      [][]byte
	ImageMIMEs  []string
}

// EmbedResult represents the result of embedding operation.
type EmbedResult struct {
	ID     string
	Vector []float32 // 3072-dimensional
	Cached bool
}

// CodeExcerpt represents a code snippet with relevance info.
type CodeExcerpt struct {
	Qualified string
	FilePath  string
	Lines     string
	Snippet   string
	Score     float64
}

// ExplainRequest configures an explanation request.
type ExplainRequest struct {
	QueryType    string
	UserQuery    string
	GraphContext string
	CodeContext  []CodeExcerpt
}

// ExplainChunk represents a streamed chunk of explanation.
type ExplainChunk struct {
	Error error
	Text  string
	Done  bool
}

// FileEntry represents a single file in the walk.
type FileEntry struct {
	Path     string
	AbsPath  string
	Language string
	Content  []byte
}

// ParsedFile represents the result of parsing a source file.
type ParsedFile struct {
	Path        string
	Language    string
	ContentHash string
	Nodes       []types.CodeNode
	Edges       []types.CodeEdge
	LineCount   int
	SizeBytes   int
}

// WalkOpts configures file system walking.
type WalkOpts struct {
	Languages []string
	Exclude   []string
	MaxFileKB int
}

// NeighborNode is an enriched reference to an adjacent graph node, carrying
// the signature and docstring needed to build richer embedding context.
type NeighborNode struct {
	Qualified string
	Signature string
	Docstring string
	FilePath  string
	// ParamName is set on DataSinks: the callee parameter that receives the data.
	ParamName string
	// ArgExpr is set on DataSources: the expression passed at the call site.
	ArgExpr   string
	StartLine int
}

// Neighborhood holds all immediate graph context for a node.
// It is used by the ContextBuilder to produce richer embedding inputs.
type Neighborhood struct {
	// Callees are functions directly called by this node.
	Callees []NeighborNode
	// Callers are functions that directly call this node.
	Callers []NeighborNode
	// DataSinks are nodes that receive data from this node via data_flow edges.
	DataSinks []NeighborNode
	// DataSources are nodes whose data flows into this node via data_flow edges.
	DataSources []NeighborNode
	// Reads lists field names (e.g. "User.Email") read by this node.
	Reads []string
	// Writes lists field names written by this node.
	Writes []string
}

// IsEmpty reports whether a Neighborhood has no useful context.
func (nb *Neighborhood) IsEmpty() bool {
	return len(nb.Callees) == 0 && len(nb.Callers) == 0 &&
		len(nb.DataSinks) == 0 && len(nb.DataSources) == 0 &&
		len(nb.Reads) == 0 && len(nb.Writes) == 0
}

// GraphStore provides CRUD operations and graph traversal.
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
	// GetNeighborhood returns the immediate graph context for nodeID: callers,
	// callees (with signatures), and data-flow sources/sinks.
	GetNeighborhood(ctx context.Context, nodeID string) (*Neighborhood, error)
	// TraceDataFlow follows data_flow edges from startID.
	// direction must be "forward" (sinks), "reverse" (sources), or "both".
	TraceDataFlow(ctx context.Context, startID string, depth int, direction string) ([]types.TraceHop, error)
	// ListNodeIDs returns the record IDs of all indexable nodes for a repo.
	// Used by the neighborhood re-embedding pass.
	ListNodeIDs(ctx context.Context, repoSlug string) ([]string, error)
	// ListNodesByFile returns all nodes (functions, classes) defined in a file.
	ListNodesByFile(ctx context.Context, repoSlug, filePath string) ([]types.CodeNode, error)
	// ListNodesByConcepts returns nodes whose concepts overlap with the given tags.
	ListNodesByConcepts(ctx context.Context, repoSlug string, concepts []string, limit int) ([]types.CodeNode, error)
	// ListRoutes returns all route edges for a repo (HTTP route registrations).
	ListRoutes(ctx context.Context, repoSlug string) ([]types.CodeEdge, error)
	// UpdateRepoIndexedAt sets the last_indexed_at timestamp using MERGE (not CONTENT)
	// so it doesn't wipe other fields on the repo record.
	UpdateRepoIndexedAt(ctx context.Context, slug string, t time.Time) error
	// FindRepoByRemoteURL finds a repo by its normalized remote URL for deduplication.
	FindRepoByRemoteURL(ctx context.Context, remoteURL string) (*types.Repo, error)
	UpsertFileBatch(ctx context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error
	UpsertRepo(ctx context.Context, repo *types.Repo) error
	GetRepo(ctx context.Context, slug string) (*types.Repo, error)
	ListRepos(ctx context.Context) ([]types.Repo, error)
	ApplySchema(ctx context.Context) error
	GetSchemaVersion(ctx context.Context) (int, error)
}

// VectorIndex provides approximate nearest neighbor search over embeddings.
type VectorIndex interface {
	Search(ctx context.Context, query []float32, opts VectorSearchOpts) ([]types.ScoredNode, error)
}

// TextIndex provides full-text search capabilities.
type TextIndex interface {
	Search(ctx context.Context, query string, opts TextSearchOpts) ([]types.ScoredNode, error)
}

// Embedder converts text and images to embeddings.
type Embedder interface {
	EmbedBatch(ctx context.Context, inputs []EmbedInput) ([]EmbedResult, error)
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
}

// LLMExplainer generates natural language explanations.
type LLMExplainer interface {
	Explain(ctx context.Context, req ExplainRequest) (<-chan ExplainChunk, error)
	// ExplainStructured returns a structured JSON explanation using Gemini's
	// response_json_schema feature. The caller unmarshals the result into the
	// appropriate type based on req.QueryType ("search", "trace", "blast").
	ExplainStructured(ctx context.Context, req ExplainRequest) ([]byte, error)
}

// ChatRequest represents a user message in an agentic conversation.
type ChatRequest struct {
	SessionID string
	UserID    string
	RepoSlug  string
	Message   string
}

// ChatEvent is streamed back as the agent reasons and calls tools.
type ChatEvent struct {
	Type     string `json:"type"`      // "thinking", "tool_call", "tool_result", "message", "error", "done"
	Content  string `json:"content"`   // text content or JSON
	ToolName string `json:"tool_name"` // set for tool_call/tool_result
	Done     bool   `json:"done"`
}

// AgentRunner executes agentic conversations with tool use.
type AgentRunner interface {
	Chat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error)
}

// ---------------------------------------------------------------------------
// Temporal Store — time-aware code graph operations
// ---------------------------------------------------------------------------

// TemporalStore extends GraphStore with commit-aware operations.
type TemporalStore interface {
	UpsertNodeTemporal(ctx context.Context, node *types.CodeNode, commitHash string, commitTime time.Time) error
	UpsertEdgeTemporal(ctx context.Context, edge *types.CodeEdge, commitHash string, commitTime time.Time) error
	MarkNodeRemoved(ctx context.Context, nodeID, commitHash string, commitTime time.Time) error
	MarkEdgeRemoved(ctx context.Context, edgeID, commitHash string, commitTime time.Time) error
	QueryTemporalRange(ctx context.Context, repoSlug, fromCommit, toCommit string) ([]types.TemporalChange, error)
	NodeHistory(ctx context.Context, nodeID string) ([]types.TemporalChange, error)
	EdgesIntroducedAt(ctx context.Context, repoSlug, commitHash string) ([]types.CodeEdge, error)
}

// ---------------------------------------------------------------------------
// Field-Level Data Flow Store
// ---------------------------------------------------------------------------

// FieldFlowStore provides field-level data flow queries.
type FieldFlowStore interface {
	TraceFieldFlow(ctx context.Context, startID string, fieldPath string, depth int, direction string) ([]types.FieldFlowHop, error)
	FindMutations(ctx context.Context, repoSlug string, fieldPath string) ([]types.FieldFlowHop, error)
}

// ---------------------------------------------------------------------------
// Memory Store — three-tier persistent memory
// ---------------------------------------------------------------------------

// MemoryStore provides CRUD for the persistent memory tier.
type MemoryStore interface {
	StoreMemory(ctx context.Context, entry *types.MemoryEntry) error
	RetrieveMemories(ctx context.Context, repoSlug string, queryVec []float32, topK int) ([]types.MemoryEntry, error)
	ListSessionMemories(ctx context.Context, sessionID string) ([]types.MemoryEntry, error)
	DeleteSessionMemories(ctx context.Context, sessionID string) error
}

// ---------------------------------------------------------------------------
// Git Walker — access to git history
// ---------------------------------------------------------------------------

// GitCommit holds metadata about a git commit.
type GitCommit struct {
	Hash      string
	Message   string
	Author    string
	Timestamp time.Time
	Files     []string
}

// GitFileDiff describes a single file change in a commit.
type GitFileDiff struct {
	Path      string
	OldPath   string // set if renamed
	Status    string // "added", "modified", "deleted", "renamed"
	Additions int
	Deletions int
	Patch     string // unified diff
}

// GitWalker provides access to git history for temporal indexing.
type GitWalker interface {
	ListCommits(ctx context.Context, repoPath string, fromRef, toRef string) ([]GitCommit, error)
	DiffCommit(ctx context.Context, repoPath, commitHash string) ([]GitFileDiff, error)
	ReadFileAtCommit(ctx context.Context, repoPath, commitHash, filePath string) ([]byte, error)
	CommitInfo(ctx context.Context, repoPath, commitHash string) (*GitCommit, error)
}

// ---------------------------------------------------------------------------
// Compressor — LLM-based context compression
// ---------------------------------------------------------------------------

// Compressor compresses context using LLM summarization.
type Compressor interface {
	CompressTurn(ctx context.Context, role, content string, toolCalls []string) (string, error)
	CompressSession(ctx context.Context, turns []string) (string, error)
}

// Parser extracts code structure from source files.
type Parser interface {
	Parse(ctx context.Context, file FileEntry) (*ParsedFile, error)
	SupportedLanguages() []string
}

// FileWalker traverses a repository file system.
type FileWalker interface {
	Walk(ctx context.Context, repoPath string, opts WalkOpts) (<-chan FileEntry, <-chan error)
}
