package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// Security Headers Middleware
// ---------------------------------------------------------------------------

// SecurityHeadersMiddleware adds standard security headers to every response.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")

		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// Request ID Middleware
// ---------------------------------------------------------------------------

// contextKey is an unexported type to prevent collisions with context keys
// defined outside this package.
type contextKey string

const requestIDKey contextKey = "request_id"

// RequestIDMiddleware generates a unique ID for every request, adds it to the
// request context and logs, and returns it in the X-Request-ID response
// header.  If the caller already supplies X-Request-ID it is reused.
func RequestIDMiddleware(logger zerolog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.New().String()
		}

		// Attach to response.
		w.Header().Set("X-Request-ID", id)

		// Attach to context so downstream handlers/logging can use it.
		ctx := context.WithValue(r.Context(), requestIDKey, id)

		// Enrich logger.
		subLogger := logger.With().Str("request_id", id).Logger()
		ctx = subLogger.WithContext(ctx)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID retrieves the request ID from the context (or "" if absent).
func GetRequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// ---------------------------------------------------------------------------
// Request Validation Middleware
// ---------------------------------------------------------------------------

const (
	maxQueryStringLen = 2048
	maxRequestBodySize = 1 << 20 // 1 MiB
)

// sqlInjectionPatterns is a simple list of suspicious SQL fragments.
var sqlInjectionPatterns = regexp.MustCompile(
	`(?i)(union\s+select|;\s*drop\s|;\s*delete\s|;\s*insert\s|'\s*or\s+'|'\s*or\s+1\s*=\s*1|--\s|/\*|\*/|;\s*truncate\s|;\s*update\s)`,
)

// RequestValidationMiddleware rejects obviously malformed or dangerous
// requests before they reach the handlers.
func RequestValidationMiddleware(next http.Handler) http.Handler {
	allowedMethods := map[string]bool{
		http.MethodGet:     true,
		http.MethodPost:    true,
		http.MethodPut:     true,
		http.MethodOptions: true,
		http.MethodHead:    true,
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Method allow-list.
		if !allowedMethods[r.Method] {
			respondError(w, http.StatusMethodNotAllowed,
				fmt.Sprintf("Method %s not allowed", r.Method), "METHOD_NOT_ALLOWED")
			return
		}

		// 2. Content-Type for POST/PUT requests.
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			ct := r.Header.Get("Content-Type")
			if ct == "" || (!strings.Contains(ct, "application/json") && !strings.Contains(ct, "application/x-www-form-urlencoded")) {
				respondError(w, http.StatusUnsupportedMediaType,
					"Content-Type must be application/json or application/x-www-form-urlencoded",
					"UNSUPPORTED_MEDIA_TYPE")
				return
			}
		}

		// 3. Query string length.
		if len(r.URL.RawQuery) > maxQueryStringLen {
			respondError(w, http.StatusBadRequest, "Query string too long", "QUERY_TOO_LONG")
			return
		}

		// 4. Request body size (for POST).
		if r.ContentLength > int64(maxRequestBodySize) {
			respondError(w, http.StatusRequestEntityTooLarge,
				"Request body too large (max 1 MiB)", "BODY_TOO_LARGE")
			return
		}
		if r.Body != nil && r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, int64(maxRequestBodySize))
		}

		// 5. SQL injection heuristics on query parameters.
		for key, values := range r.URL.Query() {
			for _, v := range values {
				if containsSQLInjection(v) {
					respondError(w, http.StatusBadRequest,
						fmt.Sprintf("Invalid query parameter: %s", key), "INVALID_QUERY")
					return
				}
			}
		}

		// 6. Path traversal check.
		if strings.Contains(r.URL.Path, "..") {
			respondError(w, http.StatusBadRequest, "Invalid path", "INVALID_PATH")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// containsSQLInjection returns true if s matches common SQL injection
// patterns.  This is a defence-in-depth heuristic, not a replacement for
// parameterised queries.
func containsSQLInjection(s string) bool {
	return sqlInjectionPatterns.MatchString(s)
}
