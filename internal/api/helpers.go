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

// PaginationInfo carries pagination metadata in responses.
type PaginationInfo struct {
	Total   int64 `json:"total"`
	Limit   int   `json:"limit"`
	Offset  int   `json:"offset"`
	HasMore bool  `json:"has_more"`
}

// SearchResponse is returned by GET /api/search.
type SearchResponse struct {
	Hits       []SearchHit    `json:"hits"`
	Total      int64          `json:"total"`
	Query      string         `json:"query"`
	Pagination PaginationInfo `json:"pagination"`
}

// SearchHit represents a single search result.
type SearchHit struct {
	Title      string  `json:"title"`
	User       string  `json:"user"`
	Timestamp  string  `json:"timestamp"`
	Comment    string  `json:"comment"`
	ByteChange int     `json:"byte_change,omitempty"`
	Wiki       string  `json:"wiki"`
	Score      float64 `json:"score"`
	Language   string  `json:"language,omitempty"`
}

// AlertsResponse is returned by GET /api/alerts.
type AlertsResponse struct {
	Alerts     []AlertEntry   `json:"alerts"`
	Total      int            `json:"total"`
	Pagination PaginationInfo `json:"pagination"`
}

// AlertEntry is a single alert in the response.
type AlertEntry struct {
	Type         string   `json:"type"`
	PageTitle    string   `json:"page_title"`
	SpikeRatio   float64  `json:"spike_ratio,omitempty"`
	Severity     string   `json:"severity"`
	Timestamp    string   `json:"timestamp"`
	Edits5Min    int      `json:"edits_5min,omitempty"`
	EditorCount  int      `json:"editor_count,omitempty"`
	EditCount    int      `json:"edit_count,omitempty"`
	RevertCount  int      `json:"revert_count,omitempty"`
	Editors      []string `json:"editors,omitempty"`
	Wiki         string   `json:"wiki,omitempty"`
}

// EditWarEntry is returned by GET /api/edit-wars.
type EditWarEntry struct {
	PageTitle   string   `json:"page_title"`
	EditorCount int      `json:"editor_count"`
	EditCount   int      `json:"edit_count"`
	RevertCount int      `json:"revert_count"`
	Severity    string   `json:"severity"`
	StartTime   string   `json:"start_time,omitempty"`
	Editors     []string `json:"editors"`
	Active      bool     `json:"active"`
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

// parseBoolQuery reads a boolean query param with a default value.
func parseBoolQuery(r *http.Request, name string, defaultVal bool) bool {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return defaultVal
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return defaultVal
	}
	return v
}
