package walker

import (
	"bufio"
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/commit0-dev/commit0/internal/domain"
)

// extensionLanguage maps lower-case file extensions to language names.
var extensionLanguage = map[string]string{
	".go":  "go",
	".py":  "python",
	".ts":  "typescript",
	".tsx": "typescript",
	".js":  "javascript",
	".jsx": "javascript",
}

// filenameLanguage maps exact filenames (no extension match) to language names.
// These are special project files that contain dependency/module metadata.
var filenameLanguage = map[string]string{
	"go.mod": "gomod",
}

// FSWalker implements domain.FileWalker using filepath.WalkDir.
// It respects .gitignore (simple pattern matching), skips VCS/vendor
// directories, and enforces optional language and size filters.
type FSWalker struct {
	log *slog.Logger
}

// Compile-time interface check.
var _ domain.FileWalker = (*FSWalker)(nil)

// NewFSWalker creates a new FSWalker with the default logger.
func NewFSWalker(log *slog.Logger) *FSWalker {
	if log == nil {
		log = slog.Default()
	}
	return &FSWalker{log: log}
}

// Walk traverses repoPath, emitting accepted FileEntry values on the returned
// channel. Any terminal error is sent on the error channel. Both channels are
// closed when the walk completes or the context is canceled.
//
// The caller must drain fileCh to avoid blocking the walk goroutine.
func (w *FSWalker) Walk(ctx context.Context, repoPath string, opts domain.WalkOpts) (<-chan domain.FileEntry, <-chan error) {
	fileCh := make(chan domain.FileEntry, 64)
	errCh := make(chan error, 1)

	go func() {
		defer close(fileCh)
		defer close(errCh)

		gitignore := loadGitignore(repoPath)
		langFilter := makeSet(opts.Languages)

		walkErr := filepath.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// Log and skip entries that returned an OS error.
				w.log.Warn("walk: skipping entry", slog.String("path", path), slog.Any("err", err))
				return nil
			}

			// ── Directories ───────────────────────────────────────────────
			if d.IsDir() {
				name := d.Name()
				switch name {
				case ".git", ".svn", ".hg", "node_modules", "vendor", ".tox", "__pycache__", ".mypy_cache", "dist", "build":
					return filepath.SkipDir
				}
				return nil
			}

			// ── Context cancellation ───────────────────────────────────────
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			// ── Relative path ──────────────────────────────────────────────
			// filepath.Rel only errors when paths are not both absolute or both
			// relative — unreachable here because we always pass absolute paths.
			rel, _ := filepath.Rel(repoPath, path)
			// Normalise path separators to forward slashes for portability.
			rel = filepath.ToSlash(rel)

			// ── .gitignore filter ─────────────────────────────────────────
			if gitignore.match(rel) {
				return nil
			}

			// ── User-supplied exclude globs ───────────────────────────────
			base := filepath.Base(path)
			for _, pattern := range opts.Exclude {
				if matched, _ := filepath.Match(pattern, base); matched {
					return nil
				}
				// Also match against the relative path.
				if matched, _ := filepath.Match(pattern, rel); matched {
					return nil
				}
			}

			// ── Language detection ────────────────────────────────────────
			baseName := filepath.Base(path)
			langName, ok := filenameLanguage[baseName]
			if !ok {
				ext := strings.ToLower(filepath.Ext(path))
				langName, ok = extensionLanguage[ext]
				if !ok {
					return nil
				}
			}
			if len(langFilter) > 0 && !langFilter[langName] {
				// gomod files are always included when "go" language is selected.
				if !(langName == "gomod" && langFilter["go"]) {
					return nil
				}
			}

			// ── Size limit ────────────────────────────────────────────────
			if opts.MaxFileKB > 0 {
				info, err := d.Info()
				if err != nil || info == nil {
					return nil //nolint:nilerr // stat failure skips the file; walk continues
				}
				if info.Size() > int64(opts.MaxFileKB)*1024 {
					w.log.Debug("walk: skipping oversized file",
						slog.String("path", rel),
						slog.Int64("sizeBytes", info.Size()),
						slog.Int("maxKB", opts.MaxFileKB))
					return nil
				}
			}

			// ── Read file ─────────────────────────────────────────────────
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				w.log.Warn("walk: cannot read file", slog.String("path", rel), slog.Any("err", readErr))
				return nil
			}

			// ── Emit ──────────────────────────────────────────────────────
			select {
			case fileCh <- domain.FileEntry{
				Path:     rel,
				AbsPath:  path,
				Language: langName,
				Content:  content,
			}:
			case <-ctx.Done():
				return ctx.Err()
			}

			return nil
		})

		if walkErr != nil {
			select {
			case errCh <- walkErr:
			default:
			}
		}
	}()

	return fileCh, errCh
}

// ── .gitignore support ────────────────────────────────────────────────────────

// gitignorePatterns is a minimal .gitignore rule set.
// It handles the most common patterns used in real projects:
//   - Literal filenames (e.g. "Makefile")
//   - Wildcard globs (e.g. "*.log", "*.pyc")
//   - Directory-prefix globs (e.g. "dist/", "build/")
//   - Negation patterns are parsed but not applied (complexity vs. value).
type gitignorePatterns struct {
	patterns []string
}

// loadGitignore reads the top-level .gitignore in repoPath.
// If the file does not exist or cannot be read, an empty set is returned.
func loadGitignore(repoPath string) *gitignorePatterns {
	g := &gitignorePatterns{}
	f, err := os.Open(filepath.Join(repoPath, ".gitignore"))
	if err != nil {
		return g
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			// Skip blanks, comments, and negation rules (not supported).
			continue
		}
		g.patterns = append(g.patterns, line)
	}
	return g
}

// match returns true if the relative path rel should be ignored.
func (g *gitignorePatterns) match(rel string) bool {
	base := filepath.Base(rel)
	for _, pattern := range g.patterns {
		// Strip trailing slash — treat "dist/" and "dist" identically.
		p := strings.TrimSuffix(pattern, "/")

		// 1. Match against the full relative path.
		if matched, _ := filepath.Match(p, rel); matched {
			return true
		}
		// 2. Match against the base filename.
		if matched, _ := filepath.Match(p, base); matched {
			return true
		}
		// 3. Check if rel starts with the pattern as a directory prefix.
		if strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

// ── utility ───────────────────────────────────────────────────────────────────

// makeSet converts a slice into a boolean lookup map.
// An empty slice produces an empty map (interpreted as "no filter").
func makeSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}
