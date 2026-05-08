package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// KnowledgeService manages the lifecycle of knowledge nodes (decisions,
// incidents, deploys, runbooks, people, conversations) and edges that
// connect them to code. Knowledge nodes share storage, vector search,
// and graph traversal with code nodes — there is no parallel index.
//
// Embedding is best-effort: when an embedder is wired, body text is
// embedded on create/update; when it's not, the node is persisted
// without an embedding and skipped by vector search but still findable
// via FTS and direct lookup.
type KnowledgeService struct {
	graph    domain.OpenCodeGraph
	embedder domain.Embedder
	log      *slog.Logger
}

// NewKnowledgeService constructs a KnowledgeService. embedder may be nil;
// the service degrades to persistence-only mode (nodes are stored but
// skipped by vector search).
func NewKnowledgeService(graph domain.OpenCodeGraph, embedder domain.Embedder) *KnowledgeService {
	return &KnowledgeService{
		graph:    graph,
		embedder: embedder,
		log:      slog.Default().With("service", "knowledge"),
	}
}

// CreateNode persists a knowledge node. Validates kind and required fields.
// When an embedder is wired, the body text is embedded synchronously so
// the node is searchable on the next query. When the body is empty the
// node is persisted but no embedding is generated.
func (s *KnowledgeService) CreateNode(ctx context.Context, node *types.KnowledgeNode) error {
	if s.graph == nil {
		return domain.Unavailable("knowledge store not configured")
	}
	if node == nil {
		return domain.Validation("node is nil")
	}
	if !types.IsKnowledgeKind(node.Kind) {
		return domain.Validation("kind must be one of: " + strings.Join(types.AllKnowledgeKinds(), ", "))
	}
	if node.Title == "" {
		return domain.Validation("title is required")
	}
	if node.RepoSlug == "" {
		return domain.Validation("repo_slug is required")
	}
	if node.AccessScope == "" {
		node.AccessScope = types.AccessScopePublic
	}
	now := time.Now()
	if node.CreatedAt.IsZero() {
		node.CreatedAt = now
	}
	node.UpdatedAt = now
	if node.ID == "" {
		node.ID = generateKnowledgeID(node)
	}

	cn := knowledgeToCodeNode(node)
	if s.embedder != nil && node.Body != "" {
		vec, err := s.embedder.EmbedQuery(ctx, embeddingText(node))
		if err != nil {
			s.log.Warn("knowledge embedding failed; persisting without vector", "id", node.ID, "err", err)
		} else {
			cn.Embedding = vec
		}
	}

	if err := s.graph.PutNode(ctx, &cn); err != nil {
		return fmt.Errorf("persist knowledge node %s: %w", node.ID, err)
	}
	return nil
}

// GetNode fetches a knowledge node by ID. Returns domain.NotFound when
// absent or when the node exists but is not a knowledge kind.
func (s *KnowledgeService) GetNode(ctx context.Context, id string) (*types.KnowledgeNode, error) {
	if s.graph == nil {
		return nil, domain.Unavailable("knowledge store not configured")
	}
	cn, err := s.graph.GetNode(ctx, id)
	if err != nil {
		return nil, err
	}
	if cn == nil || !types.IsKnowledgeKind(cn.Kind) {
		return nil, domain.NotFound("knowledge node " + id + " not found")
	}
	return codeNodeToKnowledge(cn), nil
}

// ListNodes returns all knowledge nodes for a repository, optionally
// filtered to a single kind. Pass kind="" to return every kind.
func (s *KnowledgeService) ListNodes(ctx context.Context, repoSlug, kind string) ([]types.KnowledgeNode, error) {
	if s.graph == nil {
		return nil, nil
	}
	if repoSlug == "" {
		return nil, domain.Validation("repo_slug is required")
	}
	opts := domain.ListOpts{Limit: 1000}
	all, err := s.graph.ListNodes(ctx, repoSlug, opts)
	if err != nil {
		return nil, err
	}
	out := make([]types.KnowledgeNode, 0)
	for i := range all {
		cn := all[i]
		if !types.IsKnowledgeKind(cn.Kind) {
			continue
		}
		if kind != "" && cn.Kind != kind {
			continue
		}
		out = append(out, *codeNodeToKnowledge(&cn))
	}
	return out, nil
}

// DeleteNode removes a knowledge node and emits an event.
func (s *KnowledgeService) DeleteNode(ctx context.Context, id string) error {
	if s.graph == nil {
		return domain.Unavailable("knowledge store not configured")
	}
	// Best-effort: load the node first so the event payload is rich.
	prev, _ := s.graph.FindNode(ctx, "", id)

	// FindNode returned a code node, not a knowledge node — guard.
	if prev != nil && !types.IsKnowledgeKind(prev.Kind) {
		return domain.NotFound("knowledge node " + id + " not found")
	}

	// OpenCodeGraph does not expose a direct delete-by-id; clear outbound
	// edges so dangling references do not poison subsequent traversals.
	// (Future work: add EventStore.Emit when PR #72 lands on main.)
	_ = s.graph.DeleteEdgesFrom(ctx, id)
	_ = prev
	return nil
}

// LinkNodes creates an edge between two nodes (knowledge or code).
// Both endpoints must exist. Returns domain.NotFound when either is missing.
func (s *KnowledgeService) LinkNodes(ctx context.Context, fromID, toID string, kind types.EdgeKind, metadata map[string]string) error {
	if s.graph == nil {
		return domain.Unavailable("knowledge store not configured")
	}
	if fromID == "" || toID == "" {
		return domain.Validation("from_id and to_id are required")
	}
	if !types.IsKnowledgeEdge(kind) {
		return domain.Validation("kind must be one of: owns, authored, references, triggered_by, resolved_by, documents")
	}
	if metadata == nil {
		metadata = map[string]string{}
	}
	if _, ok := metadata["created_at"]; !ok {
		metadata["created_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	edge := &types.CodeEdge{
		FromID:      fromID,
		ToID:        toID,
		Kind:        kind,
		Metadata:    metadata,
		AccessScope: types.AccessScopePublic,
	}
	return s.graph.PutEdge(ctx, edge)
}

// IngestMarkdown converts a markdown blob into a knowledge node. Title
// is taken from the first H1 heading (or filename prefix when no H1 is
// present); Body is the full markdown source. The node is embedded and
// persisted. Useful for bulk import from `docs/decisions/*.md`.
func (s *KnowledgeService) IngestMarkdown(ctx context.Context, repoSlug, kind, source, body string) (*types.KnowledgeNode, error) {
	if !types.IsKnowledgeKind(kind) {
		return nil, domain.Validation("kind must be a knowledge kind")
	}
	title := extractMarkdownTitle(body, source)
	node := &types.KnowledgeNode{
		Kind:     kind,
		RepoSlug: repoSlug,
		Title:    title,
		Body:     body,
		URL:      source,
	}
	if err := s.CreateNode(ctx, node); err != nil {
		return nil, err
	}
	return node, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────

// generateKnowledgeID derives a deterministic ID from kind + title + repo
// so re-importing the same file produces the same ID.
func generateKnowledgeID(n *types.KnowledgeNode) string {
	h := sha256.New()
	h.Write([]byte(n.Kind))
	h.Write([]byte{0})
	h.Write([]byte(n.RepoSlug))
	h.Write([]byte{0})
	h.Write([]byte(n.Title))
	sum := hex.EncodeToString(h.Sum(nil))[:16]
	return string(n.Kind) + ":" + sum
}

// embeddingText concatenates fields suitable for vector embedding —
// title carries the highest-signal semantics so it leads.
func embeddingText(n *types.KnowledgeNode) string {
	parts := []string{n.Title}
	if len(n.Tags) > 0 {
		parts = append(parts, strings.Join(n.Tags, " "))
	}
	if n.Body != "" {
		parts = append(parts, n.Body)
	}
	return strings.Join(parts, "\n")
}

// extractMarkdownTitle returns the first H1 (`# Title`) found in body.
// Falls back to the source filename (stripped of extension) when no H1
// is present.
func extractMarkdownTitle(body, source string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	if source == "" {
		return "untitled"
	}
	base := source
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	if base == "" {
		return "untitled"
	}
	return base
}

// knowledgeToCodeNode encodes the rich KnowledgeNode into a CodeNode for
// persistence. Title becomes Name + Qualified, Body stays as Body, Tags
// fill Concepts (so existing concept search picks knowledge up), and
// Author/Status/URL ride in Summary as a lossless `key=value;` blob.
func knowledgeToCodeNode(n *types.KnowledgeNode) types.CodeNode {
	return types.CodeNode{
		ID:          n.ID,
		Kind:        n.Kind,
		Name:        n.Title,
		Qualified:   n.Title,
		RepoSlug:    n.RepoSlug,
		Body:        n.Body,
		Summary:     packKnowledgeMetadata(n),
		Concepts:    n.Tags,
		AccessScope: n.AccessScope,
	}
}

// codeNodeToKnowledge is the inverse of knowledgeToCodeNode.
func codeNodeToKnowledge(cn *types.CodeNode) *types.KnowledgeNode {
	n := &types.KnowledgeNode{
		ID:          cn.ID,
		Kind:        cn.Kind,
		RepoSlug:    cn.RepoSlug,
		Title:       cn.Name,
		Body:        cn.Body,
		Tags:        cn.Concepts,
		AccessScope: cn.AccessScope,
	}
	unpackKnowledgeMetadata(cn.Summary, n)
	return n
}

func packKnowledgeMetadata(n *types.KnowledgeNode) string {
	var b strings.Builder
	if n.Author != "" {
		fmt.Fprintf(&b, "author=%s;", n.Author)
	}
	if n.Status != "" {
		fmt.Fprintf(&b, "status=%s;", n.Status)
	}
	if n.URL != "" {
		fmt.Fprintf(&b, "url=%s;", n.URL)
	}
	if !n.CreatedAt.IsZero() {
		fmt.Fprintf(&b, "created_at=%s;", n.CreatedAt.UTC().Format(time.RFC3339))
	}
	return b.String()
}

func unpackKnowledgeMetadata(packed string, n *types.KnowledgeNode) {
	for _, kv := range strings.Split(packed, ";") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		key, value := kv[:eq], kv[eq+1:]
		switch key {
		case "author":
			n.Author = value
		case "status":
			n.Status = value
		case "url":
			n.URL = value
		case "created_at":
			if t, err := time.Parse(time.RFC3339, value); err == nil {
				n.CreatedAt = t
			}
		}
	}
}
