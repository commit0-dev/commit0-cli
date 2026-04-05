package types

import "time"

// NodeKind represents the type of a code node
type NodeKind string

const (
	NodeFile     NodeKind = "file"
	NodeFunction NodeKind = "function"
	NodeClass    NodeKind = "class"
	NodeModule   NodeKind = "module"
)

// EdgeKind represents the type of a relationship between nodes
type EdgeKind string

const (
	EdgeCalls   EdgeKind = "calls"
	EdgeImports EdgeKind = "imports"
	EdgeDefines EdgeKind = "defines"
	EdgeInherits EdgeKind = "inherits"
	EdgeUses    EdgeKind = "uses"
)

// CodeNode represents a single entity in the codebase (function, class, file, module)
type CodeNode struct {
	ID          string     // SurrealDB record ID (e.g., "function:pkg⋅Handler⋅ServeHTTP")
	Kind        NodeKind   // file | function | class | module
	Name        string     // Short name
	Qualified   string     // Fully qualified: pkg.Receiver.Method
	FilePath    string     // Relative to repo root
	RepoSlug    string     // Repository identifier
	Language    string     // go, python, typescript, javascript, etc.
	StartLine   int        // Starting line number
	EndLine     int        // Ending line number
	Signature   string     // Function params + return types (if applicable)
	Docstring   string     // Documentation/comments
	Body        string     // Raw source code
	ContentHash string     // SHA-256 of embedding input
	Embedding   []float32  // 3072-dim vector (nil if not yet embedded)
	Visibility  string     // public | private | protected | internal
}

// CodeEdge represents a relationship between two code nodes
type CodeEdge struct {
	Kind      EdgeKind          // calls | imports | defines | inherits | uses
	FromID    string            // Source node ID
	ToID      string            // Target node ID
	CallSite  string            // Location of the edge (e.g., "file.go:42")
	IsDynamic bool              // Whether the edge is dynamic (e.g., interface call)
	CallType  string            // direct | interface | callback | goroutine | deferred
	Metadata  map[string]string // Additional metadata
}

// Repo represents a source code repository
type Repo struct {
	Slug         string     // Unique repository identifier
	Path         string     // Local filesystem path
	RemoteURL    string     // Remote repository URL
	DefaultBranch string    // Default branch name
	Languages    []string   // Supported languages in this repo
	LastCommit   string     // Last commit hash
	LastIndexedAt *time.Time // Timestamp of last indexing
	CreatedAt    time.Time  // Timestamp of creation
}
