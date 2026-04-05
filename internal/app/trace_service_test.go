package app

import (
	"context"
	"testing"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/pkg/types"
)

func TestTraceServiceTraceSuccess(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["my-repo::pkg.Handler"] = &types.CodeNode{
		ID:        "f1",
		Qualified: "pkg.Handler",
		Kind:      types.NodeFunction,
	}
	store.traceHops = []types.TraceHop{
		{
			Depth: 0,
			Node:  types.CodeNode{ID: "f1", Qualified: "pkg.Handler"},
		},
	}

	cfg := &config.Config{}
	svc := NewTraceService(store, nil, nil, nil, cfg)

	result, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "pkg.Handler",
		RepoSlug:  "my-repo",
		Direction: "forward",
	})

	if err != nil {
		t.Fatalf("Trace failed: %v", err)
	}

	if result.Direction != "forward" {
		t.Errorf("Direction = %s, want forward", result.Direction)
	}
}

func TestTraceServiceTraceEmptySymbol(t *testing.T) {
	cfg := &config.Config{}
	svc := NewTraceService(nil, nil, nil, nil, cfg)

	_, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "",
		RepoSlug:  "my-repo",
		Direction: "forward",
	})

	if err == nil {
		t.Errorf("Trace should fail with empty symbol")
	}
}

func TestTraceServiceTraceInvalidDirection(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["my-repo::pkg.Func"] = &types.CodeNode{ID: "f1"}

	cfg := &config.Config{}
	svc := NewTraceService(store, nil, nil, nil, cfg)

	_, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "pkg.Func",
		RepoSlug:  "my-repo",
		Direction: "invalid",
	})

	if err == nil {
		t.Errorf("Trace should fail with invalid direction")
	}
}

func TestTraceServiceTraceNotFound(t *testing.T) {
	store := newStubGraphStore()
	cfg := &config.Config{}
	svc := NewTraceService(store, nil, nil, nil, cfg)

	_, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "nonexistent",
		RepoSlug:  "my-repo",
		Direction: "forward",
	})

	if err == nil {
		t.Errorf("Trace should fail for non-existent symbol")
	}
}
