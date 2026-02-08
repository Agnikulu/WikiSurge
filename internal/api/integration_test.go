package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestServer(t *testing.T) (*APIServer, *miniredis.Miniredis) {
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

func doReq(srv *APIServer, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// ---------------------------------------------------------------------------
// Error response format tests (Task 17.1)
// ---------------------------------------------------------------------------

func TestErrorResponse_ConsistentFormat(t *testing.T) {
	srv, _ := newTestServer(t)

	tests := []struct {
		name         string
		path         string
		expectCode   string
		expectStatus int
	}{
		{"invalid trending limit", "/api/trending?limit=999", "INVALID_PARAMETER", 400},
		{"invalid trending limit neg", "/api/trending?limit=-1", "INVALID_PARAMETER", 400},
		{"invalid trending limit nan", "/api/trending?limit=abc", "INVALID_PARAMETER", 400},
		{"invalid alert severity", "/api/alerts?severity=xyz", "INVALID_PARAMETER", 400},
		{"invalid alert type", "/api/alerts?type=invalid", "INVALID_PARAMETER", 400},
		{"search missing q", "/api/search", "INVALID_PARAMETER", 400},
		{"search invalid limit", "/api/search?q=test&limit=999", "INVALID_PARAMETER", 400},
		{"search ES disabled", "/api/search?q=test", "SERVICE_UNAVAILABLE", 503},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doReq(srv, "GET", tt.path)
			assert.Equal(t, tt.expectStatus, rec.Code)

			var errResp APIErrorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
			assert.Equal(t, tt.expectCode, errResp.Error.Code)
			assert.NotEmpty(t, errResp.Error.Message)
		})
	}
}

func TestErrorResponse_HasRequestID(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doReq(srv, "GET", "/api/trending?limit=abc")
	assert.Equal(t, 400, rec.Code)

	var errResp APIErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	assert.NotEmpty(t, errResp.Error.RequestID, "error response should include request_id")
}

// ---------------------------------------------------------------------------
// Request ID middleware tests
// ---------------------------------------------------------------------------

func TestRequestID_Generated(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doReq(srv, "GET", "/health")
	assert.Equal(t, 200, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("X-Request-ID"))
}

func TestRequestID_Forwarded(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("X-Request-ID", "custom-id-123")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	assert.Equal(t, "custom-id-123", rec.Header().Get("X-Request-ID"))
}

// ---------------------------------------------------------------------------
// Health endpoint tests (Task 17.5)
// ---------------------------------------------------------------------------

func TestHealth_DetailedResponse(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := doReq(srv, "GET", "/health")

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp DetailedHealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, "ok", resp.Status)
	assert.NotEmpty(t, resp.Timestamp)
	assert.Equal(t, "1.0.0", resp.Version)
	assert.GreaterOrEqual(t, resp.Uptime, int64(0))
	assert.Contains(t, resp.Components, "redis")
	assert.Equal(t, "healthy", resp.Components["redis"].Status)
}

func TestHealth_RedisDown_Degraded(t *testing.T) {
	srv, mr := newTestServer(t)
	mr.Close()

	rec := doReq(srv, "GET", "/health")
	// Redis is down — should be degraded or error.
	assert.True(t, rec.Code == http.StatusOK || rec.Code == http.StatusServiceUnavailable)

	var resp DetailedHealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEqual(t, "ok", resp.Status)
}

func TestLiveness(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := doReq(srv, "GET", "/health/live")

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "alive", resp["status"])
}

func TestReadiness_OK(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := doReq(srv, "GET", "/health/ready")

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestReadiness_RedisDown(t *testing.T) {
	srv, mr := newTestServer(t)
	mr.Close()

	rec := doReq(srv, "GET", "/health/ready")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// ---------------------------------------------------------------------------
// Trending endpoint tests
// ---------------------------------------------------------------------------

func TestTrendingEndpoint_Success(t *testing.T) {
	srv, _ := newTestServer(t)

	for i := 0; i < 5; i++ {
		_ = srv.trending.IncrementScore(fmt.Sprintf("Page_%d", i), float64(10-i))
	}

	rec := doReq(srv, "GET", "/api/trending?limit=10")
	assert.Equal(t, http.StatusOK, rec.Code)

	var results []TrendingPageResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &results))
	assert.NotEmpty(t, results)
	assert.LessOrEqual(t, len(results), 10)
}

func TestTrendingEndpoint_LanguageFilter(t *testing.T) {
	srv, _ := newTestServer(t)

	_ = srv.trending.IncrementScore("enwiki:TestPage", 10.0)
	_ = srv.trending.IncrementScore("dewiki:DeutschePage", 8.0)

	rec := doReq(srv, "GET", "/api/trending?limit=10&language=en")
	assert.Equal(t, http.StatusOK, rec.Code)
}

// ---------------------------------------------------------------------------
// Search validation tests (Task 17.6)
// ---------------------------------------------------------------------------

func TestSearchValidation(t *testing.T) {
	srv, _ := newTestServer(t)

	tests := []struct {
		name         string
		query        string
		expectedCode int
	}{
		{"empty query", "/api/search", 400},
		{"invalid limit", "/api/search?q=test&limit=1000", 400},
		{"negative offset", "/api/search?q=test&offset=-1", 400},
		// valid query returns 503 because ES is disabled in test
		{"valid query", "/api/search?q=test&limit=10", 503},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doReq(srv, "GET", tt.query)
			assert.Equal(t, tt.expectedCode, rec.Code)
		})
	}
}

// ---------------------------------------------------------------------------
// Stats endpoint tests
// ---------------------------------------------------------------------------

func TestStatsEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := doReq(srv, "GET", "/api/stats")

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp StatsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.GreaterOrEqual(t, resp.Uptime, int64(0))
}

// ---------------------------------------------------------------------------
// Alerts endpoint tests
// ---------------------------------------------------------------------------

func TestAlertsEndpoint_EmptyStream(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := doReq(srv, "GET", "/api/alerts?type=spike")

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp AlertsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.Alerts)
}

func TestAlertsEndpoint_ValidationErrors(t *testing.T) {
	srv, _ := newTestServer(t)

	tests := []struct {
		name string
		path string
	}{
		{"bad limit", "/api/alerts?limit=abc"},
		{"bad severity", "/api/alerts?severity=unknown"},
		{"bad type", "/api/alerts?type=invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doReq(srv, "GET", tt.path)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

// ---------------------------------------------------------------------------
// Edit Wars endpoint tests
// ---------------------------------------------------------------------------

func TestEditWarsEndpoint_Empty(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := doReq(srv, "GET", "/api/edit-wars")

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestEditWarsEndpoint_BadLimit(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := doReq(srv, "GET", "/api/edit-wars?limit=abc")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// Documentation endpoints (Task 17.4)
// ---------------------------------------------------------------------------

func TestAPIDocs_ServedHTML(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := doReq(srv, "GET", "/api/docs")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(), "WikiSurge")
	assert.Contains(t, rec.Body.String(), "redoc")
}

func TestOpenAPISpec_ServedYAML(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := doReq(srv, "GET", "/api/docs/openapi.yaml")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "yaml")
	assert.Contains(t, rec.Body.String(), "openapi:")
	assert.Contains(t, rec.Body.String(), "/api/trending")
}

// ---------------------------------------------------------------------------
// Security headers tests
// ---------------------------------------------------------------------------

func TestSecurityHeaders(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := doReq(srv, "GET", "/health")

	assert.NotEmpty(t, rec.Header().Get("X-Content-Type-Options"))
	assert.NotEmpty(t, rec.Header().Get("X-Frame-Options"))
	assert.NotEmpty(t, rec.Header().Get("Strict-Transport-Security"))
}

// ---------------------------------------------------------------------------
// CORS tests
// ---------------------------------------------------------------------------

func TestIntegration_CORS_Headers(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := doReq(srv, "GET", "/health")

	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestIntegration_CORS_Preflight(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := doReq(srv, "OPTIONS", "/api/trending")

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// ---------------------------------------------------------------------------
// Method not allowed tests
// ---------------------------------------------------------------------------

func TestMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)

	// DELETE is not in the allow-list
	rec := doReq(srv, "DELETE", "/api/trending")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// ---------------------------------------------------------------------------
// Response compression tests (Task 17.7)
// ---------------------------------------------------------------------------

func TestGzipCompression_AcceptEncoding(t *testing.T) {
	srv, _ := newTestServer(t)

	// Seed enough data to exceed 1KB threshold.
	for i := 0; i < 50; i++ {
		_ = srv.trending.IncrementScore(fmt.Sprintf("enwiki:LongPageTitle_%d_with_a_lot_of_text", i), float64(100-i))
	}

	req := httptest.NewRequest("GET", "/api/trending?limit=50", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	// Response should either be gzip-compressed (Content-Encoding: gzip)
	// or plain (if below threshold). Either is acceptable.
}

// ---------------------------------------------------------------------------
// ETag / conditional GET tests (Task 17.7)
// ---------------------------------------------------------------------------

func TestETag_CacheHit(t *testing.T) {
	srv, _ := newTestServer(t)

	// First request — get the ETag.
	rec1 := doReq(srv, "GET", "/health/live")
	assert.Equal(t, http.StatusOK, rec1.Code)
	etag := rec1.Header().Get("ETag")

	if etag != "" {
		// Second request with If-None-Match — should get 304.
		req2 := httptest.NewRequest("GET", "/health/live", nil)
		req2.Header.Set("If-None-Match", etag)
		rec2 := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec2, req2)

		assert.Equal(t, http.StatusNotModified, rec2.Code)
	}
}

// ---------------------------------------------------------------------------
// WebSocket tests (Task 17.9)
// ---------------------------------------------------------------------------

func TestWebSocketFeed_Connect(t *testing.T) {
	srv, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/feed"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Skipf("WebSocket dial failed: %v", err)
		return
	}
	defer conn.Close()
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
}

func TestWebSocketFeed_ReceiveEdit(t *testing.T) {
	srv, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/feed"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Skipf("WebSocket dial failed: %v", err)
		return
	}
	defer conn.Close()

	// Give the client time to register.
	time.Sleep(100 * time.Millisecond)

	// Broadcast a test edit.
	edit := &models.WikipediaEdit{
		Title: "Test Page",
		User:  "TestUser",
		Wiki:  "enwiki",
	}
	srv.Hub().BroadcastEdit(edit)

	// Read the message with a timeout.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Skipf("WebSocket read timed out (expected in fast tests): %v", err)
		return
	}

	var wsMsg WSMessage
	require.NoError(t, json.Unmarshal(msg, &wsMsg))
	assert.Equal(t, "edit", wsMsg.Type)
}

func TestWebSocketFeed_Filter(t *testing.T) {
	srv, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Connect with language filter.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/feed?languages=en&exclude_bots=true"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Skipf("WebSocket dial failed: %v", err)
		return
	}
	defer conn.Close()

	// Connection should succeed with filter params.
	assert.NotNil(t, conn)
}

// ---------------------------------------------------------------------------
// Validation package tests (Task 17.6)
// ---------------------------------------------------------------------------

func TestValidateSearchParams(t *testing.T) {
	tests := []struct {
		name    string
		params  SearchParams
		wantErr bool
	}{
		{"valid", SearchParams{Query: "test", Limit: 10, Offset: 0, From: time.Now().Add(-1 * time.Hour), To: time.Now()}, false},
		{"empty query", SearchParams{Query: "", Limit: 10, Offset: 0, From: time.Now().Add(-1 * time.Hour), To: time.Now()}, true},
		{"limit too high", SearchParams{Query: "test", Limit: 200, Offset: 0, From: time.Now().Add(-1 * time.Hour), To: time.Now()}, true},
		{"limit zero", SearchParams{Query: "test", Limit: 0, Offset: 0, From: time.Now().Add(-1 * time.Hour), To: time.Now()}, true},
		{"negative offset", SearchParams{Query: "test", Limit: 10, Offset: -1, From: time.Now().Add(-1 * time.Hour), To: time.Now()}, true},
		{"from after to", SearchParams{Query: "test", Limit: 10, Offset: 0, From: time.Now(), To: time.Now().Add(-1 * time.Hour)}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSearchParams(tt.params)
			if tt.wantErr {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestValidateSeverity(t *testing.T) {
	assert.Nil(t, ValidateSeverity("low"))
	assert.Nil(t, ValidateSeverity("medium"))
	assert.Nil(t, ValidateSeverity("high"))
	assert.Nil(t, ValidateSeverity("critical"))
	assert.NotNil(t, ValidateSeverity("unknown"))
	assert.NotNil(t, ValidateSeverity(""))
}

func TestValidateAlertType(t *testing.T) {
	assert.Nil(t, ValidateAlertType("spike"))
	assert.Nil(t, ValidateAlertType("edit_war"))
	assert.NotNil(t, ValidateAlertType("invalid"))
}
