package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/commit0-dev/commit0/internal/domain"
)

func TestWithRetrySuccess(t *testing.T) {
	attempts := 0
	err := WithRetry(context.Background(), 3, func() error {
		attempts++
		return nil
	})

	if err != nil {
		t.Errorf("WithRetry failed: %v", err)
	}

	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (success on first try)", attempts)
	}
}

func TestWithRetryAfterRetries(t *testing.T) {
	attempts := 0
	err := WithRetry(context.Background(), 3, func() error {
		attempts++
		if attempts < 2 {
			return domain.RateLimit("too fast")
		}
		return nil
	})

	if err != nil {
		t.Errorf("WithRetry failed: %v", err)
	}

	if attempts != 2 {
		t.Errorf("attempts = %d, want 2 (success on 2nd try)", attempts)
	}
}

func TestWithRetryNonRetryable(t *testing.T) {
	attempts := 0
	err := WithRetry(context.Background(), 3, func() error {
		attempts++
		if attempts == 1 {
			return domain.NotFound("not found")
		}
		return nil
	})

	if err == nil {
		t.Errorf("WithRetry should return non-retryable error")
	}

	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on non-retryable error)", attempts)
	}
}

func TestWithRetryMaxAttemptsExceeded(t *testing.T) {
	attempts := 0
	err := WithRetry(context.Background(), 3, func() error {
		attempts++
		return domain.RateLimit("too fast")
	})

	if err == nil {
		t.Errorf("WithRetry should fail after max attempts")
	}

	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestWithRetryContextCancelled(t *testing.T) {
	attempts := 0
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	err := WithRetry(ctx, 3, func() error {
		attempts++
		return nil
	})

	if err == nil {
		t.Errorf("WithRetry should fail with canceled context")
	}

	if attempts != 0 {
		t.Errorf("attempts = %d, want 0 (context canceled before execution)", attempts)
	}
}

func TestWithRetryContextCancelledDuringRetry(t *testing.T) {
	attempts := 0
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := WithRetry(ctx, 100, func() error {
		attempts++
		if attempts == 1 {
			// First attempt fails with retryable error
			return domain.RateLimit("too fast")
		}
		// This shouldn't be reached due to timeout
		return nil
	})

	if err == nil {
		t.Errorf("WithRetry should fail with timeout context")
	}

	// Should have attempted at least once
	if attempts == 0 {
		t.Errorf("attempts = %d, want >= 1", attempts)
	}
}

func TestWithRetryZeroAttempts(t *testing.T) {
	attempts := 0
	err := WithRetry(context.Background(), 0, func() error {
		attempts++
		return nil
	})

	if err != nil {
		t.Errorf("WithRetry with 0 attempts should treat as 1 attempt, got: %v", err)
	}

	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}

func TestIsRetryableRateLimit(t *testing.T) {
	err := domain.RateLimit("too fast")
	if !isRetryable(err) {
		t.Errorf("RateLimit error should be retryable")
	}
}

func TestIsRetryableTimeout(t *testing.T) {
	err := domain.Timeout("timeout", nil)
	if !isRetryable(err) {
		t.Errorf("Timeout error should be retryable")
	}
}

func TestIsRetryableNotFound(t *testing.T) {
	err := domain.NotFound("not found")
	if isRetryable(err) {
		t.Errorf("NotFound error should not be retryable")
	}
}

func TestIsRetryableNil(t *testing.T) {
	if isRetryable(nil) {
		t.Errorf("nil error should not be retryable")
	}
}

func TestIsRetryablePlainError(t *testing.T) {
	err := errors.New("some error")
	if isRetryable(err) {
		t.Errorf("plain error should not be retryable")
	}
}
