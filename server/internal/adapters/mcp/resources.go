package mcp

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/commit0-dev/commit0/pkg/types"
)

// nodeResourceURITemplate is the RFC 6570 template the SDK advertises to
// clients on resources/templates/list. Reads of any concrete URI matching
// this template are dispatched to the registered handler.
//
// The reserved-expansion form ({+id}) is used so node IDs containing slashes,
// colons or other reserved characters still match. Simple {id} would only
// match the [A-Za-z0-9._~-] set, which is too narrow for SurrealDB record
// identifiers.
const nodeResourceURITemplate = "node://{+id}"

// nodeResourceURIScheme is the scheme prefix every concrete read must carry.
// SDK templating dispatches by template match, but the handler still needs
// to extract the raw <id> from the request URI.
const nodeResourceURIScheme = "node://"

// registerNodeResource adds the node://<id> MCP resource to the server.
//
// The SDK's AddResourceTemplate validates the URI template against RFC 6570
// at registration. The handler then receives the concrete URI requested by
// the client (e.g. "node://abc-123") and parses the <id> suffix manually —
// the SDK does not pass parsed template variables through to the handler.
//
// On a graph miss the handler returns mcpsdk.ResourceNotFoundError so the
// client receives a properly typed JSON-RPC error rather than an opaque
// internal error.
func registerNodeResource(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	server.AddResourceTemplate(&mcpsdk.ResourceTemplate{
		URITemplate: nodeResourceURITemplate,
		Name:        "Code node",
		Title:       "commit0 code node",
		MIMEType:    "text/plain",
		Description: "Read the full body of one CodeNode by graph ID. " +
			"URI form: node://<id>. Use commit0_query, commit0_lookup or " +
			"commit0_list_files to obtain a node ID first.",
	}, func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		uri := ""
		if req != nil && req.Params != nil {
			uri = req.Params.URI
		}

		nodeID, err := parseNodeURI(uri)
		if err != nil {
			log.Debug("node resource: invalid URI", "uri", uri, "err", err)
			return nil, mcpsdk.ResourceNotFoundError(uri)
		}

		graph := deps.Graph
		if graph == nil {
			// No DB wired: surface as NotFound so the client gets a well-typed
			// error rather than crashing the channel. The MCP transport has
			// no equivalent of dbUnavailableError on the resource path.
			log.Debug("node resource: graph unavailable", "uri", uri)
			return nil, mcpsdk.ResourceNotFoundError(uri)
		}

		node, err := graph.GetNode(ctx, nodeID)
		if err != nil {
			var de *types.DomainError
			if errors.As(err, &de) && de.Code == types.ErrNotFound {
				return nil, mcpsdk.ResourceNotFoundError(uri)
			}
			log.Warn("node resource: GetNode failed", "node_id", nodeID, "err", err)
			return nil, mcpsdk.ResourceNotFoundError(uri)
		}
		if node == nil {
			return nil, mcpsdk.ResourceNotFoundError(uri)
		}

		return &mcpsdk.ReadResourceResult{
			Contents: []*mcpsdk.ResourceContents{{
				URI:      uri,
				MIMEType: "text/plain",
				Text:     node.Body,
			}},
		}, nil
	})
}

// parseNodeURI extracts the <id> suffix from a node:// URI. It accepts both
// the opaque form ("node://abc-123") and an authority-style form
// ("node://host/path") and unescapes percent-encoded characters in the path.
// Returns an error for any URI that does not begin with the node:// scheme
// or has an empty id.
func parseNodeURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, nodeResourceURIScheme) {
		return "", errors.New("uri must use node:// scheme")
	}

	// Trim the scheme prefix; the remainder is treated as the opaque ID.
	// We also try url.Parse first to honor percent-encoding; fall back to
	// the trimmed suffix when url.Parse fails (e.g. for IDs with characters
	// that would be ambiguous in the URL grammar).
	if parsed, err := url.Parse(uri); err == nil {
		// url.Parse for "node://abc" puts "abc" in Host; for "node://a/b" it
		// puts "a" in Host and "/b" in Path. Concatenate to recover the ID.
		id := parsed.Host
		if parsed.Path != "" {
			id += parsed.Path
		}
		if id != "" {
			if unescaped, escErr := url.PathUnescape(id); escErr == nil {
				id = unescaped
			}
			return id, nil
		}
	}

	id := strings.TrimPrefix(uri, nodeResourceURIScheme)
	if id == "" {
		return "", errors.New("empty node id")
	}
	return id, nil
}
