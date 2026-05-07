package git

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/commit0-dev/commit0/server/internal/domain"
)

// Compile-time interface check.
var _ domain.GitWalker = (*Walker)(nil)

// Walker implements domain.GitWalker using os/exec git commands.
type Walker struct {
	log *slog.Logger
}

// NewWalker creates a new Git walker.
func NewWalker(log *slog.Logger) *Walker {
	return &Walker{log: log.With("adapter", "git")}
}

// ListCommits returns commits in chronological order between two refs.
// If fromRef is empty, returns all commits up to toRef.
// If toRef is empty, defaults to HEAD.
func (w *Walker) ListCommits(ctx context.Context, repoPath string, fromRef, toRef string) ([]domain.GitCommit, error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("abs path: %w", err)
	}

	// Format: hash|author|unix_timestamp|subject
	args := []string{"-C", absPath, "log", "--format=%H|%an|%at|%s", "--reverse"}

	if fromRef != "" && toRef != "" {
		args = append(args, fromRef+".."+toRef)
	} else if toRef != "" {
		args = append(args, toRef)
	} else if fromRef != "" {
		args = append(args, fromRef+"..HEAD")
	}

	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	var commits []domain.GitCommit
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		ts, _ := strconv.ParseInt(parts[2], 10, 64)
		commits = append(commits, domain.GitCommit{
			Hash:      parts[0],
			Author:    parts[1],
			Timestamp: time.Unix(ts, 0),
			Message:   parts[3],
		})
	}

	w.log.Debug("listed commits", "repo", repoPath, "count", len(commits))
	return commits, nil
}

// DiffCommit returns the files changed in a specific commit with diff stats.
func (w *Walker) DiffCommit(ctx context.Context, repoPath, commitHash string) ([]domain.GitFileDiff, error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("abs path: %w", err)
	}

	// Get file status and stats
	args := []string{"-C", absPath, "diff-tree", "--no-commit-id", "-r", "--numstat", commitHash}
	statsOut, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff-tree: %w", err)
	}

	// Parse numstat: "additions\tdeletions\tfilepath"
	fileStats := make(map[string]*domain.GitFileDiff)
	scanner := bufio.NewScanner(strings.NewReader(string(statsOut)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		adds, _ := strconv.Atoi(parts[0])
		dels, _ := strconv.Atoi(parts[1])
		path := parts[2]

		// Handle renames: "old => new" or "{old => new}/rest"
		oldPath := ""
		if strings.Contains(path, " => ") {
			idx := strings.Index(path, " => ")
			oldPath = strings.TrimSpace(path[:idx])
			path = strings.TrimSpace(path[idx+4:])
		}

		status := "modified"
		if adds > 0 && dels == 0 {
			status = "added"
		}
		if oldPath != "" {
			status = "renamed"
		}

		fileStats[path] = &domain.GitFileDiff{
			Path:      path,
			OldPath:   oldPath,
			Status:    status,
			Additions: adds,
			Deletions: dels,
		}
	}

	// Get the actual patch for each file
	patchArgs := []string{"-C", absPath, "diff-tree", "--no-commit-id", "-r", "-p", commitHash}
	patchOut, err := exec.CommandContext(ctx, "git", patchArgs...).Output()
	if err != nil {
		// Non-fatal: we have stats, just no patches
		w.log.Debug("git diff-tree -p failed", "err", err)
	} else {
		// Parse unified diff and assign patches to files
		assignPatches(string(patchOut), fileStats)
	}

	var diffs []domain.GitFileDiff
	for _, d := range fileStats {
		diffs = append(diffs, *d)
	}

	w.log.Debug("diffed commit", "hash", commitHash, "files", len(diffs))
	return diffs, nil
}

// ReadFileAtCommit returns the content of a file at a specific commit.
func (w *Walker) ReadFileAtCommit(ctx context.Context, repoPath, commitHash, filePath string) ([]byte, error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("abs path: %w", err)
	}

	ref := commitHash + ":" + filePath
	out, err := exec.CommandContext(ctx, "git", "-C", absPath, "show", ref).Output()
	if err != nil {
		return nil, fmt.Errorf("git show %s: %w", ref, err)
	}
	return out, nil
}

// CommitInfo returns full metadata for a specific commit, including the
// full commit message body (not just the subject line).
func (w *Walker) CommitInfo(ctx context.Context, repoPath, commitHash string) (*domain.GitCommit, error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("abs path: %w", err)
	}

	// Get commit metadata with full body
	format := "%H%n%an%n%at%n%B"
	out, err := exec.CommandContext(ctx, "git", "-C", absPath, "log", "-1", "--format="+format, commitHash).Output()
	if err != nil {
		return nil, fmt.Errorf("git log -1: %w", err)
	}

	lines := strings.SplitN(string(out), "\n", 4)
	if len(lines) < 4 {
		return nil, fmt.Errorf("unexpected git log output for %s", commitHash)
	}

	ts, _ := strconv.ParseInt(strings.TrimSpace(lines[2]), 10, 64)
	message := strings.TrimSpace(lines[3])

	// Get changed files list
	filesOut, err := exec.CommandContext(ctx, "git", "-C", absPath, "diff-tree", "--no-commit-id", "-r", "--name-only", commitHash).Output()
	if err != nil {
		w.log.Debug("diff-tree --name-only failed", "err", err)
	}
	var files []string
	if filesOut != nil {
		for _, f := range strings.Split(string(filesOut), "\n") {
			f = strings.TrimSpace(f)
			if f != "" {
				files = append(files, f)
			}
		}
	}

	return &domain.GitCommit{
		Hash:      strings.TrimSpace(lines[0]),
		Author:    strings.TrimSpace(lines[1]),
		Timestamp: time.Unix(ts, 0),
		Message:   message,
		Files:     files,
	}, nil
}

// DiffWorkingTree returns staged + unstaged changes compared to HEAD.
// It shells out to "git diff HEAD" to get the unified diff patch text, and
// "git diff HEAD --name-status" to populate the per-file status and paths.
func (w *Walker) DiffWorkingTree(ctx context.Context, repoPath string) ([]domain.GitFileDiff, error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("abs path: %w", err)
	}

	// Get file status and stats (staged + unstaged vs HEAD).
	nameStatusOut, err := exec.CommandContext(ctx, "git", "-C", absPath,
		"diff", "HEAD", "--name-status", "--no-color").Output()
	if err != nil {
		// HEAD may not exist (empty repo) — return empty slice.
		w.log.Debug("git diff HEAD --name-status failed", "err", err)
		return nil, nil
	}

	fileStats := parseNameStatus(string(nameStatusOut))
	if len(fileStats) == 0 {
		return nil, nil
	}

	// Get numeric stats (--numstat) for additions/deletions.
	numstatOut, err := exec.CommandContext(ctx, "git", "-C", absPath,
		"diff", "HEAD", "--numstat", "--no-color").Output()
	if err == nil {
		applyNumstat(string(numstatOut), fileStats)
	}

	// Get the unified diff patch.
	patchOut, err := exec.CommandContext(ctx, "git", "-C", absPath,
		"diff", "HEAD", "--no-color").Output()
	if err == nil {
		assignPatches(string(patchOut), fileStats)
	}

	diffs := fileStatsToDiffs(fileStats)
	w.log.Debug("diff working tree", "repo", repoPath, "files", len(diffs))
	return diffs, nil
}

// DiffRange returns the diff between two git refs.
// If toRef is the literal "WORKING", it delegates to DiffWorkingTree.
func (w *Walker) DiffRange(ctx context.Context, repoPath, fromRef, toRef string) ([]domain.GitFileDiff, error) {
	if toRef == "WORKING" {
		return w.DiffWorkingTree(ctx, repoPath)
	}

	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("abs path: %w", err)
	}

	rangeSpec := fromRef + ".." + toRef

	// Get file name + status.
	nameStatusOut, err := exec.CommandContext(ctx, "git", "-C", absPath,
		"diff", rangeSpec, "--name-status", "--no-color").Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s --name-status: %w", rangeSpec, err)
	}

	fileStats := parseNameStatus(string(nameStatusOut))
	if len(fileStats) == 0 {
		return nil, nil
	}

	// Numeric stats for additions/deletions.
	numstatOut, err := exec.CommandContext(ctx, "git", "-C", absPath,
		"diff", rangeSpec, "--numstat", "--no-color").Output()
	if err == nil {
		applyNumstat(string(numstatOut), fileStats)
	}

	// Unified diff patch.
	patchOut, err := exec.CommandContext(ctx, "git", "-C", absPath,
		"diff", rangeSpec, "--no-color").Output()
	if err == nil {
		assignPatches(string(patchOut), fileStats)
	}

	diffs := fileStatsToDiffs(fileStats)
	w.log.Debug("diff range", "repo", repoPath, "range", rangeSpec, "files", len(diffs))
	return diffs, nil
}

// parseNameStatus parses "git diff --name-status" output into a map keyed by
// the new (or only) file path.  Status letters: A=added, M=modified,
// D=deleted, Rnn=renamed (e.g. R100), Cnn=copied.
func parseNameStatus(output string) map[string]*domain.GitFileDiff {
	fileStats := make(map[string]*domain.GitFileDiff)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		statusCode := parts[0]
		var status, path, oldPath string

		switch {
		case strings.HasPrefix(statusCode, "R"), strings.HasPrefix(statusCode, "C"):
			if len(parts) < 3 {
				continue
			}
			oldPath = parts[1]
			path = parts[2]
			status = "renamed"
		case statusCode == "A":
			path = parts[1]
			status = "added"
		case statusCode == "D":
			path = parts[1]
			status = "deleted"
		default: // "M", "T", "U", etc.
			path = parts[1]
			status = "modified"
		}

		fileStats[path] = &domain.GitFileDiff{
			Path:    path,
			OldPath: oldPath,
			Status:  status,
		}
	}
	return fileStats
}

// applyNumstat merges additions/deletions from "git diff --numstat" output
// into the existing fileStats map.
func applyNumstat(output string, fileStats map[string]*domain.GitFileDiff) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// numstat format: "adds\tdels\tpath" — path may be "{old => new}" for renames
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		adds, _ := strconv.Atoi(parts[0])
		dels, _ := strconv.Atoi(parts[1])
		rawPath := parts[2]

		// Resolve rename notation "{old/path => new/path}" or "path/{old => new}"
		path := resolveRenameNotation(rawPath)

		if d, ok := fileStats[path]; ok {
			d.Additions = adds
			d.Deletions = dels
		}
	}
}

// resolveRenameNotation converts git rename notation like "{old => new}/file" or
// "prefix/{old => new}" to the new path.
func resolveRenameNotation(path string) string {
	open := strings.Index(path, "{")
	arrow := strings.Index(path, " => ")
	close := strings.Index(path, "}")
	if open < 0 || arrow < 0 || close < 0 || arrow < open || close < arrow {
		return path
	}
	prefix := path[:open]
	newPart := path[arrow+4 : close]
	suffix := path[close+1:]
	return prefix + newPart + suffix
}

// fileStatsToDiffs converts the fileStats map into a slice.
func fileStatsToDiffs(fileStats map[string]*domain.GitFileDiff) []domain.GitFileDiff {
	diffs := make([]domain.GitFileDiff, 0, len(fileStats))
	for _, d := range fileStats {
		diffs = append(diffs, *d)
	}
	return diffs
}

// assignPatches parses a unified diff output and assigns patches to the
// corresponding GitFileDiff entries keyed by file path.
func assignPatches(patchText string, files map[string]*domain.GitFileDiff) {
	// Split on "diff --git" markers
	sections := strings.Split(patchText, "diff --git ")
	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}
		// First line: "a/path b/path"
		firstNewline := strings.IndexByte(section, '\n')
		if firstNewline < 0 {
			continue
		}
		header := section[:firstNewline]
		parts := strings.SplitN(header, " ", 2)
		if len(parts) < 2 {
			continue
		}
		// Extract path from "b/path"
		path := strings.TrimPrefix(parts[1], "b/")

		if d, ok := files[path]; ok {
			// Store the patch content (everything after the header line)
			d.Patch = section[firstNewline+1:]
			// Truncate large patches to avoid memory issues
			if len(d.Patch) > 10000 {
				d.Patch = d.Patch[:10000] + "\n... (truncated)"
			}
		}
	}
}
