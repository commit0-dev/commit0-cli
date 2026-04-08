package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatcherService watches a repository directory for file changes and triggers
// incremental re-indexing. Debounces rapid saves (500ms) and only processes
// files with supported extensions.
type WatcherService struct {
	indexSvc *IndexService
	repoSlug string
	repoPath string
	log      *slog.Logger

	mu       sync.Mutex
	pending  map[string]time.Time // path → last-modified time
	timer    *time.Timer
	debounce time.Duration
}

// NewWatcherService creates a background file watcher.
func NewWatcherService(indexSvc *IndexService, repoSlug, repoPath string) *WatcherService {
	return &WatcherService{
		indexSvc:  indexSvc,
		repoSlug:  repoSlug,
		repoPath:  repoPath,
		log:       slog.Default().With("service", "watcher", "repo", repoSlug),
		pending:   make(map[string]time.Time),
		debounce:  500 * time.Millisecond,
	}
}

// Watch starts watching the repository directory. Blocks until ctx is cancelled.
func (w *WatcherService) Watch(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	// Recursively add directories
	if err := w.addDirsRecursive(watcher, w.repoPath); err != nil {
		return err
	}

	w.log.Info("watching for changes", "path", w.repoPath)

	for {
		select {
		case <-ctx.Done():
			w.log.Info("watcher stopped")
			return ctx.Err()

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			if !w.isSupportedFile(event.Name) {
				continue
			}

			w.mu.Lock()
			w.pending[event.Name] = time.Now()
			if w.timer != nil {
				w.timer.Stop()
			}
			w.timer = time.AfterFunc(w.debounce, func() {
				w.flush(ctx)
			})
			w.mu.Unlock()

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			w.log.Warn("watcher error", "err", err)
		}
	}
}

// flush processes all pending file changes.
func (w *WatcherService) flush(ctx context.Context) {
	w.mu.Lock()
	files := make(map[string]time.Time, len(w.pending))
	for k, v := range w.pending {
		files[k] = v
	}
	w.pending = make(map[string]time.Time)
	w.mu.Unlock()

	if len(files) == 0 {
		return
	}

	w.log.Info("re-indexing changed files", "count", len(files))

	// Trigger incremental index — the index service's content hash check
	// will skip unchanged functions within each file.
	_, err := w.indexSvc.Index(ctx, IndexRequest{
		RepoSlug: w.repoSlug,
		RepoPath: w.repoPath,
	})
	if err != nil {
		w.log.Warn("incremental re-index failed", "err", err)
	} else {
		w.log.Info("incremental re-index complete", "files_changed", len(files))
	}
}

// addDirsRecursive walks the directory tree and adds each dir to the watcher.
func (w *WatcherService) addDirsRecursive(watcher *fsnotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible dirs
		}
		if !info.IsDir() {
			return nil
		}
		name := info.Name()
		// Skip hidden dirs and common noise
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "__pycache__" || name == "vendor" || name == "dist" || name == "build" {
			return filepath.SkipDir
		}
		return watcher.Add(path)
	})
}

// isSupportedFile returns true for source files we can index.
func (w *WatcherService) isSupportedFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".py", ".ts", ".tsx", ".js", ".jsx":
		return true
	}
	return false
}
