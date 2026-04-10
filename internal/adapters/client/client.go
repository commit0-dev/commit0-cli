// Package client provides a thin HTTP client for the commit0 server API.
// CLI commands use this instead of constructing services in-process,
// enabling client-server separation (Streamable HTTP architecture).
package client

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"resty.dev/v3"
)

// Client is an HTTP client for the commit0 server API.
type Client struct {
	rc  *resty.Client
	log *slog.Logger
}

// New creates a commit0 HTTP client pointing at the given server URL.
func New(baseURL string) *Client {
	rc := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(120 * time.Second).
		SetRetryCount(3).
		SetRetryWaitTime(1 * time.Second).
		SetRetryMaxWaitTime(10 * time.Second).
		SetAllowNonIdempotentRetry(true)

	return &Client{
		rc:  rc,
		log: slog.Default().With("adapter", "client"),
	}
}

// Ping checks if the server is reachable and healthy.
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.rc.R().
		SetContext(ctx).
		Get("/health")
	if err != nil {
		return fmt.Errorf("cannot connect to commit0 server: %w", err)
	}
	if resp.IsError() {
		return fmt.Errorf("server health check failed: %d", resp.StatusCode())
	}
	return nil
}

// BaseURL returns the configured server URL (for error messages).
func (c *Client) BaseURL() string {
	return c.rc.BaseURL()
}
