package types

import "time"

// NodeKind represents the type of a code node.
type NodeKind string

const (
	NodeFile     NodeKind = "file"
	NodeFunction NodeKind = "function"
	NodeClass    NodeKind = "class"
	NodeModule   NodeKind = "module"
)

// EdgeKind represents the type of a relationship between nodes.
type EdgeKind string

const (
	EdgeCalls    EdgeKind = "calls"
	EdgeImports  EdgeKind = "imports"
	EdgeDefines  EdgeKind = "defines"
	EdgeInherits EdgeKind = "inherits"
	EdgeUses     EdgeKind = "uses"
)

// CodeNode represents a single entity in the codebase (function, class, file, module).
type CodeNode struct {
	Language    string
	Docstring   string
	Name        string
	Qualified   string
	FilePath    string
	RepoSlug    string
	Kind        NodeKind
	Visibility  string
	ID          string
	Signature   string
	ContentHash string
	Body        string
	Embedding   []float32
	StartLine   int
	EndLine     int
}

// CodeEdge represents a relationship between two code nodes.
type CodeEdge struct {
	Metadata  map[string]string
	Kind      EdgeKind
	FromID    string
	ToID      string
	CallSite  string
	CallType  string
	IsDynamic bool
}

// Repo represents a source code repository.
type Repo struct {
	CreatedAt     time.Time
	LastIndexedAt *time.Time
	Slug          string
	Path          string
	RemoteURL     string
	DefaultBranch string
	LastCommit    string
	Languages     []string
}
