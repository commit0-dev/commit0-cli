package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"log/slog"

	gitadapter "github.com/commit0-dev/commit0/server/internal/adapters/git"
)

// setupGitRepo initializes a temporary git repo, creates an initial commit
// with a file, then returns the directory path.
func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-b", "main")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")

	// Create initial file and commit.
	writeFile(t, filepath.Join(dir, "hello.go"), `package hello

func Hello() string {
	return "hello"
}
`)
	run("add", "hello.go")
	run("commit", "-m", "initial commit")

	return dir
}

// writeFile writes content to path, creating parent dirs as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newWalker() *gitadapter.Walker {
	return gitadapter.NewWalker(slog.Default())
}

// ---------------------------------------------------------------------------
// DiffWorkingTree tests
// ---------------------------------------------------------------------------

func TestDiffWorkingTree_NoChanges_ReturnsEmpty(t *testing.T) {
	dir := setupGitRepo(t)
	w := newWalker()

	diffs, err := w.DiffWorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("expected 0 diffs on clean repo, got %d", len(diffs))
	}
}

func TestDiffWorkingTree_ModifiedFile_PopulatesFields(t *testing.T) {
	dir := setupGitRepo(t)

	// Modify the file.
	writeFile(t, filepath.Join(dir, "hello.go"), `package hello

func Hello() string {
	return "hello world"
}

func Goodbye() string {
	return "goodbye"
}
`)

	w := newWalker()
	diffs, err := w.DiffWorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}

	d := diffs[0]
	if d.Path != "hello.go" {
		t.Errorf("expected path 'hello.go', got %q", d.Path)
	}
	if d.Status != "modified" {
		t.Errorf("expected status 'modified', got %q", d.Status)
	}
	if d.Patch == "" {
		t.Error("expected non-empty patch")
	}
	if !strings.Contains(d.Patch, "@@") {
		t.Error("expected patch to contain hunk header '@@'")
	}
}

func TestDiffWorkingTree_AddedFile_StatusIsAdded(t *testing.T) {
	dir := setupGitRepo(t)

	// Add a new file and stage it.
	writeFile(t, filepath.Join(dir, "newfile.go"), `package hello

func NewFunc() {}
`)

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("git %v: %v\n%s", args, err, out)
		}
	}
	run("add", "newfile.go")

	w := newWalker()
	diffs, err := w.DiffWorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d: %+v", len(diffs), diffs)
	}
	if diffs[0].Status != "added" {
		t.Errorf("expected status 'added', got %q", diffs[0].Status)
	}
}

// ---------------------------------------------------------------------------
// DiffRange tests
// ---------------------------------------------------------------------------

func TestDiffRange_ModifiedFile_ReturnsHunkHeadersInPatch(t *testing.T) {
	dir := setupGitRepo(t)

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Modify file and create a second commit.
	writeFile(t, filepath.Join(dir, "hello.go"), `package hello

func Hello() string {
	return "hello world"
}
`)
	run("add", "hello.go")
	run("commit", "-m", "second commit")

	w := newWalker()
	diffs, err := w.DiffRange(context.Background(), dir, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}

	d := diffs[0]
	if d.Path != "hello.go" {
		t.Errorf("expected path 'hello.go', got %q", d.Path)
	}
	if d.Patch == "" {
		t.Error("expected non-empty patch")
	}
	if !strings.Contains(d.Patch, "@@") {
		t.Errorf("expected hunk header '@@' in patch, got:\n%s", d.Patch)
	}
}

func TestDiffRange_WORKINGToRef_DelegatesToDiffWorkingTree(t *testing.T) {
	dir := setupGitRepo(t)

	// Modify the file without committing.
	writeFile(t, filepath.Join(dir, "hello.go"), `package hello

func Hello() string {
	return "changed"
}
`)

	w := newWalker()
	// toRef="WORKING" should delegate to DiffWorkingTree.
	diffs, err := w.DiffRange(context.Background(), dir, "HEAD", "WORKING")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff via WORKING delegation, got %d", len(diffs))
	}
	if diffs[0].Path != "hello.go" {
		t.Errorf("expected 'hello.go', got %q", diffs[0].Path)
	}
}

func TestDiffRange_ParsesHunkHeaders(t *testing.T) {
	dir := setupGitRepo(t)

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Make a change on line 4 specifically.
	writeFile(t, filepath.Join(dir, "hello.go"), `package hello

func Hello() string {
	return "modified line"
}
`)
	run("add", "hello.go")
	run("commit", "-m", "modify line 4")

	w := newWalker()
	diffs, err := w.DiffRange(context.Background(), dir, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(diffs) == 0 {
		t.Fatal("expected at least one diff")
	}

	// Verify the patch contains a valid hunk header with new-file line number.
	patch := diffs[0].Patch
	if !strings.Contains(patch, "+") || !strings.Contains(patch, "@@") {
		t.Errorf("patch does not look like a unified diff:\n%s", patch)
	}
}

func TestDiffWorkingTree_HandlesDeletedFile(t *testing.T) {
	dir := setupGitRepo(t)

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Stage a deletion.
	run("rm", "hello.go")

	w := newWalker()
	diffs, err := w.DiffWorkingTree(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for deleted file, got %d", len(diffs))
	}
	if diffs[0].Status != "deleted" {
		t.Errorf("expected status 'deleted', got %q", diffs[0].Status)
	}
}
