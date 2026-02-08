package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Details string `json:"details,omitempty"`
}

// respondJSON writes a JSON payload with the given HTTP status.
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

// respondError writes a standard error response.
func respondError(w http.ResponseWriter, status int, message, code string) {
	respondJSON(w, status, ErrorResponse{
		Error: message,
		Code:  code,
	})
}

// respondErrorWithDetails writes an error response with additional details.
func respondErrorWithDetails(w http.ResponseWriter, status int, message, code, details string) {
	respondJSON(w, status, ErrorResponse{
		Error:   message,
		Code:    code,
		Details: details,
	})
}

// parseIntQuery reads an integer query param with a default and max.
func parseIntQuery(r *http.Request, name string, defaultVal, maxVal int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return defaultVal, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if v < 0 {
		return 0, strconv.ErrRange
	}
	if maxVal > 0 && v > maxVal {
		return 0, strconv.ErrRange
	}
	return v, nil
}

// parseTimeQuery reads a time query param (RFC3339 or Unix seconds).
func parseTimeQuery(r *http.Request, name string, defaultVal time.Time) (time.Time, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return defaultVal, nil
	}
	// Try RFC3339 first
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	// Try Unix timestamp
	unix, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(unix, 0), nil
}
