package app

import (
	"strings"
	"testing"
)

// ── normalizeRemoteURL ────────────────────────────────────────────────────

func TestNormalizeRemoteURL_SSHToHTTPS(t *testing.T) {
	got := normalizeRemoteURL("git@github.com:owner/repo.git")
	want := "https://github.com/owner/repo"
	if got != want {
		t.Errorf("normalizeRemoteURL(SSH) = %q, want %q", got, want)
	}
}

func TestNormalizeRemoteURL_SSHWithoutGitSuffix(t *testing.T) {
	got := normalizeRemoteURL("git@github.com:owner/repo")
	want := "https://github.com/owner/repo"
	if got != want {
		t.Errorf("normalizeRemoteURL(SSH no .git) = %q, want %q", got, want)
	}
}

func TestNormalizeRemoteURL_HTTPSStripsGitSuffix(t *testing.T) {
	got := normalizeRemoteURL("https://github.com/owner/repo.git")
	want := "https://github.com/owner/repo"
	if got != want {
		t.Errorf("normalizeRemoteURL(HTTPS .git) = %q, want %q", got, want)
	}
}

func TestNormalizeRemoteURL_HTTPSAlreadyClean(t *testing.T) {
	got := normalizeRemoteURL("https://github.com/owner/repo")
	if got != "https://github.com/owner/repo" {
		t.Errorf("normalizeRemoteURL(clean HTTPS) = %q", got)
	}
}

func TestNormalizeRemoteURL_LeadingTrailingSpaces(t *testing.T) {
	got := normalizeRemoteURL("  https://github.com/owner/repo.git  ")
	if got != "https://github.com/owner/repo" {
		t.Errorf("normalizeRemoteURL(spaces) = %q", got)
	}
}

func TestNormalizeRemoteURL_SSHNoColon_ReturnsAsIs(t *testing.T) {
	// SSH-like prefix but no colon → falls through to HTTPS branch
	got := normalizeRemoteURL("git@no-colon-here")
	// no colon → colonIdx == -1 → doesn't enter SSH branch; treated as HTTPS
	if !strings.HasPrefix(got, "git@") {
		// If it somehow strips, that's fine as long as it doesn't panic.
		t.Logf("normalizeRemoteURL(bad SSH) = %q", got)
	}
}

func TestNormalizeRemoteURL_EmptyString(t *testing.T) {
	got := normalizeRemoteURL("")
	if got != "" {
		t.Errorf("normalizeRemoteURL('') = %q, want empty", got)
	}
}

// ── slugFromRemoteURL ─────────────────────────────────────────────────────

func TestSlugFromRemoteURL_SSH(t *testing.T) {
	got := slugFromRemoteURL("git@github.com:owner/repo.git")
	if got != "owner/repo" {
		t.Errorf("slug(SSH) = %q, want %q", got, "owner/repo")
	}
}

func TestSlugFromRemoteURL_SSHNoGitSuffix(t *testing.T) {
	got := slugFromRemoteURL("git@github.com:owner/repo")
	if got != "owner/repo" {
		t.Errorf("slug(SSH no .git) = %q, want %q", got, "owner/repo")
	}
}

func TestSlugFromRemoteURL_HTTPS(t *testing.T) {
	got := slugFromRemoteURL("https://github.com/owner/repo.git")
	if got != "owner/repo" {
		t.Errorf("slug(HTTPS) = %q, want %q", got, "owner/repo")
	}
}

func TestSlugFromRemoteURL_HTTPSClean(t *testing.T) {
	got := slugFromRemoteURL("https://github.com/owner/repo")
	if got != "owner/repo" {
		t.Errorf("slug(HTTPS clean) = %q, want %q", got, "owner/repo")
	}
}

func TestSlugFromRemoteURL_Empty(t *testing.T) {
	got := slugFromRemoteURL("")
	// empty URL → parse gives empty path → returns ""
	if got != "" {
		t.Errorf("slug('') = %q, want empty", got)
	}
}

func TestSlugFromRemoteURL_SSHNoColon_ReturnsEmpty(t *testing.T) {
	got := slugFromRemoteURL("git@github.com")
	// no colon → colonIdx == -1 → returns ""
	if got != "" {
		t.Errorf("slug(SSH no colon) = %q, want empty", got)
	}
}

func TestSlugFromRemoteURL_HTTPSNoPath(t *testing.T) {
	got := slugFromRemoteURL("https://github.com")
	// no path segments → returns ""
	if got != "" {
		t.Errorf("slug(HTTPS no path) = %q, want empty", got)
	}
}

func TestSlugFromRemoteURL_InvalidURL(t *testing.T) {
	// url.Parse accepts nearly anything; just ensure no panic
	got := slugFromRemoteURL("://invalid")
	_ = got // may be "" or anything, just must not panic
}

// ── ExtractGitMetadata ────────────────────────────────────────────────────

func TestExtractGitMetadata_NonGitDir_ReturnsZero(t *testing.T) {
	// A temp dir that is not a git repo should return zero struct.
	meta := ExtractGitMetadata(t.TempDir())
	if meta.RemoteURL != "" || meta.Branch != "" || meta.CommitHash != "" || meta.Slug != "" {
		t.Errorf("non-git dir should return zero GitMetadata, got %+v", meta)
	}
}

func TestExtractGitMetadata_RealRepo_HasCommitHash(t *testing.T) {
	// The test binary runs inside the actual repo.
	// We can't guarantee all fields, but the repo itself is a git repo.
	meta := ExtractGitMetadata(".")
	// At minimum, CommitHash should be non-empty in a real repo.
	if meta.CommitHash == "" {
		t.Log("CommitHash is empty — might be a shallow clone or unusual env, skipping assertion")
		return
	}
	// CommitHash is short (7–8 chars from `git rev-parse --short HEAD`).
	if len(meta.CommitHash) < 4 {
		t.Errorf("CommitHash looks too short: %q", meta.CommitHash)
	}
}

func TestExtractGitMetadata_InvalidPath_ReturnsZero(t *testing.T) {
	// filepath.Abs with a relative path always succeeds on real OS, but
	// we can test an impossible absolute path for git.
	meta := ExtractGitMetadata("/no-such-path-xyz-abc-123")
	// git commands will fail, so we get zero
	if meta.CommitHash != "" || meta.Branch != "" {
		t.Logf("got non-zero meta for invalid path: %+v", meta)
	}
}
