package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/metrics"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// RateLimiter
// ---------------------------------------------------------------------------

// RateLimiter implements a Redis-backed sliding-window rate limiter.
type RateLimiter struct {
	redis        *redis.Client
	config       config.APIRateLimiting
	limits       map[string]int // endpoint pattern → requests/min
	defaultLimit int
	whitelist    []*net.IPNet // parsed CIDR whitelist
	whitelistIPs []net.IP     // single-IP whitelist entries
	logger       zerolog.Logger
}

// NewRateLimiter creates a RateLimiter with the given Redis client and
// configuration.
func NewRateLimiter(redisClient *redis.Client, cfg config.APIRateLimiting, logger zerolog.Logger) *RateLimiter {
	rl := &RateLimiter{
		redis:        redisClient,
		config:       cfg,
		defaultLimit: cfg.RequestsPerMinute,
		logger:       logger.With().Str("component", "rate_limiter").Logger(),
	}

	if rl.defaultLimit <= 0 {
		rl.defaultLimit = 1000
	}

	// Per-endpoint limits.
	rl.limits = map[string]int{
		"/api/search":    100,  // expensive – lower limit
		"/api/trending":  500,  // moderate
		"/api/stats":     1000, // cheap – higher limit
		"/api/alerts":    500,
		"/api/edit-wars": 500,
	}

	// Parse whitelist.
	rl.parseWhitelist(cfg.Whitelist)

	return rl
}

// parseWhitelist converts string entries into net.IPNet / net.IP values.
func (rl *RateLimiter) parseWhitelist(entries []string) {
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		// CIDR notation
		if strings.Contains(entry, "/") {
			_, ipNet, err := net.ParseCIDR(entry)
			if err != nil {
				rl.logger.Warn().Str("entry", entry).Err(err).Msg("invalid CIDR in whitelist, skipping")
				continue
			}
			rl.whitelist = append(rl.whitelist, ipNet)
			continue
		}

		// Single IP
		ip := net.ParseIP(entry)
		if ip == nil {
			rl.logger.Warn().Str("entry", entry).Msg("invalid IP in whitelist, skipping")
			continue
		}
		rl.whitelistIPs = append(rl.whitelistIPs, ip)
	}
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// Middleware returns an http.Handler that enforces sliding-window rate limits
// backed by Redis sorted sets.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := getClientIP(r)

		// Whitelist bypass.
		if rl.isWhitelisted(clientIP) {
			next.ServeHTTP(w, r)
			return
		}

		endpoint := normalizeEndpoint(r.URL.Path)
		limit := rl.getLimitForEndpoint(endpoint)

		allowed, remaining, resetAt, err := rl.checkRateLimit(r.Context(), endpoint, clientIP, limit)
		if err != nil {
			// On Redis errors fall-open: allow the request but log the issue.
			rl.logger.Error().Err(err).Str("client_ip", clientIP).Msg("rate limit check failed, allowing request")
			next.ServeHTTP(w, r)
			return
		}

		// Always set informational headers.
		setRateLimitHeaders(w, limit, remaining, resetAt)

		if !allowed {
			metrics.RateLimitHitsTotal.WithLabelValues().Inc()
			retryAfter := int(time.Until(resetAt).Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))

			respondJSON(w, http.StatusTooManyRequests, RateLimitErrorResponse{
				Error:     "Rate limit exceeded",
				Code:      "RATE_LIMIT",
				Limit:     limit,
				Remaining: 0,
				ResetAt:   resetAt.UTC().Format(time.RFC3339),
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// Sliding-window algorithm (Redis sorted set)
// ---------------------------------------------------------------------------

// checkRateLimit performs the sliding-window check and returns whether the
// request is allowed, how many requests remain, and when the window resets.
func (rl *RateLimiter) checkRateLimit(ctx context.Context, endpoint, clientID string, limit int) (allowed bool, remaining int, resetAt time.Time, err error) {
	now := time.Now()
	windowStart := now.Add(-60 * time.Second)
	resetAt = now.Add(60 * time.Second)
	key := fmt.Sprintf("ratelimit:%s:%s", endpoint, clientID)

	// Use a pipeline to perform all three operations atomically.
	pipe := rl.redis.Pipeline()

	// 1. Remove entries older than the window.
	pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", windowStart.UnixNano()))

	// 2. Count current entries inside the window.
	countCmd := pipe.ZCard(ctx, key)

	// Execute the pipeline so we can read the count before deciding to add.
	if _, err = pipe.Exec(ctx); err != nil {
		return false, 0, resetAt, fmt.Errorf("rate limit pipeline exec: %w", err)
	}

	count := int(countCmd.Val())

	if count >= limit {
		return false, 0, resetAt, nil
	}

	// 3. Add the current request and set TTL.
	requestID := uuid.New().String()
	addPipe := rl.redis.Pipeline()
	addPipe.ZAdd(ctx, key, redis.Z{
		Score:  float64(now.UnixNano()),
		Member: requestID,
	})
	addPipe.Expire(ctx, key, 70*time.Second) // slightly longer than window
	if _, err = addPipe.Exec(ctx); err != nil {
		return false, 0, resetAt, fmt.Errorf("rate limit add pipeline exec: %w", err)
	}

	remaining = limit - count - 1
	if remaining < 0 {
		remaining = 0
	}
	return true, remaining, resetAt, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// getLimitForEndpoint returns the per-minute request limit for a given
// endpoint path.  Falls back to the default limit.
func (rl *RateLimiter) getLimitForEndpoint(endpoint string) int {
	if l, ok := rl.limits[endpoint]; ok {
		return l
	}

	// Try prefix match for sub-paths (e.g. /api/*)
	for pattern, l := range rl.limits {
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(endpoint, prefix) {
				return l
			}
		}
	}

	return rl.defaultLimit
}

// getClientIP extracts the real client IP from the request, honouring
// reverse-proxy headers.
func getClientIP(r *http.Request) string {
	// 1. X-Forwarded-For (may contain a chain: "client, proxy1, proxy2")
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		ip := strings.TrimSpace(parts[0])
		if isValidIP(ip) {
			return ip
		}
	}

	// 2. X-Real-IP
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		ip := strings.TrimSpace(xrip)
		if isValidIP(ip) {
			return ip
		}
	}

	// 3. RemoteAddr (host:port)
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr // fallback – may include port
	}
	return host
}

// isValidIP returns true if s is a valid IPv4 or IPv6 address.
func isValidIP(s string) bool {
	return net.ParseIP(s) != nil
}

// isWhitelisted checks whether the given IP is covered by any whitelist
// entry (exact match or CIDR).
func (rl *RateLimiter) isWhitelisted(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	// Exact IP match
	for _, wip := range rl.whitelistIPs {
		if wip.Equal(ip) {
			return true
		}
	}

	// CIDR match
	for _, cidr := range rl.whitelist {
		if cidr.Contains(ip) {
			return true
		}
	}

	return false
}

// setRateLimitHeaders writes the standard X-RateLimit-* headers.
func setRateLimitHeaders(w http.ResponseWriter, limit, remaining int, resetAt time.Time) {
	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
	w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
	w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt.Unix()))
}

// ---------------------------------------------------------------------------
// Rate limit error response
// ---------------------------------------------------------------------------

// RateLimitErrorResponse is the JSON body returned when a client exceeds
// the rate limit.
type RateLimitErrorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	Limit     int    `json:"limit"`
	Remaining int    `json:"remaining"`
	ResetAt   string `json:"reset_at"`
}

// respondRateLimitError is a convenience wrapper (used by tests).
func respondRateLimitError(w http.ResponseWriter, limit int, resetAt time.Time) {
	retryAfter := int(time.Until(resetAt).Seconds())
	if retryAfter < 1 {
		retryAfter = 1
	}
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))

	data, _ := json.Marshal(RateLimitErrorResponse{
		Error:     "Rate limit exceeded",
		Code:      "RATE_LIMIT",
		Limit:     limit,
		Remaining: 0,
		ResetAt:   resetAt.UTC().Format(time.RFC3339),
	})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusTooManyRequests)
	w.Write(data)
}
