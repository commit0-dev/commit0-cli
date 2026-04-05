package http

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"
)

// SlogMiddleware returns an Echo middleware that logs each request with slog.
func SlogMiddleware(log *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)
			log.Info("request",
				"method", c.Request().Method,
				"path", c.Request().URL.Path,
				"status", c.Response().Status,
				"latency_ms", time.Since(start).Milliseconds(),
				"request_id", c.Response().Header().Get(echo.HeaderXRequestID),
			)
			return err
		}
	}
}
