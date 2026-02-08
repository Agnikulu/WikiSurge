package api

import (
	"fmt"
	"net/http"
)

// ---------------------------------------------------------------------------
// Standardized Error Codes
// ---------------------------------------------------------------------------

const (
	ErrCodeInvalidParameter = "INVALID_PARAMETER"
	ErrCodeRateLimitExceeded = "RATE_LIMIT_EXCEEDED"
	ErrCodeInternalError     = "INTERNAL_ERROR"
	ErrCodeServiceUnavailable = "SERVICE_UNAVAILABLE"
	ErrCodeNotFound          = "NOT_FOUND"
	ErrCodeUnauthorized      = "UNAUTHORIZED"
	ErrCodeTimeout           = "TIMEOUT"
	ErrCodeMethodNotAllowed  = "METHOD_NOT_ALLOWED"
)

// ---------------------------------------------------------------------------
// API Error type
// ---------------------------------------------------------------------------

// APIError is the standardised error envelope returned by all endpoints.
type APIError struct {
	Message   string `json:"message"`
	Code      string `json:"code"`
	Details   string `json:"details,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// APIErrorResponse wraps APIError with a top-level "error" key.
type APIErrorResponse struct {
	Error APIError `json:"error"`
}

// ---------------------------------------------------------------------------
// Sentinel errors for validation
// ---------------------------------------------------------------------------

// ValidationError represents a request validation failure.
type ValidationError struct {
	Field   string
	Message string
	Code    string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

var (
	ErrEmptyQuery       = &ValidationError{Field: "q", Message: "query parameter is required", Code: ErrCodeInvalidParameter}
	ErrInvalidLimit     = &ValidationError{Field: "limit", Message: "limit must be between 1 and 100", Code: ErrCodeInvalidParameter}
	ErrInvalidOffset    = &ValidationError{Field: "offset", Message: "offset must be non-negative and <= 10000", Code: ErrCodeInvalidParameter}
	ErrInvalidTimeRange = &ValidationError{Field: "from/to", Message: "'from' must be before 'to'", Code: ErrCodeInvalidParameter}
	ErrInvalidSeverity  = &ValidationError{Field: "severity", Message: "severity must be one of: low, medium, high, critical", Code: ErrCodeInvalidParameter}
	ErrInvalidAlertType = &ValidationError{Field: "type", Message: "alert type must be one of: spike, edit_war", Code: ErrCodeInvalidParameter}
	ErrInvalidTimestamp = &ValidationError{Field: "timestamp", Message: "must be RFC3339 or Unix timestamp", Code: ErrCodeInvalidParameter}
)

// ---------------------------------------------------------------------------
// Error response helpers
// ---------------------------------------------------------------------------

// writeAPIError writes a standardized error response with request ID.
func writeAPIError(w http.ResponseWriter, r *http.Request, status int, message, code, details string) {
	reqID := GetRequestID(r.Context())
	respondJSON(w, status, APIErrorResponse{
		Error: APIError{
			Message:   message,
			Code:      code,
			Details:   details,
			RequestID: reqID,
		},
	})
}

// writeValidationError writes a validation error response.
func writeValidationError(w http.ResponseWriter, r *http.Request, err *ValidationError) {
	writeAPIError(w, r, http.StatusBadRequest, err.Message, err.Code, fmt.Sprintf("field: %s", err.Field))
}

// mapStatusToCode returns the standard error code for an HTTP status.
func mapStatusToCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return ErrCodeInvalidParameter
	case http.StatusUnauthorized:
		return ErrCodeUnauthorized
	case http.StatusNotFound:
		return ErrCodeNotFound
	case http.StatusTooManyRequests:
		return ErrCodeRateLimitExceeded
	case http.StatusServiceUnavailable:
		return ErrCodeServiceUnavailable
	case http.StatusGatewayTimeout:
		return ErrCodeTimeout
	default:
		return ErrCodeInternalError
	}
}
