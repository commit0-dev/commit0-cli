// Package assets embeds static assets compiled into the binary.
package assets

import _ "embed"

// SchemaSurQL contains the SurrealDB 3.0 DDL applied on first run.
//
//go:embed schema.surql
var SchemaSurQL string
