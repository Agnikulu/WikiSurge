package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/auth"
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// setupUserTestServer creates an APIServer with user store + JWT configured.
func setupUserTestServer(t *testing.T) (*APIServer, *storage.UserStore) {
	t.Helper()

	// Mini-Redis
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rc.Close() })

	// SQLite
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_users.db")
	userStore, err := storage.NewUserStore(dbPath)
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	t.Cleanup(func() { userStore.Close() })

	// JWT service
	jwtSvc := auth.NewJWTService("test-secret-key-for-user-handlers", 1*time.Hour)

	cfg := &config.Config{
		Redis: config.Redis{
			URL: "redis://" + mr.Addr(),
			HotPages: config.HotPages{
				MaxTracked: 100, PromotionThreshold: 3, WindowDuration: 15 * time.Minute,
				MaxMembersPerPage: 50, HotThreshold: 2, CleanupInterval: 5 * time.Minute,
			},
			Trending: config.TrendingConfig{
				Enabled: true, MaxPages: 100, HalfLifeMinutes: 30, PruneInterval: 5 * time.Minute,
			},
		},
		API:     config.API{Port: 8080, RateLimit: 10000},
		Logging: config.Logging{Level: "error", Format: "json"},
		Email:   config.EmailConfig{DashboardURL: "http://localhost:5173"},
	}

	logger := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	srv := NewAPIServer(rc, nil, nil, nil, nil, userStore, jwtSvc, cfg, logger)

	return srv, userStore
}

// doJSON fires a JSON request against the server's Handler().
func doJSON(srv *APIServer, method, path string, body interface{}, token string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// decodeJSON decodes a response body into a map.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode JSON: %v (body was: %s)", err, rec.Body.String())
	}
	return result
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func TestRegister_Success(t *testing.T) {
	srv, _ := setupUserTestServer(t)

	rec := doJSON(srv, "POST", "/api/auth/register", map[string]string{
		"email":    "alice@example.com",
		"password": "securepassword123",
	}, "")

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201. Body: %s", rec.Code, rec.Body.String())
	}

	body := decodeJSON(t, rec)
	if body["token"] == nil || body["token"] == "" {
		t.Error("expected non-empty token in response")
	}

	user := body["user"].(map[string]interface{})
	if user["email"] != "alice@example.com" {
		t.Errorf("email = %v, want alice@example.com", user["email"])
	}
	if user["verified"] != true {
		t.Error("user should be auto-verified in dev")
	}
	if user["digest_frequency"] != "daily" {
		t.Errorf("default digest_frequency = %v, want daily", user["digest_frequency"])
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	srv, _ := setupUserTestServer(t)

	doJSON(srv, "POST", "/api/auth/register", map[string]string{
		"email": "dup@example.com", "password": "password1234",
	}, "")

	rec := doJSON(srv, "POST", "/api/auth/register", map[string]string{
		"email": "dup@example.com", "password": "password5678",
	}, "")

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestRegister_BadEmail(t *testing.T) {
	srv, _ := setupUserTestServer(t)

	rec := doJSON(srv, "POST", "/api/auth/register", map[string]string{
		"email": "notanemail", "password": "password1234",
	}, "")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestRegister_ShortPassword(t *testing.T) {
	srv, _ := setupUserTestServer(t)

	rec := doJSON(srv, "POST", "/api/auth/register", map[string]string{
		"email": "short@example.com", "password": "abc",
	}, "")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

func TestLogin_Success(t *testing.T) {
	srv, _ := setupUserTestServer(t)

	// Register first
	doJSON(srv, "POST", "/api/auth/register", map[string]string{
		"email": "login@example.com", "password": "securepassword123",
	}, "")

	// Login
	rec := doJSON(srv, "POST", "/api/auth/login", map[string]string{
		"email": "login@example.com", "password": "securepassword123",
	}, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. Body: %s", rec.Code, rec.Body.String())
	}

	body := decodeJSON(t, rec)
	if body["token"] == nil || body["token"] == "" {
		t.Error("expected token in login response")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	srv, _ := setupUserTestServer(t)

	doJSON(srv, "POST", "/api/auth/register", map[string]string{
		"email": "wrong@example.com", "password": "correctpassword",
	}, "")

	rec := doJSON(srv, "POST", "/api/auth/login", map[string]string{
		"email": "wrong@example.com", "password": "incorrectpassword",
	}, "")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestLogin_NonexistentUser(t *testing.T) {
	srv, _ := setupUserTestServer(t)

	rec := doJSON(srv, "POST", "/api/auth/login", map[string]string{
		"email": "ghost@example.com", "password": "password1234",
	}, "")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Profile (protected)
// ---------------------------------------------------------------------------

func registerAndLogin(t *testing.T, srv *APIServer, email, password string) string {
	t.Helper()
	doJSON(srv, "POST", "/api/auth/register", map[string]string{
		"email": email, "password": password,
	}, "")
	rec := doJSON(srv, "POST", "/api/auth/login", map[string]string{
		"email": email, "password": password,
	}, "")
	body := decodeJSON(t, rec)
	return body["token"].(string)
}

func TestGetProfile_Success(t *testing.T) {
	srv, _ := setupUserTestServer(t)
	token := registerAndLogin(t, srv, "profile@example.com", "password1234")

	rec := doJSON(srv, "GET", "/api/user/profile", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. Body: %s", rec.Code, rec.Body.String())
	}

	body := decodeJSON(t, rec)
	if body["email"] != "profile@example.com" {
		t.Errorf("email = %v", body["email"])
	}
}

func TestGetProfile_NoAuth(t *testing.T) {
	srv, _ := setupUserTestServer(t)

	rec := doJSON(srv, "GET", "/api/user/profile", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestGetProfile_BadToken(t *testing.T) {
	srv, _ := setupUserTestServer(t)

	rec := doJSON(srv, "GET", "/api/user/profile", nil, "totally-invalid-token")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Preferences
// ---------------------------------------------------------------------------

func TestUpdatePreferences(t *testing.T) {
	srv, _ := setupUserTestServer(t)
	token := registerAndLogin(t, srv, "prefs@example.com", "password1234")

	// Update
	rec := doJSON(srv, "PUT", "/api/user/preferences", map[string]interface{}{
		"digest_frequency": "weekly",
		"digest_content":   "global",
		"spike_threshold":  5.0,
	}, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT preferences: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	// Read back
	rec = doJSON(srv, "GET", "/api/user/preferences", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET preferences: status = %d", rec.Code)
	}

	body := decodeJSON(t, rec)
	if body["digest_frequency"] != "weekly" {
		t.Errorf("freq = %v, want weekly", body["digest_frequency"])
	}
	if body["digest_content"] != "global" {
		t.Errorf("content = %v, want global", body["digest_content"])
	}
	if body["spike_threshold"] != 5.0 {
		t.Errorf("threshold = %v, want 5", body["spike_threshold"])
	}
}

func TestUpdatePreferences_InvalidFrequency(t *testing.T) {
	srv, _ := setupUserTestServer(t)
	token := registerAndLogin(t, srv, "badprefs@example.com", "password1234")

	rec := doJSON(srv, "PUT", "/api/user/preferences", map[string]interface{}{
		"digest_frequency": "hourly",
		"digest_content":   "both",
		"spike_threshold":  2.0,
	}, token)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Watchlist
// ---------------------------------------------------------------------------

func TestUpdateWatchlist(t *testing.T) {
	srv, _ := setupUserTestServer(t)
	token := registerAndLogin(t, srv, "wl@example.com", "password1234")

	// Set watchlist
	rec := doJSON(srv, "PUT", "/api/user/watchlist", map[string]interface{}{
		"watchlist": []string{"Bitcoin", "Taylor Swift", "OpenAI"},
	}, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT watchlist: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	// Read back
	rec = doJSON(srv, "GET", "/api/user/watchlist", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET watchlist: status = %d", rec.Code)
	}

	body := decodeJSON(t, rec)
	wl := body["watchlist"].([]interface{})
	if len(wl) != 3 {
		t.Errorf("watchlist len = %d, want 3", len(wl))
	}
	if count := body["count"].(float64); count != 3 {
		t.Errorf("count = %v, want 3", count)
	}
}

func TestUpdateWatchlist_TooMany(t *testing.T) {
	srv, _ := setupUserTestServer(t)
	token := registerAndLogin(t, srv, "toomany@example.com", "password1234")

	// Build a list of 101 pages
	pages := make([]string, 101)
	for i := range pages {
		pages[i] = "Page_" + string(rune('A'+i%26))
	}

	rec := doJSON(srv, "PUT", "/api/user/watchlist", map[string]interface{}{
		"watchlist": pages,
	}, token)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for >100 pages", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Unsubscribe
// ---------------------------------------------------------------------------

func TestUnsubscribe(t *testing.T) {
	srv, userStore := setupUserTestServer(t)

	// Register a user
	doJSON(srv, "POST", "/api/auth/register", map[string]string{
		"email": "unsub@example.com", "password": "password1234",
	}, "")

	// Get the unsub token directly from DB
	user, _ := userStore.GetUserByEmail("unsub@example.com")
	if user == nil {
		t.Fatal("user not found in DB")
	}

	// Hit unsubscribe endpoint
	req := httptest.NewRequest("GET", "/api/digest/unsubscribe?token="+user.UnsubToken, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unsubscribe status = %d, want 200. Body: %s", rec.Code, rec.Body.String())
	}

	// Verify user is now unsubscribed
	updated, _ := userStore.GetUserByID(user.ID)
	if string(updated.DigestFreq) != "none" {
		t.Errorf("after unsubscribe, freq = %q, want none", updated.DigestFreq)
	}
}

func TestUnsubscribe_BadToken(t *testing.T) {
	srv, _ := setupUserTestServer(t)

	req := httptest.NewRequest("GET", "/api/digest/unsubscribe?token=invalid-token-xyz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for bad token", rec.Code)
	}
}

func TestUnsubscribe_MissingToken(t *testing.T) {
	srv, _ := setupUserTestServer(t)

	req := httptest.NewRequest("GET", "/api/digest/unsubscribe", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for missing token", rec.Code)
	}
}
