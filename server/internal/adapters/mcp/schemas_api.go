package mcp

import (
	"fmt"
	"strings"

	"github.com/commit0-dev/commit0/pkg/types"
)

// apiSurfaceFormatSummary returns the in-process types.APISurface as the
// structured payload. apiSurfaceFormatOpenAPI returns a small wrapper so MCP
// clients can introspect (format + endpoint count) without re-parsing the
// embedded OpenAPI JSON.
const (
	apiSurfaceFormatSummary = "summary"
	apiSurfaceFormatOpenAPI = "openapi"
)

// apiSurfaceIn is the typed input for commit0_api_surface.
type apiSurfaceIn struct {
	RepoSlug string `json:"repo_slug"        jsonschema:"Repository slug returned by commit0_list_repos. Required."`
	Format   string `json:"format,omitempty" jsonschema:"Output format. 'summary' (default) returns the typed APISurface; 'openapi' returns the OpenAPI 3.0 JSON spec as text content."`
}

// APISurfaceSummaryOut is the structured payload for format=summary. It is
// a re-export of types.APISurface under a stable adapter-layer name so the
// MCP wire shape can evolve independently of the application layer.
type APISurfaceSummaryOut = types.APISurface

// APISurfaceOpenAPIOut is the structured payload for format=openapi. The raw
// spec text rides in the TextContent of the CallToolResult; the structured
// payload exposes only the count + format so consumers can route on shape.
type APISurfaceOpenAPIOut struct {
	RepoSlug       string `json:"repo_slug"`
	Format         string `json:"format"` // always "openapi"
	EndpointsCount int    `json:"endpoints_count"`
	SpecBytes      int    `json:"spec_bytes"`
	TimingMS       int64  `json:"timing_ms"`
}

// validAPISurfaceFormat returns true if format is empty or one of the two
// canonical values. Empty defaults to summary downstream.
func validAPISurfaceFormat(format string) bool {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", apiSurfaceFormatSummary, apiSurfaceFormatOpenAPI:
		return true
	}
	return false
}

// apiSurfaceSummaryMarkdown renders an APISurfaceSummaryOut as a Markdown
// table grouped by HTTP method. Used as the TextContent for format=summary.
func apiSurfaceSummaryMarkdown(repoSlug string, surface *types.APISurface) string {
	var b strings.Builder
	endpoints := 0
	if surface != nil {
		endpoints = len(surface.Endpoints)
	}
	fmt.Fprintf(&b, "## API surface — `%s` (%d endpoint", repoSlug, endpoints)
	if endpoints != 1 {
		b.WriteString("s")
	}
	b.WriteString(")\n\n")
	if surface == nil || endpoints == 0 {
		b.WriteString("No HTTP route edges discovered for this repository.\n")
		return b.String()
	}

	// Bucket by HTTP method so the table groups GET/POST/PUT/etc together.
	groups := map[string][]types.APIEndpointDetail{}
	for _, detail := range surface.Endpoints {
		method := strings.ToUpper(strings.TrimSpace(detail.Endpoint.Method))
		if method == "" {
			method = "ANY"
		}
		groups[method] = append(groups[method], detail)
	}

	// Render in a stable order so the same surface produces the same Markdown.
	canonical := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "ANY"}
	rendered := map[string]bool{}
	render := func(method string, details []types.APIEndpointDetail) {
		if len(details) == 0 {
			return
		}
		fmt.Fprintf(&b, "### %s (%d)\n", method, len(details))
		for _, detail := range details {
			ep := detail.Endpoint
			fmt.Fprintf(&b, "- `%s` → `%s`", ep.Path, ep.Handler)
			if ep.FilePath != "" {
				fmt.Fprintf(&b, " — `%s:%d`", ep.FilePath, ep.Line)
			}
			b.WriteString("\n")
			if len(detail.AuthChain) > 0 {
				fmt.Fprintf(&b, "  _auth:_ %s\n", strings.Join(detail.AuthChain, " → "))
			}
			if detail.Binding.RequestType != "" {
				fmt.Fprintf(&b, "  _request:_ `%s`\n", detail.Binding.RequestType)
			}
			if len(detail.Binding.ResponseTypes) > 0 {
				fmt.Fprintf(&b, "  _responses:_ `%s`\n", strings.Join(detail.Binding.ResponseTypes, "`, `"))
			}
		}
		b.WriteString("\n")
	}
	for _, method := range canonical {
		render(method, groups[method])
		rendered[method] = true
	}
	// Render any non-canonical methods last (rare but possible: TRACE, CONNECT).
	for method, details := range groups {
		if rendered[method] {
			continue
		}
		render(method, details)
	}
	fmt.Fprintf(&b, "_elapsed: %dms_\n", surface.Timing.TotalMS)
	return b.String()
}

// apiSurfaceOpenAPIWrapper builds the structured payload for format=openapi.
// The raw OpenAPI bytes live in TextContent; this wrapper carries metadata
// so MCP clients can route on shape without parsing the JSON.
func apiSurfaceOpenAPIWrapper(repoSlug string, surface *types.APISurface, specBytes int) APISurfaceOpenAPIOut {
	out := APISurfaceOpenAPIOut{
		RepoSlug:  repoSlug,
		Format:    apiSurfaceFormatOpenAPI,
		SpecBytes: specBytes,
	}
	if surface != nil {
		out.EndpointsCount = len(surface.Endpoints)
		out.TimingMS = surface.Timing.TotalMS
	}
	return out
}
