package retry

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/commit0-dev/commit0/internal/domain"
)

// WithRetry executes a function with exponential backoff retry logic
func WithRetry(ctx context.Context, maxAttempts int, fn func() error) error {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Check context before attempting
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("context cancelled: %w", err)
		}

		// Execute function
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !isRetryable(err) {
			return err
		}

		// Don't sleep after last failed attempt
		if attempt == maxAttempts-1 {
			break
		}

		// Calculate backoff: base*(2^attempt) + jitter
		baseSleep := time.Duration(1<<uint(attempt)) * 100 * time.Millisecond
		jitter := time.Duration(rand.Intn(50)) * time.Millisecond
		totalSleep := baseSleep + jitter

		// Sleep or exit on context cancellation
		select {
		case <-time.After(totalSleep):
			// Continue to next attempt
		case <-ctx.Done():
			return fmt.Errorf("context cancelled during retry: %w", ctx.Err())
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", maxAttempts, lastErr)
}

// isRetryable checks if an error is retryable
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Check if it's a DomainError with retryable error code
	var domainErr *domain.DomainError
	if errors.As(err, &domainErr) {
		return domainErr.Code == domain.ErrRateLimit || domainErr.Code == domain.ErrTimeout
	}

	return false
}
