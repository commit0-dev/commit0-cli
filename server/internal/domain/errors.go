package domain

import "github.com/commit0-dev/commit0/pkg/types"

// Type aliases — canonical definitions live in pkg/types.
// All server-side code continues using domain.NotFound(), domain.DomainError, etc.
type ErrorCode = types.ErrorCode
type DomainError = types.DomainError

const (
	ErrNotFound      = types.ErrNotFound
	ErrRateLimit     = types.ErrRateLimit
	ErrTimeout       = types.ErrTimeout
	ErrConflict      = types.ErrConflict
	ErrValidation    = types.ErrValidation
	ErrBundleCorrupt = types.ErrBundleCorrupt
	ErrSyncConflict  = types.ErrSyncConflict
	ErrAuthFailed    = types.ErrAuthFailed
	ErrOutOfScope    = types.ErrOutOfScope
)

var (
	NotFound      = types.NotFound
	RateLimit     = types.RateLimit
	Timeout       = types.Timeout
	Conflict      = types.Conflict
	Validation    = types.Validation
	BundleCorrupt = types.BundleCorrupt
	SyncConflict  = types.SyncConflict
	AuthFailed    = types.AuthFailed
	OutOfScope    = types.OutOfScope
)
