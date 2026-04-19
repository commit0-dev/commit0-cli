// Package sdk provides a thin HTTP client for the commit0 server API.
// CLI commands use this instead of constructing services in-process,
// enabling client-server separation (Streamable HTTP architecture).
package sdk

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"resty.dev/v3"
)

// Client is an HTTP client for the commit0 server API.
type Client struct {
	rc         *resty.Client
	log        *slog.Logger
	passphrase string // sync auth passphrase (empty = no auth)
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

// SetSyncPassphrase configures the passphrase for authenticating sync API calls.
func (c *Client) SetSyncPassphrase(passphrase string) {
	c.passphrase = passphrase
}

// syncRequest creates a Resty request with sync auth headers if passphrase is set.
func (c *Client) syncRequest(ctx context.Context) *resty.Request {
	r := c.rc.R().SetContext(ctx)
	if c.passphrase != "" {
		ts := fmt.Sprintf("%d", time.Now().Unix())
		mac := hmac.New(sha256.New, []byte(c.passphrase))
		mac.Write([]byte(ts))
		token := hex.EncodeToString(mac.Sum(nil))
		r.SetHeader("Authorization", "Bearer "+token)
		r.SetHeader("X-Sync-Timestamp", ts)
	}
	return r
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

// HealthCheck returns the server health status including state and active jobs.
func (c *Client) HealthCheck(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	resp, err := c.rc.R().
		SetContext(ctx).
		SetResult(&result).
		Get("/health")
	if err != nil {
		return nil, fmt.Errorf("health check: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("health check failed: %d", resp.StatusCode())
	}
	return result, nil
}

// BaseURL returns the configured server URL (for error messages).
func (c *Client) BaseURL() string {
	return c.rc.BaseURL()
}
