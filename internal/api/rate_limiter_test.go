package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helper: miniredis-backed RateLimiter
// ---------------------------------------------------------------------------

func testRateLimiter(t *testing.T, limit int) (*RateLimiter, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(func() { mr.Close() })

	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rc.Close() })

	cfg := config.APIRateLimiting{
		Enabled:           true,
		RequestsPerMinute: limit,
		BurstSize:         10,
		KeyType:           "ip",
		Whitelist:         []string{"10.0.0.0/8", "192.168.1.100"},
	}

	return NewRateLimiter(rc, cfg, zerolog.Nop()), mr
}

// ---------------------------------------------------------------------------
// Rate Limiter Unit Tests
// ---------------------------------------------------------------------------

func TestRateLimiter_AllowsWithinLimit(t *testing.T) {
	rl, _ := testRateLimiter(t, 10)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		req.RemoteAddr = "1.2.3.4:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "request %d should be allowed", i+1)
		assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Limit"))
		assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Remaining"))
		assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Reset"))
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	rl, _ := testRateLimiter(t, 5)

	// Override the per-endpoint limit for /api/stats so we actually hit 5.
	rl.limits["/api/stats"] = 5

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var blocked int
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		req.RemoteAddr = "1.2.3.4:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			blocked++
			// Verify 429 JSON body.
			var body RateLimitErrorResponse
			err := json.NewDecoder(rec.Body).Decode(&body)
			require.NoError(t, err)
			assert.Equal(t, "RATE_LIMIT", body.Code)
			assert.NotEmpty(t, rec.Header().Get("Retry-After"))
		}
	}
	assert.True(t, blocked > 0, "should have blocked at least one request")
}

func TestRateLimiter_PerEndpointLimits(t *testing.T) {
	rl, _ := testRateLimiter(t, 1000) // high default

	assert.Equal(t, 100, rl.getLimitForEndpoint("/api/search"))
	assert.Equal(t, 500, rl.getLimitForEndpoint("/api/trending"))
	assert.Equal(t, 1000, rl.getLimitForEndpoint("/api/stats"))
	assert.Equal(t, 500, rl.getLimitForEndpoint("/api/alerts"))
	assert.Equal(t, 1000, rl.getLimitForEndpoint("/unknown"), "unknown endpoint should use default")
}

func TestRateLimiter_WhitelistBypass(t *testing.T) {
	rl, _ := testRateLimiter(t, 1) // extremely low limit

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Whitelisted IP (10.0.0.0/8)
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		req.RemoteAddr = "10.1.2.3:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "whitelisted IP should never be rate-limited")
	}
}

func TestRateLimiter_WhitelistExactIP(t *testing.T) {
	rl, _ := testRateLimiter(t, 1)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exact match: 192.168.1.100
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		req.RemoteAddr = "192.168.1.100:9999"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}
}

func TestRateLimiter_SeparateCountersPerIP(t *testing.T) {
	rl, _ := testRateLimiter(t, 3)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Client A
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		req.RemoteAddr = "5.5.5.5:111"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	// Client B should have its own counter
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		req.RemoteAddr = "6.6.6.6:222"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}
}

func TestRateLimiter_RateLimitHeaders(t *testing.T) {
	rl, _ := testRateLimiter(t, 50)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.RemoteAddr = "9.9.9.9:100"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "1000", rec.Header().Get("X-RateLimit-Limit")) // /api/stats limit
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Reset"))
}

// ---------------------------------------------------------------------------
// IP Extraction Tests
// ---------------------------------------------------------------------------

func TestGetClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 70.41.3.18, 150.172.238.178")
	req.RemoteAddr = "127.0.0.1:1234"

	ip := getClientIP(req)
	assert.Equal(t, "203.0.113.50", ip)
}

func TestGetClientIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "198.51.100.42")
	req.RemoteAddr = "127.0.0.1:1234"

	ip := getClientIP(req)
	assert.Equal(t, "198.51.100.42", ip)
}

func TestGetClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.1:54321"

	ip := getClientIP(req)
	assert.Equal(t, "192.0.2.1", ip)
}

func TestGetClientIP_IPv6(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "[::1]:8080"

	ip := getClientIP(req)
	assert.Equal(t, "::1", ip)
}

// ---------------------------------------------------------------------------
// IsWhitelisted Tests
// ---------------------------------------------------------------------------

func TestIsWhitelisted_CIDR(t *testing.T) {
	rl, _ := testRateLimiter(t, 100)

	assert.True(t, rl.isWhitelisted("10.0.0.1"))
	assert.True(t, rl.isWhitelisted("10.255.255.255"))
	assert.False(t, rl.isWhitelisted("11.0.0.1"))
}

func TestIsWhitelisted_ExactIP(t *testing.T) {
	rl, _ := testRateLimiter(t, 100)

	assert.True(t, rl.isWhitelisted("192.168.1.100"))
	assert.False(t, rl.isWhitelisted("192.168.1.101"))
}

func TestIsWhitelisted_InvalidIP(t *testing.T) {
	rl, _ := testRateLimiter(t, 100)
	assert.False(t, rl.isWhitelisted("not-an-ip"))
}

// ---------------------------------------------------------------------------
// Security Headers Tests
// ---------------------------------------------------------------------------

func TestSecurityHeadersMiddleware(t *testing.T) {
	handler := SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	assert.Equal(t, "1; mode=block", rec.Header().Get("X-XSS-Protection"))
	assert.Contains(t, rec.Header().Get("Strict-Transport-Security"), "max-age=31536000")
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "default-src 'self'")
	assert.NotEmpty(t, rec.Header().Get("Referrer-Policy"))
	assert.NotEmpty(t, rec.Header().Get("Permissions-Policy"))
}

// ---------------------------------------------------------------------------
// Request ID Tests
// ---------------------------------------------------------------------------

func TestRequestIDMiddleware_GeneratesID(t *testing.T) {
	logger := zerolog.Nop()
	handler := RequestIDMiddleware(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the ID is accessible from context.
		id := GetRequestID(r.Context())
		assert.NotEmpty(t, id)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.NotEmpty(t, rec.Header().Get("X-Request-ID"))
}

func TestRequestIDMiddleware_ReusesExisting(t *testing.T) {
	logger := zerolog.Nop()
	handler := RequestIDMiddleware(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	existingID := "my-custom-request-id"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", existingID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, existingID, rec.Header().Get("X-Request-ID"))
}

// ---------------------------------------------------------------------------
// Request Validation Tests
// ---------------------------------------------------------------------------

func TestRequestValidation_MethodNotAllowed(t *testing.T) {
	handler := RequestValidationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{http.MethodDelete, http.MethodPut, http.MethodPatch} {
		req := httptest.NewRequest(method, "/api/stats", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, "method %s should be rejected", method)
	}
}

func TestRequestValidation_AllowedMethods(t *testing.T) {
	handler := RequestValidationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodOptions, http.MethodHead} {
		req := httptest.NewRequest(method, "/api/stats", nil)
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "method %s should be allowed", method)
	}
}

func TestRequestValidation_PostContentType(t *testing.T) {
	handler := RequestValidationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Missing Content-Type on POST
	req := httptest.NewRequest(http.MethodPost, "/api/search", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)

	// Wrong Content-Type
	req = httptest.NewRequest(http.MethodPost, "/api/search", nil)
	req.Header.Set("Content-Type", "text/plain")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
}

func TestRequestValidation_QueryTooLong(t *testing.T) {
	handler := RequestValidationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	longQuery := strings.Repeat("a", maxQueryStringLen+1)
	req := httptest.NewRequest(http.MethodGet, "/api/search?q="+longQuery, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRequestValidation_SQLInjection(t *testing.T) {
	handler := RequestValidationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		query string
		block bool
	}{
		{"normal search", false},
		{"hello world", false},
		{"'; DROP TABLE users; --", true},
		{"1 UNION SELECT * FROM passwords", true},
		{"' OR '1'='1", true},
		{"legitimate query", false},
	}

	for _, tt := range tests {
		v := url.Values{}
		v.Set("q", tt.query)
		req := httptest.NewRequest(http.MethodGet, "/api/search?"+v.Encode(), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if tt.block {
			assert.Equal(t, http.StatusBadRequest, rec.Code, "should block: %s", tt.query)
		} else {
			assert.Equal(t, http.StatusOK, rec.Code, "should allow: %s", tt.query)
		}
	}
}

func TestRequestValidation_PathTraversal(t *testing.T) {
	handler := RequestValidationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/../etc/passwd", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRequestValidation_BodyTooLarge(t *testing.T) {
	handler := RequestValidationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/search", nil)
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(maxRequestBodySize + 1)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

// ---------------------------------------------------------------------------
// SQL Injection Detection Unit Test
// ---------------------------------------------------------------------------

func TestContainsSQLInjection(t *testing.T) {
	assert.False(t, containsSQLInjection("hello world"))
	assert.False(t, containsSQLInjection("Wikipedia article"))
	assert.True(t, containsSQLInjection("'; DROP TABLE users;--"))
	assert.True(t, containsSQLInjection("UNION SELECT password FROM users"))
	assert.True(t, containsSQLInjection("' OR '1'='1"))
}

// ---------------------------------------------------------------------------
// Integration: full middleware stack
// ---------------------------------------------------------------------------

func TestFullMiddlewareStack_SecurityHeaders(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rc.Close()

	cfg := &config.Config{
		Features: config.Features{Trending: true, EditWars: true},
		Redis: config.Redis{
			URL:       fmt.Sprintf("redis://%s", mr.Addr()),
			MaxMemory: "256mb",
			HotPages:  config.HotPages{MaxTracked: 500, PromotionThreshold: 3, WindowDuration: 15 * time.Minute, MaxMembersPerPage: 50, HotThreshold: 2, CleanupInterval: 5 * time.Minute},
			Trending:  config.TrendingConfig{Enabled: true, MaxPages: 500, HalfLifeMinutes: 30.0, PruneInterval: 5 * time.Minute},
		},
		Elasticsearch: config.Elasticsearch{Enabled: false},
		API: config.API{
			Port:      8080,
			RateLimit: 10000,
			RateLimiting: config.APIRateLimiting{
				Enabled:           true,
				RequestsPerMinute: 10000,
				BurstSize:         100,
				KeyType:           "ip",
				Whitelist:         []string{"127.0.0.1", "::1"},
			},
		},
		Logging: config.Logging{Level: "error", Format: "json"},
	}

	logger := zerolog.Nop()
	srv := NewAPIServer(rc, nil, nil, nil, nil, cfg, logger)
	h := srv.Handler()

	// Fire a request through the full stack.
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Security headers should be present.
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	assert.NotEmpty(t, rec.Header().Get("X-Request-ID"))
}

// ---------------------------------------------------------------------------
// isValidIP
// ---------------------------------------------------------------------------

func TestIsValidIP(t *testing.T) {
	assert.True(t, isValidIP("192.168.1.1"))
	assert.True(t, isValidIP("::1"))
	assert.False(t, isValidIP("not-an-ip"))
	assert.False(t, isValidIP(""))
}
