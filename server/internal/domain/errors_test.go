package domain

import (
	"errors"
	"testing"
)

func TestDomainErrorError(t *testing.T) {
	err := &DomainError{Code: ErrNotFound, Message: "user not found"}
	expected := "not_found: user not found"

	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestDomainErrorUnwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := &DomainError{Code: ErrTimeout, Message: "operation timed out", Cause: cause}

	if errors.Unwrap(err) != cause {
		t.Errorf("Unwrap() did not return cause")
	}
}

func TestDomainErrorUnwrapNil(t *testing.T) {
	err := &DomainError{Code: ErrConflict, Message: "conflict"}

	if errors.Unwrap(err) != nil {
		t.Errorf("Unwrap() should return nil for nil cause")
	}
}

func TestDomainErrorIs(t *testing.T) {
	err1 := &DomainError{Code: ErrNotFound, Message: "not found"}

	// errors.Is checks for direct equality or Unwrap chain
	if !errors.Is(err1, err1) {
		t.Errorf("errors.Is should work with same DomainError")
	}
}

func TestNotFound(t *testing.T) {
	err := NotFound("resource not found")

	if err.Code != ErrNotFound {
		t.Errorf("Code = %s, want not_found", err.Code)
	}

	if err.Message != "resource not found" {
		t.Errorf("Message = %s, want 'resource not found'", err.Message)
	}
}

func TestRateLimit(t *testing.T) {
	err := RateLimit("API rate limit exceeded")

	if err.Code != ErrRateLimit {
		t.Errorf("Code = %s, want rate_limit", err.Code)
	}
}

func TestTimeout(t *testing.T) {
	cause := errors.New("context deadline exceeded")
	err := Timeout("operation timed out", cause)

	if err.Code != ErrTimeout {
		t.Errorf("Code = %s, want timeout", err.Code)
	}

	if errors.Unwrap(err) != cause {
		t.Errorf("Cause not preserved")
	}
}

func TestConflict(t *testing.T) {
	err := Conflict("resource already exists")

	if err.Code != ErrConflict {
		t.Errorf("Code = %s, want conflict", err.Code)
	}
}

func TestValidation(t *testing.T) {
	err := Validation("invalid email format")

	if err.Code != ErrValidation {
		t.Errorf("Code = %s, want validation", err.Code)
	}
}
