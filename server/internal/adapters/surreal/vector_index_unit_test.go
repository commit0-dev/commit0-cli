package surreal

import (
	"context"
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

// Note: Integration tests for GetNodeEmbedding that require a live SurrealDB
// connection should be added to integration_test.go with the -tags=integration build tag.
// Unit tests here are limited to validation logic since we can't easily mock
// surrealdb.Query at the package level.
