package types

import "fmt"

// ErrorCode represents the category of a domain error.
type ErrorCode string

const (
	ErrNotFound       ErrorCode = "not_found"
	ErrRateLimit      ErrorCode = "rate_limit"
	ErrTimeout        ErrorCode = "timeout"
	ErrConflict       ErrorCode = "conflict"
	ErrValidation     ErrorCode = "validation"
	ErrBundleCorrupt  ErrorCode = "bundle_corrupt"
	ErrSyncConflict   ErrorCode = "sync_conflict"
	ErrAuthFailed     ErrorCode = "auth_failed"
	ErrOutOfScope     ErrorCode = "out_of_scope"
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

// BundleCorrupt creates a bundle corruption error.
func BundleCorrupt(msg string) *DomainError {
	return &DomainError{Code: ErrBundleCorrupt, Message: msg}
}

// SyncConflict creates a sync conflict error.
func SyncConflict(msg string) *DomainError {
	return &DomainError{Code: ErrSyncConflict, Message: msg}
}

// AuthFailed creates an authentication failure error.
func AuthFailed(msg string) *DomainError {
	return &DomainError{Code: ErrAuthFailed, Message: msg}
}

// OutOfScope creates an out-of-scope error.
func OutOfScope(msg string) *DomainError {
	return &DomainError{Code: ErrOutOfScope, Message: msg}
}
