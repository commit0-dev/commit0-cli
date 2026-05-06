package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// TemporalIndexRequest configures a temporal indexing run across git history.
type TemporalIndexRequest struct {
	RepoPath   string
	RepoSlug   string
	FromCommit string // start of range (empty = all history)
	ToCommit   string // end of range (empty = HEAD)
}

// TemporalQueryRequest configures a temporal history query.
type TemporalQueryRequest struct {
	RepoSlug      string
	NodeQualified string // specific node to query history for
	FromCommit    string
	ToCommit      string
}

// TemporalService provides temporal (git-aware) code graph operations.
// It walks git history to track when nodes and edges were introduced or modified,
// enabling "when was this function added?" and "what changed between commits?" queries.
type TemporalService struct {
	graph     domain.OpenCodeGraph
	tempStore domain.TemporalStore
	gitWalker domain.GitWalker
	parser    domain.Parser
	log       *slog.Logger
}

// NewTemporalService creates a temporal service.
func NewTemporalService(
	graph domain.OpenCodeGraph,
	tempStore domain.TemporalStore,
	gitWalker domain.GitWalker,
	parser domain.Parser,
) *TemporalService {
	return &TemporalService{
		graph:     graph,
		tempStore: tempStore,
		gitWalker: gitWalker,
		parser:    parser,
		log:       slog.Default().With("service", "temporal"),
	}
}

// IndexCommitRange walks git history and diff-indexes each commit,
// marking introduced_commit and last_modified_commit on nodes and edges.
func (s *TemporalService) IndexCommitRange(ctx context.Context, req TemporalIndexRequest) error {
	startTime := time.Now()

	commits, err := s.gitWalker.ListCommits(ctx, req.RepoPath, req.FromCommit, req.ToCommit)
	if err != nil {
		return err
	}

	s.log.Info("temporal indexing started",
		"repo", req.RepoSlug,
		"commits", len(commits),
		"from", req.FromCommit,
		"to", req.ToCommit,
	)

	for i, commit := range commits {
		if err := ctx.Err(); err != nil {
			return err
		}

		diffs, err := s.gitWalker.DiffCommit(ctx, req.RepoPath, commit.Hash)
		if err != nil {
			s.log.Warn("diff failed, skipping commit", "hash", commit.Hash, "err", err)
			continue
		}

		nodesChanged := 0
		for _, diff := range diffs {
			if diff.Status == "deleted" {
				continue
			}

			// Read file content at this commit
			content, err := s.gitWalker.ReadFileAtCommit(ctx, req.RepoPath, commit.Hash, diff.Path)
			if err != nil {
				continue
			}

			// Parse the file
			parsed, err := s.parser.Parse(ctx, domain.FileEntry{
				Path:     diff.Path,
				AbsPath:  diff.Path,
				Language: languageFromPath(diff.Path),
				Content:  content,
			})
			if err != nil {
				continue
			}

			// For each node in the parsed file, update temporal metadata
			for _, node := range parsed.Nodes {
				node.RepoSlug = req.RepoSlug

				if diff.Status == "added" {
					// New file — all nodes are introduced at this commit
					if err := s.tempStore.UpsertNodeTemporal(ctx, &node, commit.Hash, commit.Timestamp); err != nil {
						s.log.Debug("upsert node temporal failed", "node", node.Qualified, "err", err)
					}
				} else {
					// Modified file — mark as last modified
					existing, _ := s.graph.FindNode(ctx, req.RepoSlug, node.Qualified)
					if existing == nil {
						// New node in modified file
						if err := s.tempStore.UpsertNodeTemporal(ctx, &node, commit.Hash, commit.Timestamp); err != nil {
							s.log.Debug("upsert node temporal failed", "node", node.Qualified, "err", err)
						}
					} else if existing.ContentHash != node.ContentHash {
						// Node changed — update last_modified
						existing.LastModifiedCommit = commit.Hash
						existing.LastModifiedAt = &commit.Timestamp
						if err := s.tempStore.UpsertNodeTemporal(ctx, existing, commit.Hash, commit.Timestamp); err != nil {
							s.log.Debug("update node temporal failed", "node", node.Qualified, "err", err)
						}
					}
				}
				nodesChanged++
			}

			// Update edges with temporal metadata
			for _, edge := range parsed.Edges {
				if err := s.tempStore.UpsertEdgeTemporal(ctx, &edge, commit.Hash, commit.Timestamp); err != nil {
					s.log.Debug("upsert edge temporal failed", "err", err)
				}
			}
		}

		if nodesChanged > 0 {
			s.log.Debug("indexed commit",
				"i", i+1, "of", len(commits),
				"hash", commit.Hash[:8],
				"files", len(diffs),
				"nodes", nodesChanged,
			)
		}
	}

	s.log.Info("temporal indexing complete",
		"repo", req.RepoSlug,
		"commits", len(commits),
		"duration", time.Since(startTime),
	)
	return nil
}

// QueryHistory returns temporal changes for a node or commit range.
func (s *TemporalService) QueryHistory(ctx context.Context, req TemporalQueryRequest) ([]types.TemporalChange, error) {
	if req.NodeQualified != "" {
		// Query specific node's history
		node, err := s.graph.FindNode(ctx, req.RepoSlug, req.NodeQualified)
		if err != nil {
			return nil, err
		}
		if node == nil {
			return nil, domain.NotFound(fmt.Sprintf("symbol %s not found", req.NodeQualified))
		}
		if s.tempStore == nil {
			return nil, domain.Validation("temporal store not available")
		}
		return s.tempStore.NodeHistory(ctx, node.ID)
	}

	// Query range
	return s.tempStore.QueryTemporalRange(ctx, req.RepoSlug, req.FromCommit, req.ToCommit)
}

// languageFromPath derives language from file extension.
func languageFromPath(path string) string {
	switch {
	case hasAnySuffix(path, ".go"):
		return "go"
	case hasAnySuffix(path, ".ts", ".tsx"):
		return "typescript"
	case hasAnySuffix(path, ".js", ".jsx"):
		return "javascript"
	case hasAnySuffix(path, ".py"):
		return "python"
	default:
		return ""
	}
}

func hasAnySuffix(s string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
			return true
		}
	}
	return false
}
