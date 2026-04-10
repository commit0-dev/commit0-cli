package domain

import "github.com/commit0-dev/commit0/pkg/types"

// Type aliases — canonical definitions live in pkg/types.
// All server-side code continues using domain.NotFound(), domain.DomainError, etc.
type ErrorCode = types.ErrorCode
type DomainError = types.DomainError

const (
	ErrNotFound   = types.ErrNotFound
	ErrRateLimit  = types.ErrRateLimit
	ErrTimeout    = types.ErrTimeout
	ErrConflict   = types.ErrConflict
	ErrValidation = types.ErrValidation
)

// NotFound creates a not found error.
var NotFound = types.NotFound

// RateLimit creates a rate limit error.
var RateLimit = types.RateLimit

// Timeout creates a timeout error.
var Timeout = types.Timeout

// Conflict creates a conflict error.
var Conflict = types.Conflict

// Validation creates a validation error.
var Validation = types.Validation
