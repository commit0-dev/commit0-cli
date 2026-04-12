package types

import "time"

// NodeKind classifies code nodes. Type alias (= string) so plain strings
// are interchangeable — new labels don't need constants (OpenCodeGraph §2.1).
type NodeKind = string

const (
	NodeFile     NodeKind = "file"
	NodeFunction NodeKind = "function"
	NodeClass    NodeKind = "class"
	NodeModule   NodeKind = "module"
)

// EdgeKind classifies edge relationships. Type alias (= string) so plain strings
// are interchangeable — new labels don't need constants (OpenCodeGraph §2.2).
type EdgeKind = string

const (
	EdgeCalls    EdgeKind = "calls"
	EdgeImports  EdgeKind = "imports"
	EdgeDefines  EdgeKind = "defines"
	EdgeInherits EdgeKind = "inherits"
	EdgeUses     EdgeKind = "uses"

	// EdgeDataFlow records that a function passes a specific argument to a named
	// parameter of another function. Metadata keys: "param_name", "arg_expr", "arg_type".
	// Enhanced with: "field_path", "mutation_type", "mutation_expr", "mutation_line".
	EdgeDataFlow EdgeKind = "data_flow"

	// EdgeReads records that a function reads a field or global variable.
	// Metadata key: "field" (qualified field name, e.g. "User.Email").
	EdgeReads EdgeKind = "reads"

	// EdgeWrites records that a function writes a field or global variable.
	// Metadata key: "field" (qualified field name).
	EdgeWrites EdgeKind = "writes"

	// EdgeRoute records that a file registers an HTTP route pointing to a handler function.
	// Metadata keys: "http_method", "http_path", "middleware" (comma-separated), "group_prefix".
	EdgeRoute EdgeKind = "route"

	// EdgeControlFlow connects basic blocks within a function body showing execution order.
	// Metadata keys: "branch_type" (sequential, if_true, if_false, loop_entry, loop_back, return),
	// "condition" (the condition expression text for branching edges).
	EdgeControlFlow EdgeKind = "control_flow"

	// EdgeDataDep connects a variable definition to all points where that definition is used.
	// Metadata keys: "var_name", "def_line", "use_line", "def_type" (assignment, parameter, return_value, for_range).
	EdgeDataDep EdgeKind = "data_dep"
)

// MutationKind classifies how data is transformed at a code point.
type MutationKind string

const (
	MutationNone        MutationKind = "none"
	MutationTransform   MutationKind = "transform"    // string ops, math ops
	MutationTypeConvert MutationKind = "type_convert"  // cast, conversion
	MutationFieldSet    MutationKind = "field_set"     // struct field assignment
	MutationFieldDelete MutationKind = "field_delete"  // map delete, nil assignment
	MutationFilter      MutationKind = "filter"        // conditional inclusion
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
	Summary     string   // LLM-generated semantic description of what this code does
	Concepts    []string // Semantic tags: ["caching", "auth", "middleware"]
	Embedding   []float32
	StartLine   int
	EndLine     int

	// Temporal metadata — tracks when this node was introduced/modified in git history.
	IntroducedCommit   string
	IntroducedAt       *time.Time
	LastModifiedCommit string
	LastModifiedAt     *time.Time
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

	// Temporal metadata — tracks when this edge was introduced/removed.
	IntroducedCommit string
	IntroducedAt     *time.Time
	RemovedCommit    string
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
