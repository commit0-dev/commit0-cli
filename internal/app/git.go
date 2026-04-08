package app

import (
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitMetadata holds identity information extracted from a local git repository.
type GitMetadata struct {
	RemoteURL  string // normalized remote origin URL (e.g. "https://github.com/owner/repo")
	Branch     string // current branch name
	CommitHash string // HEAD commit hash (short)
	Slug       string // canonical slug derived from remote URL (e.g. "owner/repo")
}

// ExtractGitMetadata reads git identity from a local directory.
// Returns a zero struct if the path is not a git repo (non-fatal).
func ExtractGitMetadata(repoPath string) GitMetadata {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return GitMetadata{}
	}

	var meta GitMetadata

	// Remote URL
	if out, err := exec.Command("git", "-C", absPath, "remote", "get-url", "origin").Output(); err == nil {
		raw := strings.TrimSpace(string(out))
		meta.RemoteURL = normalizeRemoteURL(raw)
		meta.Slug = slugFromRemoteURL(raw)
	}

	// Current branch
	if out, err := exec.Command("git", "-C", absPath, "branch", "--show-current").Output(); err == nil {
		meta.Branch = strings.TrimSpace(string(out))
	}

	// HEAD commit hash
	if out, err := exec.Command("git", "-C", absPath, "rev-parse", "--short", "HEAD").Output(); err == nil {
		meta.CommitHash = strings.TrimSpace(string(out))
	}

	return meta
}

// normalizeRemoteURL converts SSH and HTTPS URLs to a canonical HTTPS form
// without the .git suffix. This enables deduplication across clone methods.
func normalizeRemoteURL(raw string) string {
	raw = strings.TrimSpace(raw)

	// SSH: git@github.com:owner/repo.git → https://github.com/owner/repo
	if strings.HasPrefix(raw, "git@") {
		colonIdx := strings.IndexByte(raw, ':')
		if colonIdx > 0 {
			host := raw[4:colonIdx] // "github.com"
			path := strings.TrimSuffix(raw[colonIdx+1:], ".git")
			return "https://" + host + "/" + path
		}
	}

	// HTTPS: strip .git suffix
	raw = strings.TrimSuffix(raw, ".git")
	return raw
}

// slugFromRemoteURL derives "owner/repo" from a git remote URL.
func slugFromRemoteURL(raw string) string {
	raw = strings.TrimSpace(raw)

	// SSH format: git@github.com:owner/repo.git
	if strings.HasPrefix(raw, "git@") {
		colonIdx := strings.IndexByte(raw, ':')
		if colonIdx > 0 {
			path := strings.TrimSuffix(raw[colonIdx+1:], ".git")
			parts := strings.SplitN(path, "/", 2)
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				return parts[0] + "/" + parts[1]
			}
		}
		return ""
	}

	// HTTPS format
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
		return parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")
	}
	return ""
}
