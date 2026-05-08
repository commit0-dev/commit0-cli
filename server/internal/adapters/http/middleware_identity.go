package http

import (
	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// HeaderUserID is the canonical request header carrying the acting user's ID.
//
// Choice: a header (not a cookie or auth token) so the platform stays
// trivially scriptable from CLIs, MCP clients, and CI jobs while a more
// sophisticated authentication layer (OIDC, mTLS) is layered on later.
//
// Threat model: this header is *not* an authentication primitive. A
// production deployment puts an authenticating reverse proxy (Caddy, nginx,
// or Cloudflare Access) in front of the server and rewrites the header from
// the verified claim. The middleware here trusts whatever it receives.
const HeaderUserID = "X-User-ID"

// IdentityMiddleware extracts X-User-ID from the request, resolves it
// through the IdentityService, and attaches the resulting Identity to
// the request context.
//
// Anonymous requests (no header, or unresolvable user) continue through
// the chain with the zero-value Identity{} attached. Downstream code must
// handle that explicitly via Identity.IsAnonymous() or AuthorID() (which
// falls back to "system").
//
// When idSvc is nil (single-tenant deployment without identity persistence)
// the middleware is a no-op — every request is anonymous.
func IdentityMiddleware(idSvc *app.IdentityService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if idSvc == nil {
			c.Next()
			return
		}
		userID := c.GetHeader(HeaderUserID)
		identity := idSvc.Resolve(c.Request.Context(), userID)
		ctx := domain.WithIdentity(c.Request.Context(), identity)
		c.Request = c.Request.WithContext(ctx)
		// Mirror the identity into Gin's per-request store so handlers that
		// already use c.Get/MustGet patterns can reach it without going
		// through context.Context. Both paths read the same value.
		c.Set("identity", identity)
		c.Next()
	}
}
