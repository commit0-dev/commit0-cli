package client

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/commit0-dev/commit0/internal/domain"
)

// errorResponse matches the server's gin.H{"message": ...} error format.
type errorResponse struct {
	Message string `json:"message"`
}

// mapHTTPError translates an HTTP error response into the appropriate domain error.
// This reverses the server-side writeError() mapping in handlers.go.
func mapHTTPError(statusCode int, body []byte) error {
	var errResp errorResponse
	if err := json.Unmarshal(body, &errResp); err != nil || errResp.Message == "" {
		errResp.Message = string(body)
	}

	switch statusCode {
	case http.StatusNotFound:
		return domain.NotFound(errResp.Message)
	case http.StatusBadRequest:
		return domain.Validation(errResp.Message)
	case http.StatusConflict:
		return domain.Conflict(errResp.Message)
	case http.StatusTooManyRequests:
		return domain.RateLimit(errResp.Message)
	case http.StatusServiceUnavailable:
		return fmt.Errorf("service unavailable: %s", errResp.Message)
	default:
		return fmt.Errorf("server error %d: %s", statusCode, errResp.Message)
	}
}
