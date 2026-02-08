package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Search parameter validation
// ---------------------------------------------------------------------------

// SearchParams holds parsed and validated search parameters.
type SearchParams struct {
	Query    string
	Limit    int
	Offset   int
	From     time.Time
	To       time.Time
	Language string
	Bot      string
}

// ParseSearchParams extracts search parameters from the request.
// Returns a ValidationError if any parameter cannot be parsed.
func ParseSearchParams(r *http.Request) (SearchParams, *ValidationError) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit == 0 {
		limit = 50
	}

	offsetStr := q.Get("offset")
	var offset int
	if offsetStr != "" {
		var err error
		offset, err = strconv.Atoi(offsetStr)
		if err != nil {
			return SearchParams{}, &ValidationError{
				Field:   "offset",
				Message: fmt.Sprintf("offset must be a valid integer, got '%s'", offsetStr),
				Code:    ErrCodeInvalidParameter,
			}
		}
	}

	from, err := parseTimeQuery(r, "from", time.Now().Add(-7*24*time.Hour))
	if err != nil {
		return SearchParams{}, &ValidationError{
			Field:   "from",
			Message: "from must be RFC3339 or Unix timestamp",
			Code:    ErrCodeInvalidParameter,
		}
	}
	to, err := parseTimeQuery(r, "to", time.Now())
	if err != nil {
		return SearchParams{}, &ValidationError{
			Field:   "to",
			Message: "to must be RFC3339 or Unix timestamp",
			Code:    ErrCodeInvalidParameter,
		}
	}

	return SearchParams{
		Query:    q.Get("q"),
		Limit:    limit,
		Offset:   offset,
		From:     from,
		To:       to,
		Language: q.Get("language"),
		Bot:      q.Get("bot"),
	}, nil
}

// ValidateSearchParams validates search parameters.
func ValidateSearchParams(params SearchParams) *ValidationError {
	if params.Query == "" {
		return ErrEmptyQuery
	}
	if params.Limit < 1 || params.Limit > 100 {
		return ErrInvalidLimit
	}
	if params.Offset < 0 || params.Offset > 10000 {
		return ErrInvalidOffset
	}
	if params.From.After(params.To) {
		return ErrInvalidTimeRange
	}
	return nil
}

// ---------------------------------------------------------------------------
// Trending parameter validation
// ---------------------------------------------------------------------------

// TrendingParams holds parsed trending parameters.
type TrendingParams struct {
	Limit    int
	Language string
}

// ParseTrendingParams extracts trending parameters from the request.
func ParseTrendingParams(r *http.Request) (TrendingParams, *ValidationError) {
	limit, err := parseIntQuery(r, "limit", 20, 100)
	if err != nil {
		return TrendingParams{}, &ValidationError{
			Field:   "limit",
			Message: "limit must be an integer between 1 and 100",
			Code:    ErrCodeInvalidParameter,
		}
	}
	return TrendingParams{
		Limit:    limit,
		Language: r.URL.Query().Get("language"),
	}, nil
}

// ---------------------------------------------------------------------------
// Alert parameter validation
// ---------------------------------------------------------------------------

// AlertParams holds parsed alert parameters.
type AlertParams struct {
	Limit     int
	Offset    int
	Since     time.Time
	Severity  string
	AlertType string
}

// ParseAndValidateAlertParams parses and validates alert parameters.
func ParseAndValidateAlertParams(r *http.Request) (AlertParams, *ValidationError) {
	limit, err := parseIntQuery(r, "limit", 20, 100)
	if err != nil {
		return AlertParams{}, &ValidationError{
			Field:   "limit",
			Message: "limit must be an integer between 1 and 100",
			Code:    ErrCodeInvalidParameter,
		}
	}

	offset, err := parseIntQuery(r, "offset", 0, 10000)
	if err != nil {
		return AlertParams{}, &ValidationError{
			Field:   "offset",
			Message: "offset must be a non-negative integer up to 10000",
			Code:    ErrCodeInvalidParameter,
		}
	}

	since, err := parseTimeQuery(r, "since", time.Now().Add(-24*time.Hour))
	if err != nil {
		return AlertParams{}, &ValidationError{
			Field:   "since",
			Message: "since must be RFC3339 or Unix timestamp",
			Code:    ErrCodeInvalidParameter,
		}
	}

	severity := r.URL.Query().Get("severity")
	if severity != "" {
		if verr := ValidateSeverity(severity); verr != nil {
			return AlertParams{}, verr
		}
	}

	alertType := r.URL.Query().Get("type")
	if alertType != "" {
		if verr := ValidateAlertType(alertType); verr != nil {
			return AlertParams{}, verr
		}
	}

	return AlertParams{
		Limit:     limit,
		Offset:    offset,
		Since:     since,
		Severity:  severity,
		AlertType: alertType,
	}, nil
}

// ---------------------------------------------------------------------------
// Individual validators
// ---------------------------------------------------------------------------

// ValidateLimit checks that a limit is within bounds.
func ValidateLimit(limit, min, max int) *ValidationError {
	if limit < min || limit > max {
		return &ValidationError{
			Field:   "limit",
			Message: fmt.Sprintf("limit must be between %d and %d", min, max),
			Code:    ErrCodeInvalidParameter,
		}
	}
	return nil
}

// ValidateSeverity checks for valid severity levels.
func ValidateSeverity(severity string) *ValidationError {
	valid := map[string]bool{"low": true, "medium": true, "high": true, "critical": true}
	if !valid[strings.ToLower(severity)] {
		return &ValidationError{
			Field:   "severity",
			Message: fmt.Sprintf("invalid severity '%s'; valid values: low, medium, high, critical", severity),
			Code:    ErrCodeInvalidParameter,
		}
	}
	return nil
}

// ValidateAlertType checks for valid alert types.
func ValidateAlertType(alertType string) *ValidationError {
	valid := map[string]bool{"spike": true, "edit_war": true, "spikes": true, "editwars": true}
	if !valid[strings.ToLower(alertType)] {
		return &ValidationError{
			Field:   "type",
			Message: fmt.Sprintf("invalid alert type '%s'; valid types: spike, edit_war", alertType),
			Code:    ErrCodeInvalidParameter,
		}
	}
	return nil
}

// ValidateTimeRange ensures from is before to.
func ValidateTimeRange(from, to time.Time) *ValidationError {
	if from.After(to) {
		return ErrInvalidTimeRange
	}
	return nil
}
