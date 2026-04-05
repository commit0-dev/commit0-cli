package domain

import "fmt"

// ErrorCode represents the category of a domain error.
type ErrorCode string

const (
	ErrNotFound   ErrorCode = "not_found"
	ErrRateLimit  ErrorCode = "rate_limit"
	ErrTimeout    ErrorCode = "timeout"
	ErrConflict   ErrorCode = "conflict"
	ErrValidation ErrorCode = "validation"
)

// DomainError represents an error within the domain layer.
type DomainError struct {
	Cause   error
	Code    ErrorCode
	Message string
}

// Error implements the error interface.
func (e *DomainError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause error.
func (e *DomainError) Unwrap() error {
	return e.Cause
}

// NotFound creates a not found error.
func NotFound(msg string) *DomainError {
	return &DomainError{Code: ErrNotFound, Message: msg}
}

// RateLimit creates a rate limit error.
func RateLimit(msg string) *DomainError {
	return &DomainError{Code: ErrRateLimit, Message: msg}
}

// Timeout creates a timeout error.
func Timeout(msg string, cause error) *DomainError {
	return &DomainError{Code: ErrTimeout, Message: msg, Cause: cause}
}

// Conflict creates a conflict error.
func Conflict(msg string) *DomainError {
	return &DomainError{Code: ErrConflict, Message: msg}
}

// Validation creates a validation error.
func Validation(msg string) *DomainError {
	return &DomainError{Code: ErrValidation, Message: msg}
}
