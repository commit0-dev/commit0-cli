package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// resolveRepoSource resolves the repo path and canonical slug from the given
// argument, which is either a remote GitHub URL or a local filesystem path.
//
//   - GitHub URL  → clone/update to $TMPDIR/commit0-repos/<owner>/<repo>, slug = "owner/repo"
//   - Local path  → use as-is, derive slug from `git remote get-url origin`
func resolveRepoSource(ctx context.Context, arg string) (repoPath, repoSlug string, err error) {
	if isRemoteURL(arg) {
		repoSlug, err = slugFromURL(arg)
		if err != nil {
			return
		}
		repoPath, err = cloneOrUpdate(ctx, arg, repoSlug)
		return
	}

	// Local path: validate it exists, then derive slug from git remote.
	repoPath = arg
	repoSlug, err = slugFromLocalRepo(arg)
	return
}

// isRemoteURL returns true for http/https/git@ URLs.
func isRemoteURL(s string) bool {
	return strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "git@")
}

// slugFromURL parses "owner/repo" from a GitHub-style URL.
// Supports:
//
//	https://github.com/owner/repo
//	https://github.com/owner/repo.git
//	git@github.com:owner/repo.git
func slugFromURL(rawURL string) (string, error) {
	// SSH format: git@github.com:owner/repo.git
	if strings.HasPrefix(rawURL, "git@") {
		colonIdx := strings.IndexByte(rawURL, ':')
		if colonIdx < 0 {
			return "", fmt.Errorf("invalid git SSH URL: %s", rawURL)
		}
		path := strings.TrimSuffix(rawURL[colonIdx+1:], ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", fmt.Errorf("cannot parse owner/repo from: %s", rawURL)
		}
		return parts[0] + "/" + parts[1], nil
	}

	// HTTPS format
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse URL %q: %w", rawURL, err)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("cannot derive owner/repo from URL: %s", rawURL)
	}
	return parts[0] + "/" + strings.TrimSuffix(parts[1], ".git"), nil
}

// slugFromLocalRepo reads the git remote origin URL and derives the slug.
func slugFromLocalRepo(path string) (string, error) {
	out, err := exec.Command("git", "-C", path, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", fmt.Errorf(
			"cannot determine repo slug: no git remote origin found in %q\n"+
				"  Either pass a GitHub URL: commit0 index https://github.com/owner/repo.git\n"+
				"  Or add a remote:          git remote add origin https://github.com/owner/repo.git",
			path,
		)
	}
	return slugFromURL(strings.TrimSpace(string(out)))
}

// cloneOrUpdate clones the repo on first run or pulls latest on subsequent runs.
// Clones are persisted under $TMPDIR/commit0-repos/<owner>/<repo> so they are
// reused across invocations without polluting the working directory.
func cloneOrUpdate(ctx context.Context, remoteURL, slug string) (string, error) {
	dest := filepath.Join(os.TempDir(), "commit0-repos", filepath.FromSlash(slug))

	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		fmt.Printf("Updating existing clone at %s...\n", dest)
		pull := exec.CommandContext(ctx, "git", "-C", dest, "pull", "--ff-only")
		pull.Stdout = os.Stdout
		pull.Stderr = os.Stderr
		if err := pull.Run(); err != nil {
			// Non-fatal: stale clone is still indexable.
			fmt.Printf("Warning: git pull failed (%v), indexing existing clone.\n", err)
		}
		return dest, nil
	}

	fmt.Printf("Cloning %s...\n", remoteURL)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("create repos dir: %w", err)
	}
	clone := exec.CommandContext(ctx, "git", "clone", remoteURL, dest)
	clone.Stdout = os.Stdout
	clone.Stderr = os.Stderr
	if err := clone.Run(); err != nil {
		return "", fmt.Errorf("git clone: %w", err)
	}
	return dest, nil
}
