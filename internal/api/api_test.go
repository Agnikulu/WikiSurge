package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testServer creates a fully wired APIServer backed by miniredis.
func testServer(t *testing.T) (*APIServer, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	cfg := &config.Config{
		Features: config.Features{Trending: true, EditWars: true},
		Redis: config.Redis{
			URL:       fmt.Sprintf("redis://%s", mr.Addr()),
			MaxMemory: "256mb",
			HotPages: config.HotPages{
				MaxTracked: 500, PromotionThreshold: 3, WindowDuration: 15 * time.Minute,
				MaxMembersPerPage: 50, HotThreshold: 2, CleanupInterval: 5 * time.Minute,
			},
			Trending: config.TrendingConfig{
				Enabled: true, MaxPages: 500, HalfLifeMinutes: 30.0, PruneInterval: 5 * time.Minute,
			},
		},
		Elasticsearch: config.Elasticsearch{Enabled: false},
		API:           config.API{Port: 8080, RateLimit: 10000},
		Logging:       config.Logging{Level: "error", Format: "json"},
	}

	logger := zerolog.Nop()
	hotPages := storage.NewHotPageTracker(redisClient, &cfg.Redis.HotPages)
	trending := storage.NewTrendingScorerForTest(redisClient, &cfg.Redis.Trending)
	alerts := storage.NewRedisAlerts(redisClient)

	srv := NewAPIServer(redisClient, nil, trending, hotPages, alerts, cfg, logger)

	t.Cleanup(func() {
		hotPages.Shutdown()
		trending.Stop()
		redisClient.Close()
		mr.Close()
	})

	return srv, mr
}

// doRequest is a helper that fires an HTTP request against the server's raw router.
func doRequest(srv *APIServer, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// ---------------------------------------------------------------------------
// Health endpoint
// ---------------------------------------------------------------------------

func TestHealth_OK(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/health")

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp HealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, "connected", resp.Redis)
	assert.Greater(t, resp.Uptime, int64(-1))
}

func TestHealth_RedisDown(t *testing.T) {
	srv, mr := testServer(t)
	mr.Close() // kill Redis

	rec := doRequest(srv, "GET", "/health")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var resp HealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Contains(t, resp.Status, "degraded")
}

// ---------------------------------------------------------------------------
// Trending endpoint
// ---------------------------------------------------------------------------

func TestTrending_Empty(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/trending")

	assert.Equal(t, http.StatusOK, rec.Code)

	var results []TrendingPageResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &results))
	assert.Empty(t, results)
}

func TestTrending_WithData(t *testing.T) {
	srv, _ := testServer(t)

	// Seed trending data
	for i := 0; i < 5; i++ {
		_ = srv.trending.IncrementScore(fmt.Sprintf("Page_%d", i), float64(10-i))
	}

	rec := doRequest(srv, "GET", "/api/trending?limit=3")
	assert.Equal(t, http.StatusOK, rec.Code)

	var results []TrendingPageResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &results))
	assert.Len(t, results, 3)
	assert.Equal(t, 1, results[0].Rank)
	assert.GreaterOrEqual(t, results[0].Score, results[1].Score)
}

func TestTrending_InvalidLimit(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/trending?limit=999")

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	assert.Equal(t, "INVALID_PARAM", errResp.Code)
}

func TestTrending_NegativeLimit(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/trending?limit=-5")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTrending_NonNumericLimit(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/trending?limit=abc")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// Stats endpoint
// ---------------------------------------------------------------------------

func TestStats_OK(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/stats")

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp StatsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.GreaterOrEqual(t, resp.Uptime, int64(0))
	assert.NotNil(t, resp.TopLanguages)
}

func TestStats_Cached(t *testing.T) {
	srv, _ := testServer(t)

	// First call populates cache
	rec1 := doRequest(srv, "GET", "/api/stats")
	assert.Equal(t, http.StatusOK, rec1.Code)

	// Second call should hit cache (same result)
	rec2 := doRequest(srv, "GET", "/api/stats")
	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, rec1.Body.String(), rec2.Body.String())
}

// ---------------------------------------------------------------------------
// Alerts endpoint
// ---------------------------------------------------------------------------

func TestAlerts_EmptyStream(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/alerts?type=spikes")

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAlerts_InvalidType(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/alerts?type=bogus")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAlerts_WithData(t *testing.T) {
	srv, _ := testServer(t)

	ctx := context.Background()
	_ = srv.alerts.PublishSpikeAlert(ctx, "enwiki", "TestPage", 3.5, 42)

	rec := doRequest(srv, "GET", "/api/alerts?type=spikes&limit=5")
	assert.Equal(t, http.StatusOK, rec.Code)

	var alerts []storage.Alert
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &alerts))
	assert.Len(t, alerts, 1)
	assert.Equal(t, "spike", alerts[0].Type)
}

// ---------------------------------------------------------------------------
// Edit Wars endpoint
// ---------------------------------------------------------------------------

func TestEditWars_Empty(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/edit-wars")

	assert.Equal(t, http.StatusOK, rec.Code)
}

// ---------------------------------------------------------------------------
// Search endpoint
// ---------------------------------------------------------------------------

func TestSearch_MissingQuery(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/search")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSearch_ESDisabled(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/search?q=test")

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

func TestCORS_Headers(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/health")

	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "GET")
}

func TestCORS_Preflight(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "OPTIONS", "/api/trending")

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestRecovery_PanicHandled(t *testing.T) {
	logger := zerolog.Nop()

	// Create a handler that panics
	panicker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})
	handler := RecoveryMiddleware(logger, panicker)

	req := httptest.NewRequest("GET", "/panic", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	assert.Equal(t, "INTERNAL_ERROR", errResp.Code)
}

func TestRateLimiter_AllowsNormal(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RateLimitMiddleware(100, inner)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// ---------------------------------------------------------------------------
// Query-param helpers
// ---------------------------------------------------------------------------

func TestParseIntQuery_Default(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	v, err := parseIntQuery(req, "limit", 20, 100)
	require.NoError(t, err)
	assert.Equal(t, 20, v)
}

func TestParseIntQuery_Valid(t *testing.T) {
	req := httptest.NewRequest("GET", "/?limit=50", nil)
	v, err := parseIntQuery(req, "limit", 20, 100)
	require.NoError(t, err)
	assert.Equal(t, 50, v)
}

func TestParseIntQuery_ExceedsMax(t *testing.T) {
	req := httptest.NewRequest("GET", "/?limit=200", nil)
	_, err := parseIntQuery(req, "limit", 20, 100)
	assert.Error(t, err)
}

func TestParseIntQuery_Negative(t *testing.T) {
	req := httptest.NewRequest("GET", "/?limit=-1", nil)
	_, err := parseIntQuery(req, "limit", 20, 100)
	assert.Error(t, err)
}

func TestParseTimeQuery_RFC3339(t *testing.T) {
	req := httptest.NewRequest("GET", "/?since=2024-01-01T00:00:00Z", nil)
	v, err := parseTimeQuery(req, "since", time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 2024, v.Year())
}

func TestParseTimeQuery_Unix(t *testing.T) {
	req := httptest.NewRequest("GET", "/?since=1704067200", nil)
	v, err := parseTimeQuery(req, "since", time.Time{})
	require.NoError(t, err)
	assert.Equal(t, int64(1704067200), v.Unix())
}

// ---------------------------------------------------------------------------
// JSON response helpers
// ---------------------------------------------------------------------------

func TestRespondJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	respondJSON(rec, http.StatusOK, map[string]string{"hello": "world"})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, rec.Body.String(), `"hello":"world"`)
}

func TestRespondError(t *testing.T) {
	rec := httptest.NewRecorder()
	respondError(rec, http.StatusBadRequest, "bad", "BAD_REQ")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	assert.Equal(t, "BAD_REQ", errResp.Code)
}

// ---------------------------------------------------------------------------
// Language extraction
// ---------------------------------------------------------------------------

func TestExtractLanguage(t *testing.T) {
	assert.Equal(t, "en", extractLanguage("enwiki:Main_Page"))
	assert.Equal(t, "es", extractLanguage("eswiki:Portada"))
	assert.Equal(t, "", extractLanguage("Main_Page"))
}
