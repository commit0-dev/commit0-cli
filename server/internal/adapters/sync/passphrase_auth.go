package sync

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// Compile-time interface check.
var _ domain.SyncAuth = (*PassphraseAuth)(nil)

// PassphraseAuth implements SyncAuth using a shared passphrase with HMAC-SHA256.
// This is the built-in OSS auth mechanism. Vendors can implement SyncAuth
// with PKI, OIDC, SAML, etc.
type PassphraseAuth struct {
	passphrase string
}

// NewPassphraseAuth creates a passphrase-based SyncAuth.
// Returns nil (no auth) if passphrase is empty.
func NewPassphraseAuth(passphrase string) *PassphraseAuth {
	if passphrase == "" {
		return nil
	}
	return &PassphraseAuth{passphrase: passphrase}
}

// SignBundle produces an HMAC-SHA256 signature for a bundle's ContentHash.
func (a *PassphraseAuth) SignBundle(contentHash string) (string, error) {
	mac := hmac.New(sha256.New, []byte(a.passphrase))
	mac.Write([]byte(contentHash))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifyBundle checks the HMAC-SHA256 signature against the ContentHash.
func (a *PassphraseAuth) VerifyBundle(contentHash, signature string) error {
	expected, err := a.SignBundle(contentHash)
	if err != nil {
		return fmt.Errorf("compute expected signature: %w", err)
	}
	sigBytes, err := hex.DecodeString(signature)
	if err != nil {
		return types.BundleCorrupt("invalid signature encoding")
	}
	expectedBytes, _ := hex.DecodeString(expected)
	if !hmac.Equal(sigBytes, expectedBytes) {
		return types.AuthFailed("HMAC signature mismatch")
	}
	return nil
}
