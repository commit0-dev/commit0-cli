package types

import "time"

type EventType = string

const (
	EventNodeCreated    EventType = "node.created"
	EventNodeUpdated    EventType = "node.updated"
	EventNodeDeleted    EventType = "node.deleted"
	EventEdgeCreated    EventType = "edge.created"
	EventEdgeDeleted    EventType = "edge.deleted"
	EventIndexStarted   EventType = "index.started"
	EventIndexCompleted EventType = "index.completed"
	EventRepoCreated    EventType = "repo.created"
	EventRepoDeleted    EventType = "repo.deleted"
)

type Event struct {
	ID        string         `json:"id"`
	Type      EventType      `json:"type"`
	RepoSlug  string         `json:"repo_slug"`
	AuthorID  string         `json:"author_id"`
	Timestamp time.Time      `json:"timestamp"`
	Payload   map[string]any `json:"payload"`
	Source    string         `json:"source"`
}

type EventFilter struct {
	RepoSlug string     `json:"repo_slug,omitempty"`
	Types    []string   `json:"types,omitempty"`
	Source   string     `json:"source,omitempty"`
	Since    *time.Time `json:"since,omitempty"`
	Until    *time.Time `json:"until,omitempty"`
	Limit    int        `json:"limit,omitempty"`
}
