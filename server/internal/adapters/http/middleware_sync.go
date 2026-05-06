package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// SyncAuthMiddleware creates a Gin middleware that validates sync requests
// using a shared passphrase via HMAC challenge-response.
// If passphrase is empty, all sync requests are allowed (no auth).
func SyncAuthMiddleware(passphrase string) gin.HandlerFunc {
	if passphrase == "" {
		return func(c *gin.Context) { c.Next() }
	}

	return func(c *gin.Context) {
		// Expect: Authorization: Bearer hmac(<passphrase>, <timestamp>)
		// Where timestamp is in the X-Sync-Timestamp header.
		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "missing Authorization header"})
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")

		timestamp := c.GetHeader("X-Sync-Timestamp")
		if timestamp == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "missing X-Sync-Timestamp header"})
			return
		}

		// Verify HMAC(passphrase, timestamp) == token.
		mac := hmac.New(sha256.New, []byte(passphrase))
		mac.Write([]byte(timestamp))
		expected := hex.EncodeToString(mac.Sum(nil))

		tokenBytes, err := hex.DecodeString(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "invalid token encoding"})
			return
		}
		expectedBytes, _ := hex.DecodeString(expected)

		if !hmac.Equal(tokenBytes, expectedBytes) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "invalid credentials"})
			return
		}

		c.Next()
	}
}
