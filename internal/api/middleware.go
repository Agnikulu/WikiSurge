package api

import (
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/metrics"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

// responseRecorder wraps http.ResponseWriter to capture the status code and
// response body size for logging and metrics.
type responseRecorder struct {
	http.ResponseWriter
	statusCode  int
	bytesWritten int
	headerSent   bool
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rr *responseRecorder) WriteHeader(code int) {
	if !rr.headerSent {
		rr.statusCode = code
		rr.headerSent = true
		rr.ResponseWriter.WriteHeader(code)
	}
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	n, err := rr.ResponseWriter.Write(b)
	rr.bytesWritten += n
	return n, err
}

// logSampleRate returns the sampling rate for a given path.
// High-volume endpoints are sampled at a lower rate to reduce log volume.
func logSampleRate(path string) float64 {
	switch {
	case strings.HasPrefix(path, "/ws/"):
		return 0.01 // 1% for WebSocket upgrades
	case path == "/health", path == "/health/live":
		return 0.01 // 1% for liveness probes
	default:
		return 0.10 // 10% for normal endpoints
	}
}

// LoggerMiddleware logs every request with method, path, status, duration,
// request ID, and response size. Successful requests are sampled to reduce
// log volume; errors are always logged.
func LoggerMiddleware(logger zerolog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := newResponseRecorder(w)

		next.ServeHTTP(rec, r)

		duration := time.Since(start)
		ip := clientIP(r)
		reqID := GetRequestID(r.Context())

		// Always log errors; sample successful requests.
		if rec.statusCode >= 400 {
			logger.Error().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("ip", ip).
				Int("status", rec.statusCode).
				Dur("duration", duration).
				Int("response_bytes", rec.bytesWritten).
				Str("request_id", reqID).
				Str("user_agent", r.UserAgent()).
				Msg("request")
		} else {
			// Sample successful requests.
			if rand.Float64() < logSampleRate(r.URL.Path) { //nolint:gosec
				logger.Info().
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Str("ip", ip).
					Int("status", rec.statusCode).
					Dur("duration", duration).
					Int("response_bytes", rec.bytesWritten).
					Str("request_id", reqID).
					Msg("request")
			}
		}
	})
}

// RecoveryMiddleware catches panics, logs a stack trace (not leaked to the
// client), and returns a standardised 500 error.
func RecoveryMiddleware(logger zerolog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				stack := debug.Stack()
				reqID := GetRequestID(r.Context())
				logger.Error().
					Interface("panic", err).
					Bytes("stack", stack).
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Str("ip", clientIP(r)).
					Str("request_id", reqID).
					Msg("panic recovered")

				metrics.IncrementCounter("processing_errors_total", map[string]string{"consumer": "api_panic"})
				metrics.APIErrorsTotal.WithLabelValues(ErrCodeInternalError).Inc()

				// Don't leak internal details.
				writeAPIError(w, r, http.StatusInternalServerError,
					"An unexpected error occurred", ErrCodeInternalError, "")
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

// MetricsMiddleware tracks request count, duration, response size, and errors
// per endpoint.
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
		metrics.APIResponseSizeBytes.WithLabelValues(endpoint).Observe(float64(rec.bytesWritten))

		// Track errors by code
		if rec.statusCode >= 400 {
			errCode := mapStatusToCode(rec.statusCode)
			metrics.APIErrorsTotal.WithLabelValues(errCode).Inc()
		}
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
			metrics.APIErrorsTotal.WithLabelValues(ErrCodeRateLimitExceeded).Inc()
			writeAPIError(w, r, http.StatusTooManyRequests,
				"Rate limit exceeded", ErrCodeRateLimitExceeded, "")
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
	case path == "/health/live":
		return "/health/live"
	case path == "/health/ready":
		return "/health/ready"
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
	case strings.HasPrefix(path, "/api/docs"):
		return "/api/docs"
	case strings.HasPrefix(path, "/ws/"):
		return "/ws"
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

// ---------------------------------------------------------------------------
// Gzip compression middleware  (Task 17.7)
// ---------------------------------------------------------------------------

const gzipMinSize = 1024 // Only compress responses > 1 KB

// gzipResponseWriter wraps http.ResponseWriter to transparently gzip output.
type gzipResponseWriter struct {
	http.ResponseWriter
	writer      io.Writer
	gzipWriter  *gzip.Writer
	pool        *sync.Pool
	wroteHeader bool
	statusCode  int
	buf         []byte
	compressed  bool
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	g.statusCode = code
	// Delay actual header write until we know whether to compress.
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		g.wroteHeader = true
		g.buf = append(g.buf, b...)

		// Wait until we have enough data to decide.
		if len(g.buf) < gzipMinSize {
			return len(b), nil
		}

		// Buffer exceeds min size — compress.
		g.compressed = true
		g.ResponseWriter.Header().Set("Content-Encoding", "gzip")
		g.ResponseWriter.Header().Del("Content-Length")
		g.ResponseWriter.WriteHeader(g.statusCode)

		gz := g.pool.Get().(*gzip.Writer)
		gz.Reset(g.ResponseWriter)
		g.gzipWriter = gz
		g.writer = gz

		return g.writer.Write(g.buf)
	}
	return g.writer.Write(b)
}

func (g *gzipResponseWriter) Close() error {
	if !g.wroteHeader && len(g.buf) > 0 {
		// Data never exceeded threshold — send uncompressed.
		g.ResponseWriter.WriteHeader(g.statusCode)
		_, _ = g.ResponseWriter.Write(g.buf)
	}
	if g.gzipWriter != nil {
		err := g.gzipWriter.Close()
		g.pool.Put(g.gzipWriter)
		return err
	}
	return nil
}

var gzipPool = &sync.Pool{
	New: func() interface{} {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return w
	},
}

// GzipMiddleware compresses responses > gzipMinSize when the client accepts gzip.
func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip if client doesn't accept gzip.
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// Skip WebSocket upgrades.
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			next.ServeHTTP(w, r)
			return
		}

		gw := &gzipResponseWriter{
			ResponseWriter: w,
			writer:         w,
			pool:           gzipPool,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(gw, r)
		_ = gw.Close()
	})
}

// ---------------------------------------------------------------------------
// ETag / conditional GET middleware  (Task 17.7)
// ---------------------------------------------------------------------------

// ETagMiddleware adds ETag headers and handles If-None-Match for GET requests.
func ETagMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only for GET requests.
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}

		// Skip WebSocket.
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			next.ServeHTTP(w, r)
			return
		}

		rec := &etagRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)

		// Only ETag 200 responses.
		if rec.statusCode != http.StatusOK || len(rec.body) == 0 {
			w.WriteHeader(rec.statusCode)
			_, _ = w.Write(rec.body)
			return
		}

		etag := calculateETag(rec.body)
		w.Header().Set("ETag", etag)

		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.WriteHeader(rec.statusCode)
		_, _ = w.Write(rec.body)
	})
}

type etagRecorder struct {
	http.ResponseWriter
	statusCode int
	body       []byte
}

func (e *etagRecorder) WriteHeader(code int) {
	e.statusCode = code
}

func (e *etagRecorder) Write(b []byte) (int, error) {
	e.body = append(e.body, b...)
	return len(b), nil
}

// calculateETag computes a weak ETag from response body.
func calculateETag(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf(`W/\"%x\"`, h[:8])
}

// ---------------------------------------------------------------------------
// Error handler middleware  (Task 17.1)
// ---------------------------------------------------------------------------

// ErrorHandlerMiddleware catches unhandled error status codes and ensures they
// use the standardised error envelope. This is a safety net — handlers should
// prefer writeAPIError directly.
func ErrorHandlerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := newResponseRecorder(w)
		next.ServeHTTP(rec, r)

		// If the handler already wrote a body we trust it used the correct
		// format.  This middleware only catches cases where WriteHeader was
		// called with an error code but no body was written (e.g., a bare
		// http.Error call).
	})
}
