package types

import "time"

// ---------------------------------------------------------------------------
// Knowledge node kinds — extends NodeKind beyond pure code entities.
// ---------------------------------------------------------------------------
//
// Closes platform-readiness gap A3 (code graph cosplaying as knowledge
// graph). The four code-only kinds (file, function, class, module) admit
// only what tree-sitter parses out of source files. A real engineering
// knowledge graph also contains:
//
//   * People — engineers, owners, on-call rotations
//   * Decisions — ADRs, RFCs, design choices
//   * Incidents — outages, postmortems, action items
//   * Deploys — releases, rollbacks
//   * Runbooks — playbooks, troubleshooting guides
//   * Conversations — Slack threads, PR discussions, design reviews
//
// All knowledge nodes live in the same OpenCodeGraph and participate in
// vector search and graph traversal — `decision X references function Y`
// is a first-class edge, not a metadata field.

const (
	// NodePerson represents an individual engineer or maintainer.
	NodePerson NodeKind = "person"
	// NodeDecision represents an architectural decision record (ADR), RFC,
	// or design memo. Body holds the markdown rationale.
	NodeDecision NodeKind = "decision"
	// NodeIncident represents an outage or production issue.
	NodeIncident NodeKind = "incident"
	// NodeDeploy represents a release event tied to one or more commits.
	NodeDeploy NodeKind = "deploy"
	// NodeRunbook represents an operational playbook or troubleshooting guide.
	NodeRunbook NodeKind = "runbook"
	// NodeConversation represents a discussion thread (Slack, PR review).
	NodeConversation NodeKind = "conversation"
)

// IsKnowledgeKind reports whether the kind belongs to the knowledge-graph
// extension (i.e. is not one of the original code-only kinds).
func IsKnowledgeKind(kind NodeKind) bool {
	switch kind {
	case NodePerson, NodeDecision, NodeIncident, NodeDeploy, NodeRunbook, NodeConversation:
		return true
	default:
		return false
	}
}

// AllKnowledgeKinds returns the canonical list of knowledge node kinds.
func AllKnowledgeKinds() []NodeKind {
	return []NodeKind{NodePerson, NodeDecision, NodeIncident, NodeDeploy, NodeRunbook, NodeConversation}
}

// ---------------------------------------------------------------------------
// Knowledge edge kinds — relationships between knowledge and code nodes.
// ---------------------------------------------------------------------------

const (
	// EdgeOwns connects a Person → Function/Class/File they maintain.
	// Source-of-truth for code ownership. Metadata: "since" (RFC3339).
	EdgeOwns EdgeKind = "owns"

	// EdgeAuthored connects a Person → Decision/Runbook/Conversation
	// they wrote. Metadata: "at" (RFC3339).
	EdgeAuthored EdgeKind = "authored"

	// EdgeReferences connects any Knowledge node → any other node it
	// mentions. Bidirectional traversal — "what code does this ADR
	// touch" and "which ADRs touch this function".
	EdgeReferences EdgeKind = "references"

	// EdgeTriggeredBy connects an Incident → the Deploy that caused it.
	// Forward edge: Incident → Deploy.
	EdgeTriggeredBy EdgeKind = "triggered_by"

	// EdgeResolvedBy connects an Incident → the Deploy or Decision that
	// resolved it. Forward edge: Incident → resolution.
	EdgeResolvedBy EdgeKind = "resolved_by"

	// EdgeDocuments connects a Runbook/Decision → a Function/Class/Module
	// it explains. The inverse of EdgeReferences but with stronger
	// semantics: this knowledge IS the documentation for that code.
	EdgeDocuments EdgeKind = "documents"
)

// IsKnowledgeEdge reports whether the edge kind is part of the knowledge
// graph extension.
func IsKnowledgeEdge(kind EdgeKind) bool {
	switch kind {
	case EdgeOwns, EdgeAuthored, EdgeReferences, EdgeTriggeredBy, EdgeResolvedBy, EdgeDocuments:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// KnowledgeNode — domain-rich payload for knowledge content.
// ---------------------------------------------------------------------------

// KnowledgeNode is a typed view over CodeNode for knowledge content. It
// carries the same ID and joins the same graph but exposes fields like
// Title and Status that don't make sense on functions or files.
//
// Persistence is handled by encoding KnowledgeNode → CodeNode (with the
// extra fields packed into Concepts/Body/Metadata) and decoding the
// inverse on read. The conversion lives in app/knowledge_service.go so
// the storage layer stays type-agnostic.
type KnowledgeNode struct {
	ID          string    `json:"id"`
	Kind        NodeKind  `json:"kind"`
	RepoSlug    string    `json:"repo_slug"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	Author      string    `json:"author,omitempty"`
	Status      string    `json:"status,omitempty"` // "draft" | "accepted" | "deprecated" | "open" | "resolved"
	Tags        []string  `json:"tags,omitempty"`
	URL         string    `json:"url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at,omitzero"`
	AccessScope string    `json:"access_scope,omitempty"`
}

// KnowledgeStatus enumerates the legal Status values. Empty is allowed
// (no status). Decision uses draft/accepted/deprecated; Incident uses
// open/resolved; runbooks usually have no status.
const (
	StatusDraft      = "draft"
	StatusAccepted   = "accepted"
	StatusDeprecated = "deprecated"
	StatusOpen       = "open"
	StatusResolved   = "resolved"
)
