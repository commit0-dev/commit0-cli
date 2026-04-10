package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// LoadDotEnv searches for a .env file starting from the current working
// directory and walking up to the filesystem root. The first .env found is
// loaded. Each KEY=VALUE pair is set via os.Setenv only when the variable is
// not already present in the environment, so real environment variables always
// take precedence.
//
// The file is silently ignored when not found. Parse errors for individual
// lines are skipped (comment or blank lines are skipped without error).
func LoadDotEnv() {
	path, ok := findDotEnv()
	if !ok {
		return
	}
	_ = parseDotEnv(path)
}

// findDotEnv walks from cwd toward the filesystem root and returns the path
// of the first .env file found.
func findDotEnv() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		candidate := filepath.Join(dir, ".env")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}
	return "", false
}

// parseDotEnv reads path and sets env vars for KEY=VALUE lines.
// Returns the first I/O error encountered; individual malformed lines are skipped.
func parseDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip blank lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip optional inline "export " prefix.
		line = strings.TrimPrefix(line, "export ")

		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue // no '=' or key is empty
		}

		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		// Strip surrounding quotes (" or ').
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}

		// Only set if not already in the environment.
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
	return scanner.Err()
}
