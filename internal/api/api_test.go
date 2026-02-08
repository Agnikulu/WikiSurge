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

	var resp DetailedHealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp.Status)
	assert.Greater(t, resp.Uptime, int64(-1))
	assert.NotEmpty(t, resp.Components)
	if redis, ok := resp.Components["redis"]; ok {
		assert.Equal(t, "healthy", redis.Status)
	}
}

func TestHealth_RedisDown(t *testing.T) {
	srv, mr := testServer(t)
	mr.Close() // kill Redis

	rec := doRequest(srv, "GET", "/health")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var resp DetailedHealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Contains(t, resp.Status, "error")
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

	var errResp APIErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	assert.Equal(t, "INVALID_PARAMETER", errResp.Error.Code)
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
	rec := doRequest(srv, "GET", "/api/alerts?type=spike")

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp AlertsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.Alerts)
	assert.Equal(t, 0, resp.Total)
}

func TestAlerts_DefaultType_BothStreams(t *testing.T) {
	srv, _ := testServer(t)
	ctx := context.Background()

	// Publish spike and edit war alerts
	_ = srv.alerts.PublishSpikeAlert(ctx, "enwiki", "SpikePage", 5.0, 50)
	_ = srv.alerts.PublishEditWarAlert(ctx, "enwiki", "WarPage", []string{"A", "B"}, 100)

	// No type param → queries both streams
	rec := doRequest(srv, "GET", "/api/alerts")
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp AlertsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.GreaterOrEqual(t, resp.Total, 2)
}

func TestAlerts_InvalidType(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/alerts?type=bogus")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAlerts_WithSpikeData(t *testing.T) {
	srv, _ := testServer(t)

	ctx := context.Background()
	_ = srv.alerts.PublishSpikeAlert(ctx, "enwiki", "TestPage", 3.5, 42)

	rec := doRequest(srv, "GET", "/api/alerts?type=spike&limit=5")
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp AlertsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.Alerts, 1)
	assert.Equal(t, "spike", resp.Alerts[0].Type)
	assert.Equal(t, "TestPage", resp.Alerts[0].PageTitle)
	assert.Equal(t, 3.5, resp.Alerts[0].SpikeRatio)
}

func TestAlerts_SeverityFilter(t *testing.T) {
	srv, _ := testServer(t)
	ctx := context.Background()

	// Low severity (ratio=1.5), medium (ratio=3.0), high (ratio=6.0)
	_ = srv.alerts.PublishSpikeAlert(ctx, "enwiki", "LowPage", 1.5, 10)
	_ = srv.alerts.PublishSpikeAlert(ctx, "enwiki", "MediumPage", 3.0, 20)
	_ = srv.alerts.PublishSpikeAlert(ctx, "enwiki", "HighPage", 6.0, 50)

	rec := doRequest(srv, "GET", "/api/alerts?type=spike&severity=high")
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp AlertsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// Only the high severity alert (ratio >= 5) should appear
	for _, a := range resp.Alerts {
		assert.Equal(t, "high", a.Severity)
	}
}

func TestAlerts_InvalidSeverity(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/alerts?severity=extreme")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAlerts_InvalidLimit(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/alerts?limit=abc")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAlerts_Pagination(t *testing.T) {
	srv, _ := testServer(t)
	ctx := context.Background()

	// Publish 5 alerts
	for i := 0; i < 5; i++ {
		_ = srv.alerts.PublishSpikeAlert(ctx, "enwiki", fmt.Sprintf("Page_%d", i), float64(i+1), i*10)
	}

	// Fetch page 1 (limit=2, offset=0)
	rec := doRequest(srv, "GET", "/api/alerts?type=spike&limit=2&offset=0")
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp AlertsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.Alerts, 2)
	assert.True(t, resp.Pagination.HasMore)
	assert.Equal(t, int64(5), resp.Pagination.Total)

	// Fetch page 2 (limit=2, offset=2)
	rec2 := doRequest(srv, "GET", "/api/alerts?type=spike&limit=2&offset=2")
	assert.Equal(t, http.StatusOK, rec2.Code)

	var resp2 AlertsResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.Len(t, resp2.Alerts, 2)
	assert.True(t, resp2.Pagination.HasMore)
}

func TestAlerts_CacheHeaders(t *testing.T) {
	srv, _ := testServer(t)

	// First request = MISS
	rec1 := doRequest(srv, "GET", "/api/alerts?type=spike")
	assert.Equal(t, "MISS", rec1.Header().Get("X-Cache"))
	assert.Equal(t, "max-age=5", rec1.Header().Get("Cache-Control"))

	// Second identical request = HIT
	rec2 := doRequest(srv, "GET", "/api/alerts?type=spike")
	assert.Equal(t, "HIT", rec2.Header().Get("X-Cache"))
}

func TestAlerts_SinceFilter(t *testing.T) {
	srv, _ := testServer(t)
	ctx := context.Background()

	_ = srv.alerts.PublishSpikeAlert(ctx, "enwiki", "RecentPage", 5.0, 50)

	// Use 'since' set to 1 hour ago — should include the alert
	since := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	rec := doRequest(srv, "GET", "/api/alerts?type=spike&since="+since)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp AlertsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.GreaterOrEqual(t, resp.Total, 1)
}

func TestAlerts_InvalidSince(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/alerts?since=not-a-date")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// Edit Wars endpoint
// ---------------------------------------------------------------------------

func TestEditWars_Empty(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/edit-wars")

	assert.Equal(t, http.StatusOK, rec.Code)

	var results []EditWarEntry
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &results))
	assert.Empty(t, results)
}

func TestEditWars_ActiveWithData(t *testing.T) {
	srv, mr := testServer(t)
	_ = mr // used indirectly through server's redis client

	ctx := context.Background()

	// Simulate an active edit war by creating the Redis keys the detector would set
	pageTitle := "Controversial_Topic"

	// Set the marker key
	srv.redis.Set(ctx, fmt.Sprintf("editwar:%s", pageTitle), "1", time.Hour)

	// Set editor tracking hash
	editorsKey := fmt.Sprintf("editwar:editors:%s", pageTitle)
	srv.redis.HSet(ctx, editorsKey, "UserA", "5")
	srv.redis.HSet(ctx, editorsKey, "UserB", "3")
	srv.redis.HSet(ctx, editorsKey, "UserC", "4")
	srv.redis.Expire(ctx, editorsKey, time.Hour)

	rec := doRequest(srv, "GET", "/api/edit-wars?active=true")
	assert.Equal(t, http.StatusOK, rec.Code)

	var results []EditWarEntry
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &results))
	assert.Len(t, results, 1)
	assert.Equal(t, pageTitle, results[0].PageTitle)
	assert.Equal(t, 3, results[0].EditorCount)
	assert.True(t, results[0].Active)
	assert.Len(t, results[0].Editors, 3)
}

func TestEditWars_Historical(t *testing.T) {
	srv, _ := testServer(t)
	ctx := context.Background()

	// Publish an edit war alert to the stream
	_ = srv.alerts.PublishEditWarAlert(ctx, "enwiki", "WarPage", []string{"A", "B"}, 100)

	rec := doRequest(srv, "GET", "/api/edit-wars?active=false")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestEditWars_InvalidLimit(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/edit-wars?limit=abc")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// Search endpoint
// ---------------------------------------------------------------------------

func TestSearch_MissingQuery(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/search")

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var errResp APIErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	assert.Equal(t, "INVALID_PARAMETER", errResp.Error.Code)
	assert.Contains(t, errResp.Error.Message, "required")
}

func TestSearch_ESDisabled(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/search?q=test")

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestSearch_InvalidLimit(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/search?q=test&limit=1000")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSearch_InvalidOffset(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/search?q=test&offset=abc")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSearch_InvalidFromTimestamp(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/search?q=test&from=bad-date")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSearch_InvalidToTimestamp(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/search?q=test&to=bad-date")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSearch_FromAfterTo(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/search?q=test&from=2025-01-01T00:00:00Z&to=2024-01-01T00:00:00Z")

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var errResp APIErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	assert.Contains(t, errResp.Error.Message, "before")
}

func TestSearch_NegativeLimit(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/search?q=test&limit=-5")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// Search query builder (unit tests)
// ---------------------------------------------------------------------------

func TestBuildSearchQuery_Basic(t *testing.T) {
	srv, _ := testServer(t)
	from := time.Now().Add(-24 * time.Hour)
	to := time.Now()

	q := srv.buildSearchQuery("election", from, to, 50, 0, "", "")

	// Verify top-level keys
	assert.Equal(t, 50, q["size"])
	assert.Equal(t, 0, q["from"])
	assert.NotNil(t, q["query"])
	assert.NotNil(t, q["sort"])
	assert.NotNil(t, q["highlight"])
}

func TestBuildSearchQuery_WithLanguageFilter(t *testing.T) {
	srv, _ := testServer(t)
	from := time.Now().Add(-24 * time.Hour)
	to := time.Now()

	q := srv.buildSearchQuery("test", from, to, 10, 0, "en", "")

	boolQuery := q["query"].(map[string]interface{})["bool"].(map[string]interface{})
	filters := boolQuery["filter"].([]interface{})
	// Should have at least: range filter + language filter
	assert.GreaterOrEqual(t, len(filters), 2)
}

func TestBuildSearchQuery_WithBotFilter(t *testing.T) {
	srv, _ := testServer(t)
	from := time.Now().Add(-24 * time.Hour)
	to := time.Now()

	q := srv.buildSearchQuery("test", from, to, 10, 0, "", "false")

	boolQuery := q["query"].(map[string]interface{})["bool"].(map[string]interface{})
	filters := boolQuery["filter"].([]interface{})
	// Should have at least: range filter + bot filter
	assert.GreaterOrEqual(t, len(filters), 2)
}

func TestBuildSearchQuery_PhraseMatch(t *testing.T) {
	srv, _ := testServer(t)
	from := time.Now().Add(-24 * time.Hour)
	to := time.Now()

	q := srv.buildSearchQuery("\"exact phrase\"", from, to, 10, 0, "", "")

	boolQuery := q["query"].(map[string]interface{})["bool"].(map[string]interface{})
	must := boolQuery["must"].([]interface{})
	multiMatch := must[0].(map[string]interface{})["multi_match"].(map[string]interface{})
	assert.Equal(t, "phrase", multiMatch["type"])
	assert.Equal(t, "exact phrase", multiMatch["query"])
}

func TestBuildSearchQuery_Offset(t *testing.T) {
	srv, _ := testServer(t)
	from := time.Now().Add(-24 * time.Hour)
	to := time.Now()

	q := srv.buildSearchQuery("test", from, to, 10, 20, "", "")
	assert.Equal(t, 10, q["size"])
	assert.Equal(t, 20, q["from"])
}

// ---------------------------------------------------------------------------
// Search response parser (unit tests)
// ---------------------------------------------------------------------------

func TestParseSearchResponse_Empty(t *testing.T) {
	srv, _ := testServer(t)

	esResult := map[string]interface{}{
		"hits": map[string]interface{}{
			"total": map[string]interface{}{
				"value": float64(0),
			},
			"hits": []interface{}{},
		},
	}

	resp := srv.parseSearchResponse(esResult, "test", 50, 0)
	assert.Equal(t, int64(0), resp.Total)
	assert.Empty(t, resp.Hits)
	assert.Equal(t, "test", resp.Query)
	assert.False(t, resp.Pagination.HasMore)
}

func TestParseSearchResponse_WithHits(t *testing.T) {
	srv, _ := testServer(t)

	esResult := map[string]interface{}{
		"hits": map[string]interface{}{
			"total": map[string]interface{}{
				"value": float64(100),
			},
			"hits": []interface{}{
				map[string]interface{}{
					"_score": float64(4.5),
					"_source": map[string]interface{}{
						"title":       "Test Article",
						"user":        "TestUser",
						"comment":     "Updated info",
						"wiki":        "enwiki",
						"language":    "en",
						"timestamp":   "2024-01-15T12:00:00.000Z",
						"byte_change": float64(200),
					},
				},
				map[string]interface{}{
					"_score": float64(3.2),
					"_source": map[string]interface{}{
						"title":       "Another Article",
						"user":        "OtherUser",
						"comment":     "Minor edit",
						"wiki":        "enwiki",
						"language":    "en",
						"timestamp":   "2024-01-15T11:00:00.000Z",
						"byte_change": float64(-50),
					},
				},
			},
		},
	}

	resp := srv.parseSearchResponse(esResult, "test query", 50, 0)
	assert.Equal(t, int64(100), resp.Total)
	assert.Len(t, resp.Hits, 2)
	assert.Equal(t, "test query", resp.Query)
	assert.True(t, resp.Pagination.HasMore)

	// Verify first hit
	assert.Equal(t, "Test Article", resp.Hits[0].Title)
	assert.Equal(t, "TestUser", resp.Hits[0].User)
	assert.Equal(t, 4.5, resp.Hits[0].Score)
	assert.Equal(t, 200, resp.Hits[0].ByteChange)
	assert.Equal(t, "en", resp.Hits[0].Language)
}

func TestParseSearchResponse_Pagination(t *testing.T) {
	srv, _ := testServer(t)

	esResult := map[string]interface{}{
		"hits": map[string]interface{}{
			"total": map[string]interface{}{
				"value": float64(100),
			},
			"hits": []interface{}{},
		},
	}

	resp := srv.parseSearchResponse(esResult, "q", 10, 90)
	assert.Equal(t, int64(100), resp.Pagination.Total)
	assert.Equal(t, 10, resp.Pagination.Limit)
	assert.Equal(t, 90, resp.Pagination.Offset)
	assert.False(t, resp.Pagination.HasMore) // 90+10 = 100, no more

	resp2 := srv.parseSearchResponse(esResult, "q", 10, 80)
	assert.True(t, resp2.Pagination.HasMore) // 80+10 = 90 < 100
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

	var errResp APIErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	assert.Equal(t, "INTERNAL_ERROR", errResp.Error.Code)
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

// ---------------------------------------------------------------------------
// Response cache
// ---------------------------------------------------------------------------

func TestCache_SetAndGet(t *testing.T) {
	c := newResponseCache()
	defer c.Stop()

	c.Set("key1", []byte(`{"data":"hello"}`), 10*time.Second)

	data, ok := c.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, `{"data":"hello"}`, string(data))
}

func TestCache_Expired(t *testing.T) {
	c := newResponseCache()
	defer c.Stop()

	c.Set("key2", []byte("expired"), 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	_, ok := c.Get("key2")
	assert.False(t, ok)
}

func TestCache_Miss(t *testing.T) {
	c := newResponseCache()
	defer c.Stop()

	_, ok := c.Get("nonexistent")
	assert.False(t, ok)
}

func TestCacheKey_Deterministic(t *testing.T) {
	k1 := cacheKey("search", "test", "2024-01-01")
	k2 := cacheKey("search", "test", "2024-01-01")
	assert.Equal(t, k1, k2)

	k3 := cacheKey("search", "different", "2024-01-01")
	assert.NotEqual(t, k1, k3)
}

// ---------------------------------------------------------------------------
// ParseBoolQuery helper
// ---------------------------------------------------------------------------

func TestParseBoolQuery_True(t *testing.T) {
	req := httptest.NewRequest("GET", "/?active=true", nil)
	v := parseBoolQuery(req, "active", false)
	assert.True(t, v)
}

func TestParseBoolQuery_False(t *testing.T) {
	req := httptest.NewRequest("GET", "/?active=false", nil)
	v := parseBoolQuery(req, "active", true)
	assert.False(t, v)
}

func TestParseBoolQuery_Default(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	v := parseBoolQuery(req, "active", true)
	assert.True(t, v)
}

func TestParseBoolQuery_Invalid(t *testing.T) {
	req := httptest.NewRequest("GET", "/?active=xyz", nil)
	v := parseBoolQuery(req, "active", true)
	assert.True(t, v) // falls back to default
}

// ---------------------------------------------------------------------------
// Storage: DeriveSeverity
// ---------------------------------------------------------------------------

func TestDeriveSeverity_Spike(t *testing.T) {
	tests := []struct {
		ratio    float64
		expected string
	}{
		{1.0, "low"},
		{3.0, "medium"},
		{6.0, "high"},
		{12.0, "critical"},
	}

	for _, tt := range tests {
		alert := storage.Alert{
			Type: storage.AlertTypeSpike,
			Data: map[string]interface{}{"spike_ratio": tt.ratio},
		}
		assert.Equal(t, tt.expected, storage.DeriveSeverity(alert), "ratio=%f", tt.ratio)
	}
}

func TestDeriveSeverity_EditWar(t *testing.T) {
	alert := storage.Alert{
		Type: storage.AlertTypeEditWar,
		Data: map[string]interface{}{"num_editors": float64(5)},
	}
	assert.Equal(t, "high", storage.DeriveSeverity(alert))

	alert.Data["num_editors"] = float64(7)
	assert.Equal(t, "critical", storage.DeriveSeverity(alert))
}
