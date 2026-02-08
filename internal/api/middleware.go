package api

import (
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/metrics"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

// responseRecorder wraps http.ResponseWriter to capture the status code.
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.statusCode = code
	rr.ResponseWriter.WriteHeader(code)
}

// LoggerMiddleware logs every request with method, path, status, and duration.
func LoggerMiddleware(logger zerolog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := newResponseRecorder(w)

		next.ServeHTTP(rec, r)

		duration := time.Since(start)
		logger.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", rec.statusCode).
			Dur("duration", duration).
			Str("client_ip", clientIP(r)).
			Str("user_agent", r.UserAgent()).
			Msg("request")
	})
}

// RecoveryMiddleware catches panics, logs a stack trace, and returns 500.
func RecoveryMiddleware(logger zerolog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				stack := debug.Stack()
				logger.Error().
					Interface("panic", err).
					Bytes("stack", stack).
					Str("path", r.URL.Path).
					Msg("panic recovered")

				metrics.IncrementCounter("processing_errors_total", map[string]string{"consumer": "api_panic"})
				respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// CORSMiddleware sets CORS headers and handles OPTIONS preflight.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// MetricsMiddleware tracks request count and duration per endpoint.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		metrics.APIRequestsInFlight.WithLabelValues().Inc()
		defer metrics.APIRequestsInFlight.WithLabelValues().Dec()

		rec := newResponseRecorder(w)
		next.ServeHTTP(rec, r)

		duration := time.Since(start).Seconds()
		endpoint := normalizeEndpoint(r.URL.Path)

		metrics.APIRequestsTotal.WithLabelValues(endpoint, r.Method).Inc()
		metrics.APIRequestDuration.WithLabelValues(endpoint).Observe(duration)
	})
}

// RateLimitMiddleware applies a global token-bucket rate limiter.
func RateLimitMiddleware(rps int, next http.Handler) http.Handler {
	if rps <= 0 {
		rps = 1000
	}
	limiter := rate.NewLimiter(rate.Limit(rps), rps*2)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			metrics.RateLimitHitsTotal.WithLabelValues().Inc()
			respondError(w, http.StatusTooManyRequests, "Rate limit exceeded", "RATE_LIMITED")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// normalizeEndpoint collapses dynamic path segments for metric labels.
func normalizeEndpoint(path string) string {
	switch {
	case path == "/health":
		return "/health"
	case strings.HasPrefix(path, "/api/trending"):
		return "/api/trending"
	case strings.HasPrefix(path, "/api/stats"):
		return "/api/stats"
	case strings.HasPrefix(path, "/api/alerts"):
		return "/api/alerts"
	case strings.HasPrefix(path, "/api/edit-wars"):
		return "/api/edit-wars"
	case strings.HasPrefix(path, "/api/search"):
		return "/api/search"
	default:
		return "/other"
	}
}

// clientIP extracts the client IP from X-Forwarded-For or RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	return r.RemoteAddr
}
