package types

import "time"

// ---------------------------------------------------------------------------
// P2P Graph Sync — Types
// ---------------------------------------------------------------------------

// PeerInfo identifies a remote commit0 server for P2P sync.
type PeerInfo struct {
	Name       string     // user-assigned name (like git remote)
	Endpoint   string     // host:port for QUIC data plane
	APIURL     string     // HTTP control plane URL
	AddedAt    time.Time
	LastSyncAt *time.Time
}

// SyncScope declares that a repo is in the local sync scope.
type SyncScope struct {
	RepoSlug string
	AddedAt  time.Time
}

// GraphBundle is the serializable unit of graph sync — the complete
// graph skeleton for a single repo at a specific commit.
// Excludes Body, Embedding, Summary, and Concepts (see SyncNode).
type GraphBundle struct {
	FormatVersion int
	RepoSlug      string
	RemoteURL     string // canonical normalized URL
	LastCommit    string
	Languages     []string
	CreatedAt     time.Time
	Nodes         []SyncNode
	Edges         []SyncEdge
	ContentHash   string // SHA-256 of canonical CBOR(Nodes+Edges)
	Signature     string // HMAC or vendor-specific signature
}

// SyncNode is the syncable subset of CodeNode.
// Excludes: Body (use git clone), Embedding (re-embed locally),
// Summary and Concepts (LLM-generated, non-deterministic).
type SyncNode struct {
	ID                 string
	Kind               NodeKind
	Name               string
	Qualified          string
	FilePath           string
	RepoSlug           string
	Language           string
	Visibility         string
	Signature          string
	Docstring          string
	ContentHash        string
	StartLine          int
	EndLine            int
	IntroducedCommit   string
	LastModifiedCommit string
	IntroducedAt       *time.Time
	LastModifiedAt     *time.Time
}

// SyncEdge is the syncable representation of CodeEdge.
// All fields are included (edges are small metadata).
type SyncEdge struct {
	Kind             EdgeKind
	FromID           string
	ToID             string
	CallSite         string
	CallType         string
	IsDynamic        bool
	Metadata         map[string]string
	IntroducedCommit string
	IntroducedAt     *time.Time
	RemovedCommit    string // NOTE: no RemovedAt timestamp on CodeEdge
}

// SyncManifest is a lightweight summary for "do I need to pull?" checks.
// Contains NO code intelligence — safe to exchange before authentication.
type SyncManifest struct {
	RepoSlug    string
	RemoteURL   string
	LastCommit  string
	ContentHash string
	NodeCount   int
	EdgeCount   int
	UpdatedAt   time.Time
}

// SyncDelta represents incremental changes since a known state.
// Used with SurrealDB changefeeds for efficient sync.
type SyncDelta struct {
	RepoSlug      string
	BaseCommit    string // the commit the requester already has
	TargetCommit  string // the commit this delta brings them to
	NodesUpserted []SyncNode
	NodesRemoved  []string // IDs of removed nodes
	EdgesUpserted []SyncEdge
	EdgesRemoved  []string // "from->kind->to" identifiers
	ContentHash   string
	Signature     string
}

// SyncResult summarizes the outcome of a sync operation.
type SyncResult struct {
	RepoSlug      string
	Direction     string // "pull", "push", "import", "export"
	PeerName      string
	NodesImported int
	EdgesImported int
	NodesSkipped  int // unchanged (ContentHash match)
	ReEmbedQueued bool
	Timing        TimingInfo
}
