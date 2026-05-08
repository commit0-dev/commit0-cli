package mcp

import (
	"github.com/commit0-dev/commit0/pkg/types"
)

// indexStatusOut is the structured payload for commit0_index_status.
//
// We re-export types.IndexProgress under a stable adapter-layer name so the
// MCP wire schema is decoupled from any future field tweaks in pkg/types.
// The two are aliased today (no field rename) to keep the change diff small;
// switch to a converter when that ceases to hold.
type indexStatusOut = types.IndexProgress
