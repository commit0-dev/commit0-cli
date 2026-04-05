package main

import "github.com/commit0-dev/commit0/cmd"

// Injected at build time via -ldflags.
// ko and the GitHub Actions release workflow set these automatically.
var (
	version = "dev"
	commit  = "none"
)

func main() {
	cmd.Execute()
}
