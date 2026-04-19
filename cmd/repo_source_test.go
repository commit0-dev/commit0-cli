package cmd

import (
	"testing"
)

func TestSlugFromURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "https with .git",
			input: "https://github.com/commit0-dev/commit0.git",
			want:  "commit0-dev/commit0",
		},
		{
			name:  "https without .git",
			input: "https://github.com/commit0-dev/commit0",
			want:  "commit0-dev/commit0",
		},
		{
			name:  "ssh with .git",
			input: "git@github.com:commit0-dev/commit0.git",
			want:  "commit0-dev/commit0",
		},
		{
			name:  "ssh without .git",
			input: "git@github.com:commit0-dev/commit0",
			want:  "commit0-dev/commit0",
		},
		{
			name:  "http (non-TLS)",
			input: "http://github.com/owner/repo.git",
			want:  "owner/repo",
		},
		{
			name:    "missing repo component",
			input:   "https://github.com/onlyowner",
			wantErr: true,
		},
		{
			name:    "invalid ssh format",
			input:   "git@github.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := slugFromURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got slug %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("slugFromURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsRemoteURL(t *testing.T) {
	remote := []string{
		"https://github.com/owner/repo.git",
		"http://github.com/owner/repo",
		"git@github.com:owner/repo.git",
	}
	local := []string{
		".",
		"/absolute/path",
		"relative/path",
		"../sibling",
	}
	for _, u := range remote {
		if !isRemoteURL(u) {
			t.Errorf("isRemoteURL(%q) = false, want true", u)
		}
	}
	for _, u := range local {
		if isRemoteURL(u) {
			t.Errorf("isRemoteURL(%q) = true, want false", u)
		}
	}
}
