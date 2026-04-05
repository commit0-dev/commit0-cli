package walker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/commit0-dev/commit0/internal/domain"
)

// collectFiles drains both channels and returns the file entries received.
// It must be called after Walk() to avoid blocking the walk goroutine.
func collectFiles(fileCh <-chan domain.FileEntry, errCh <-chan error) ([]domain.FileEntry, error) {
	var files []domain.FileEntry
	var walkErr error

	for fileCh != nil || errCh != nil {
		select {
		case f, ok := <-fileCh:
			if !ok {
				fileCh = nil
				continue
			}
			files = append(files, f)
		case e, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if e != nil {
				walkErr = e
			}
		}
	}
	return files, walkErr
}

// ── NewFSWalker ───────────────────────────────────────────────────────────────

func TestNewFSWalker_NilLogger(t *testing.T) {
	w := NewFSWalker(nil)
	if w == nil {
		t.Fatal("expected non-nil FSWalker")
	}
	if w.log == nil {
		t.Fatal("expected log to fall back to slog.Default(), got nil")
	}
}

// ── Walk – happy path ─────────────────────────────────────────────────────────

func TestWalk_HappyPath(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-Go file should be ignored.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# readme"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewFSWalker(nil)
	fileCh, errCh := w.Walk(context.Background(), dir, domain.WalkOpts{})
	files, err := collectFiles(fileCh, errCh)
	if err != nil {
		t.Fatalf("unexpected walk error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	for _, f := range files {
		if f.Language != "go" {
			t.Errorf("expected language 'go', got %q for %s", f.Language, f.Path)
		}
		if f.AbsPath == "" {
			t.Errorf("AbsPath should be set for %s", f.Path)
		}
		if f.Content == nil {
			t.Errorf("Content should be set for %s", f.Path)
		}
	}
}

// ── Walk – skips known directories ───────────────────────────────────────────

func TestWalk_SkipsKnownDirectories(t *testing.T) {
	dir := t.TempDir()

	skippedDirs := []string{
		".git", "node_modules", "vendor", "__pycache__",
		"dist", "build", ".tox", ".mypy_cache", ".svn", ".hg",
	}

	// Create a Go file in each skipped directory; none should appear.
	for _, d := range skippedDirs {
		sub := filepath.Join(dir, d)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "file.go"), []byte("package x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// One file in root that should be found.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewFSWalker(nil)
	fileCh, errCh := w.Walk(context.Background(), dir, domain.WalkOpts{})
	files, err := collectFiles(fileCh, errCh)
	if err != nil {
		t.Fatalf("unexpected walk error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file (root main.go), got %d: %v", len(files), files)
	}
}

// ── Walk – language filter ────────────────────────────────────────────────────

func TestWalk_LanguageFilter(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.py"), []byte("print('hi')"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte("export {}"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewFSWalker(nil)
	fileCh, errCh := w.Walk(context.Background(), dir, domain.WalkOpts{
		Languages: []string{"go"},
	})
	files, err := collectFiles(fileCh, errCh)
	if err != nil {
		t.Fatalf("unexpected walk error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 go file, got %d", len(files))
	}
	if files[0].Language != "go" {
		t.Errorf("expected 'go', got %q", files[0].Language)
	}
}

// ── Walk – MaxFileKB ──────────────────────────────────────────────────────────

func TestWalk_MaxFileKB(t *testing.T) {
	dir := t.TempDir()

	// Small file: should be included.
	small := make([]byte, 512) // 0.5 KB
	if err := os.WriteFile(filepath.Join(dir, "small.go"), small, 0o644); err != nil {
		t.Fatal(err)
	}

	// Large file: 5 KB — exceeds limit of 2 KB.
	large := make([]byte, 5*1024)
	if err := os.WriteFile(filepath.Join(dir, "large.go"), large, 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewFSWalker(nil)
	fileCh, errCh := w.Walk(context.Background(), dir, domain.WalkOpts{MaxFileKB: 2})
	files, err := collectFiles(fileCh, errCh)
	if err != nil {
		t.Fatalf("unexpected walk error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file (small.go), got %d", len(files))
	}
	if files[0].Path != "small.go" {
		t.Errorf("expected 'small.go', got %q", files[0].Path)
	}
}

// ── Walk – Exclude globs ──────────────────────────────────────────────────────

func TestWalk_ExcludeGlobs(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewFSWalker(nil)
	fileCh, errCh := w.Walk(context.Background(), dir, domain.WalkOpts{
		Exclude: []string{"*_test.go"},
	})
	files, err := collectFiles(fileCh, errCh)
	if err != nil {
		t.Fatalf("unexpected walk error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Path != "main.go" {
		t.Errorf("expected 'main.go', got %q", files[0].Path)
	}
}

func TestWalk_ExcludeGlobsRelPath(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "root.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "pkg.go"), []byte("package pkg"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewFSWalker(nil)
	fileCh, errCh := w.Walk(context.Background(), dir, domain.WalkOpts{
		Exclude: []string{"pkg/pkg.go"},
	})
	files, err := collectFiles(fileCh, errCh)
	if err != nil {
		t.Fatalf("unexpected walk error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(files), files)
	}
	if files[0].Path != "root.go" {
		t.Errorf("expected 'root.go', got %q", files[0].Path)
	}
}

// ── Walk – .gitignore ─────────────────────────────────────────────────────────

func TestWalk_GitignorePatterns(t *testing.T) {
	dir := t.TempDir()

	gitignore := "*.pyc\nbuild/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		t.Fatal(err)
	}

	// Should be included.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Should be excluded via gitignore (*.pyc matches but extension not in language map;
	// still tests the filter path. Use a .go that matches a gitignore glob instead.)
	if err := os.WriteFile(filepath.Join(dir, "ignored.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Update gitignore to ignore ignored.go directly.
	gitignore = "ignored.go\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewFSWalker(nil)
	fileCh, errCh := w.Walk(context.Background(), dir, domain.WalkOpts{})
	files, err := collectFiles(fileCh, errCh)
	if err != nil {
		t.Fatalf("unexpected walk error: %v", err)
	}

	for _, f := range files {
		if f.Path == "ignored.go" {
			t.Errorf("ignored.go should have been filtered by .gitignore")
		}
	}
}

// ── Walk – context cancellation ───────────────────────────────────────────────

func TestWalk_ContextCancellation(t *testing.T) {
	dir := t.TempDir()

	// Create enough files to give the goroutine a chance to see the canceled context.
	for i := range 10 {
		name := filepath.Join(dir, "file_"+string(rune('a'+i))+".go")
		if err := os.WriteFile(name, []byte("package main"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	w := NewFSWalker(nil)
	fileCh, errCh := w.Walk(ctx, dir, domain.WalkOpts{})
	// Must drain both channels; goroutine must not deadlock.
	_, _ = collectFiles(fileCh, errCh)
	// Test passes as long as it does not hang.
}

// TestWalk_ContextCancelDuringEmit triggers the ctx.Done() case inside the
// emit select by filling the 64-slot fileCh buffer and then canceling the
// context so the goroutine's next send picks the Done branch instead.
func TestWalk_ContextCancelDuringEmit(t *testing.T) {
	dir := t.TempDir()
	// 65 files > 64-slot fileCh buffer; goroutine blocks trying to emit #65.
	for i := 0; i < 65; i++ {
		name := filepath.Join(dir, fmt.Sprintf("f%02d.go", i))
		if err := os.WriteFile(name, []byte("package main"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := NewFSWalker(nil)
	fileCh, errCh := w.Walk(ctx, dir, domain.WalkOpts{})

	// Give the goroutine time to fill the 64-slot buffer and block on the 65th.
	time.Sleep(20 * time.Millisecond)
	cancel() // triggers ctx.Done() in the emit select for the 65th file

	// Drain so the goroutine can exit cleanly.
	for range fileCh {
	}
	<-errCh
}

// ── Walk – non-existent path ──────────────────────────────────────────────────

// TestWalk_NonExistentPath verifies that the walker doesn't panic or hang on a
// missing root path. The entry-level OS error is logged and swallowed (the
// callback returns nil), so errCh receives nil — this is intentional behavior.
func TestWalk_NonExistentPath(t *testing.T) {
	w := NewFSWalker(nil)
	fileCh, errCh := w.Walk(context.Background(), "/no/such/path/xyz123", domain.WalkOpts{})
	files, err := collectFiles(fileCh, errCh)
	if err != nil {
		t.Errorf("expected nil error for non-existent path (error is swallowed), got: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files from non-existent path, got %d", len(files))
	}
}

// ── loadGitignore ─────────────────────────────────────────────────────────────

func TestLoadGitignore_NoFile(t *testing.T) {
	dir := t.TempDir()
	g := loadGitignore(dir)
	if g == nil {
		t.Fatal("expected non-nil gitignorePatterns")
	}
	if len(g.patterns) != 0 {
		t.Errorf("expected 0 patterns, got %d", len(g.patterns))
	}
}

func TestLoadGitignore_WithPatterns(t *testing.T) {
	dir := t.TempDir()

	content := `# this is a comment

*.log
!important.log
dist/
build
`
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	g := loadGitignore(dir)
	// Expected: "*.log", "dist/", "build" — blank lines, comments, and negation skipped.
	expected := []string{"*.log", "dist/", "build"}
	if len(g.patterns) != len(expected) {
		t.Fatalf("expected %d patterns, got %d: %v", len(expected), len(g.patterns), g.patterns)
	}
	for i, p := range expected {
		if g.patterns[i] != p {
			t.Errorf("pattern[%d]: expected %q, got %q", i, p, g.patterns[i])
		}
	}
}

// ── gitignorePatterns.match ───────────────────────────────────────────────────

func TestGitignorePatterns_Match(t *testing.T) {
	g := &gitignorePatterns{
		patterns: []string{"*.log", "dist/", "secret.go"},
	}

	tests := []struct {
		rel     string
		desc    string
		wantHit bool
	}{
		// Glob match via base name.
		{"app.log", "glob *.log matches base", true},
		// Literal match against base name.
		{"secret.go", "literal match base", true},
		// Directory prefix match (trailing slash stripped, prefix check).
		{"dist/bundle.js", "directory prefix match", true},
		// Full rel path matches literal.
		{"pkg/secret.go", "glob match against base in subdir", true},
		// No match.
		{"main.go", "no match", false},
		{"src/main.go", "no match in subdir", false},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := g.match(tt.rel)
			if got != tt.wantHit {
				t.Errorf("match(%q) = %v, want %v", tt.rel, got, tt.wantHit)
			}
		})
	}
}

func TestGitignorePatterns_NoPatterns(t *testing.T) {
	g := &gitignorePatterns{}
	if g.match("anything.go") {
		t.Error("empty patterns should never match")
	}
}

func TestGitignorePatterns_FullRelPathMatch(t *testing.T) {
	g := &gitignorePatterns{patterns: []string{"pkg/helper.go"}}
	if !g.match("pkg/helper.go") {
		t.Error("expected full relative path to match")
	}
	if g.match("other/helper.go") {
		t.Error("different directory should not match")
	}
}

// ── makeSet ───────────────────────────────────────────────────────────────────

func TestMakeSet_Empty(t *testing.T) {
	s := makeSet(nil)
	if len(s) != 0 {
		t.Errorf("expected empty map, got %v", s)
	}
	s2 := makeSet([]string{})
	if len(s2) != 0 {
		t.Errorf("expected empty map for empty slice, got %v", s2)
	}
}

func TestMakeSet_NonEmpty(t *testing.T) {
	s := makeSet([]string{"go", "python", "go"})
	if !s["go"] {
		t.Error("expected 'go' in set")
	}
	if !s["python"] {
		t.Error("expected 'python' in set")
	}
	if s["typescript"] {
		t.Error("'typescript' should not be in set")
	}
	// Duplicate entries should not cause issues; map size should be 2.
	if len(s) != 2 {
		t.Errorf("expected 2 unique entries, got %d", len(s))
	}
}

// ── Walk – all supported languages emitted ────────────────────────────────────

func TestWalk_AllLanguages(t *testing.T) {
	dir := t.TempDir()

	files := map[string]string{
		"app.go":  "go",
		"app.py":  "python",
		"app.ts":  "typescript",
		"app.tsx": "typescript",
		"app.js":  "javascript",
		"app.jsx": "javascript",
		"app.rb":  "", // not supported
		"app.c":   "", // not supported
	}

	for name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("// content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	w := NewFSWalker(nil)
	fileCh, errCh := w.Walk(context.Background(), dir, domain.WalkOpts{})
	got, err := collectFiles(fileCh, errCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only 6 supported-extension files should come through.
	if len(got) != 6 {
		t.Fatalf("expected 6 files, got %d: %v", len(got), got)
	}
	for _, f := range got {
		expected := files[f.Path]
		if expected == "" {
			t.Errorf("unexpected file %q in results", f.Path)
		}
		if f.Language != expected {
			t.Errorf("file %q: expected language %q, got %q", f.Path, expected, f.Language)
		}
	}
}

// ── Walk – relative paths use forward slashes ─────────────────────────────────

func TestWalk_RelativePathForwardSlashes(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg", "util")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "helper.go"), []byte("package util"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewFSWalker(nil)
	fileCh, errCh := w.Walk(context.Background(), dir, domain.WalkOpts{})
	files, err := collectFiles(fileCh, errCh)
	if err != nil {
		t.Fatalf("unexpected walk error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	for _, ch := range files[0].Path {
		if ch == '\\' {
			t.Errorf("path %q contains backslash; should use forward slashes", files[0].Path)
		}
	}
}
