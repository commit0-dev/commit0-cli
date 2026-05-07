package surreal

import (
	"context"
	"strings"
	"testing"
)

// TestGetNodeEmbedding_EmptyNodeID tests validation of empty node ID.
func TestGetNodeEmbedding_EmptyNodeID(t *testing.T) {
	adapter := &SurrealAdapter{}
	_, err := adapter.GetNodeEmbedding(context.Background(), "")
	if err == nil {
		t.Errorf("expected error for empty nodeID, got nil")
	}
}

// TestGetNodeEmbedding_MalformedNodeID guards against the regression fixed in
// this PR: passing a node ID without a colon-separated table prefix (e.g. just
// "foo") used to flow into models.NewRecordID with empty table and the whole
// string as the identifier, surfacing as a confusing "cannot marshal RecordID
// with empty table or ID" SDK error at query time. We now reject this at the
// adapter boundary with a clear domain validation error.
func TestGetNodeEmbedding_MalformedNodeID(t *testing.T) {
	adapter := &SurrealAdapter{}
	_, err := adapter.GetNodeEmbedding(context.Background(), "no-colon-here")
	if err == nil {
		t.Fatalf("expected error for malformed nodeID, got nil")
	}
	if !strings.Contains(err.Error(), "table:identifier") {
		t.Errorf("expected error to mention 'table:identifier' shape, got: %v", err)
	}
}

// Note: Integration tests for GetNodeEmbedding that require a live SurrealDB
// connection should be added to integration_test.go with the -tags=integration build tag.
// Unit tests here are limited to validation logic since we can't easily mock
// surrealdb.Query at the package level.
