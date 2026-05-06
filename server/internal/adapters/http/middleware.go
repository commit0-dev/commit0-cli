package http

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// SlogMiddleware returns a Gin middleware that logs each request with slog.
func SlogMiddleware(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if log != nil {
			log.Info("request",
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"status", c.Writer.Status(),
				"latency_ms", time.Since(start).Milliseconds(),
				"request_id", c.GetHeader("X-Request-Id"),
			)
		}
	}
}
