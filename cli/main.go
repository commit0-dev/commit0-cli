package main

import "github.com/commit0-dev/commit0/cli/cmd"

// Injected at build time via -ldflags.
var (
	version = "dev"
	commit  = "none"
)

func main() {
	cmd.SetVersion(version, commit)
	cmd.Execute()
}
