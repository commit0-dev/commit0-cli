package mcp

import (
	"fmt"

	"github.com/commit0-dev/commit0/pkg/types"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolError converts a domain error into an MCP tool-level error result.
// Tool-level errors use isError:true in the result content so the LLM can
// read the message and self-correct. They are NOT JSON-RPC protocol errors.
//
// All domain.DomainError values map here. Unknown errors also land here
// to ensure the MCP channel is never broken by application logic failures.
func toolError(err error) *mcpsdk.CallToolResult {
	msg := errorMessage(err)
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: msg},
		},
		IsError: true,
	}
}

// errorMessage produces a user-readable error description, with extra context
// for common domain error codes.
func errorMessage(err error) string {
	if err == nil {
		return "unknown error"
	}

	var de types.DomainError
	if asDomainError(err, &de) {
		switch de.Code {
		case types.ErrNotFound:
			return fmt.Sprintf("not found: %s", de.Message)
		case types.ErrValidation:
			return fmt.Sprintf("invalid input: %s", de.Message)
		case types.ErrRateLimit:
			return fmt.Sprintf("rate limited — please retry in a moment: %s", de.Message)
		case types.ErrTimeout:
			return fmt.Sprintf("operation timed out: %s", de.Message)
		case types.ErrConflict:
			return fmt.Sprintf("conflict: %s", de.Message)
		case types.ErrAuthFailed:
			return fmt.Sprintf("authentication failed: %s", de.Message)
		default:
			return fmt.Sprintf("commit0 error [%s]: %s", de.Code, de.Message)
		}
	}
	return err.Error()
}

// asDomainError extracts a DomainError from err, similar to errors.As but
// for the pointer DomainError type.
func asDomainError(err error, out *types.DomainError) bool {
	if err == nil {
		return false
	}
	if de, ok := err.(*types.DomainError); ok {
		*out = *de
		return true
	}
	return false
}

// dbUnavailableError returns a tool error explaining that SurrealDB is required.
// Used when lazy adapter init fails at first tool call.
func dbUnavailableError(url string) *mcpsdk.CallToolResult {
	msg := fmt.Sprintf(
		"commit0 MCP requires SurrealDB at %s — run `docker compose up surreal` then retry.",
		url,
	)
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: msg},
		},
		IsError: true,
	}
}
