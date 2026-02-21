package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Agnikulu/WikiSurge/internal/auth"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
)

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	Token     string       `json:"token"`
	ExpiresAt string       `json:"expires_at"`
	User      userResponse `json:"user"`
}

type userResponse struct {
	ID             string   `json:"id"`
	Email          string   `json:"email"`
	Verified       bool     `json:"verified"`
	DigestFreq     string   `json:"digest_frequency"`
	DigestContent  string   `json:"digest_content"`
	SpikeThreshold float64  `json:"spike_threshold"`
	Watchlist      []string `json:"watchlist"`
}

type updateWatchlistRequest struct {
	Watchlist []string `json:"watchlist"`
}

// ---------------------------------------------------------------------------
// Auth Handlers
// ---------------------------------------------------------------------------

func (s *APIServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid JSON body", ErrCodeInvalidParameter, "")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	// Validate
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		writeAPIError(w, r, http.StatusBadRequest, "Valid email is required", ErrCodeInvalidParameter, "field: email")
		return
	}
	if len(req.Password) < 8 {
		writeAPIError(w, r, http.StatusBadRequest, "Password must be at least 8 characters", ErrCodeInvalidParameter, "field: password")
		return
	}

	// Check existing
	existing, err := s.userStore.GetUserByEmail(req.Email)
	if err != nil {
		s.logger.Error().Err(err).Msg("register: db error checking existing user")
		writeAPIError(w, r, http.StatusInternalServerError, "Registration failed", ErrCodeInternalError, "")
		return
	}
	if existing != nil {
		writeAPIError(w, r, http.StatusConflict, "An account with this email already exists", "EMAIL_EXISTS", "")
		return
	}

	// Hash password
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.logger.Error().Err(err).Msg("register: failed to hash password")
		writeAPIError(w, r, http.StatusInternalServerError, "Registration failed", ErrCodeInternalError, "")
		return
	}

	// Create user
	user, err := s.userStore.CreateUser(req.Email, hash)
	if err != nil {
		s.logger.Error().Err(err).Msg("register: failed to create user")
		writeAPIError(w, r, http.StatusInternalServerError, "Registration failed", ErrCodeInternalError, "")
		return
	}

	// Auto-verify for now (in production, send verification email)
	_ = s.userStore.SetVerified(user.ID)
	user.Verified = true

	// Generate token
	tokenPair, err := s.jwtService.GenerateToken(user.ID, user.Email)
	if err != nil {
		s.logger.Error().Err(err).Msg("register: failed to generate token")
		writeAPIError(w, r, http.StatusInternalServerError, "Registration failed", ErrCodeInternalError, "")
		return
	}

	s.logger.Info().Str("user_id", user.ID).Str("email", user.Email).Msg("User registered")

	respondJSON(w, http.StatusCreated, authResponse{
		Token:     tokenPair.AccessToken,
		ExpiresAt: tokenPair.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		User:      toUserResponse(user),
	})
}

func (s *APIServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid JSON body", ErrCodeInvalidParameter, "")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	if req.Email == "" || req.Password == "" {
		writeAPIError(w, r, http.StatusBadRequest, "Email and password are required", ErrCodeInvalidParameter, "")
		return
	}

	user, err := s.userStore.GetUserByEmail(req.Email)
	if err != nil {
		s.logger.Error().Err(err).Msg("login: db error")
		writeAPIError(w, r, http.StatusInternalServerError, "Login failed", ErrCodeInternalError, "")
		return
	}
	if user == nil {
		writeAPIError(w, r, http.StatusUnauthorized, "Invalid email or password", ErrCodeUnauthorized, "")
		return
	}

	if !auth.CheckPassword(req.Password, user.PasswordHash) {
		writeAPIError(w, r, http.StatusUnauthorized, "Invalid email or password", ErrCodeUnauthorized, "")
		return
	}

	tokenPair, err := s.jwtService.GenerateToken(user.ID, user.Email)
	if err != nil {
		s.logger.Error().Err(err).Msg("login: failed to generate token")
		writeAPIError(w, r, http.StatusInternalServerError, "Login failed", ErrCodeInternalError, "")
		return
	}

	s.logger.Info().Str("user_id", user.ID).Msg("User logged in")

	respondJSON(w, http.StatusOK, authResponse{
		Token:     tokenPair.AccessToken,
		ExpiresAt: tokenPair.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		User:      toUserResponse(user),
	})
}

// ---------------------------------------------------------------------------
// User Profile / Preferences Handlers (require auth)
// ---------------------------------------------------------------------------

func (s *APIServer) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	user, err := s.userStore.GetUserByID(userID)
	if err != nil || user == nil {
		writeAPIError(w, r, http.StatusNotFound, "User not found", ErrCodeNotFound, "")
		return
	}

	respondJSON(w, http.StatusOK, toUserResponse(user))
}

func (s *APIServer) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	user, err := s.userStore.GetUserByID(userID)
	if err != nil || user == nil {
		writeAPIError(w, r, http.StatusNotFound, "User not found", ErrCodeNotFound, "")
		return
	}

	respondJSON(w, http.StatusOK, models.DigestPreferences{
		DigestFreq:     user.DigestFreq,
		DigestContent:  user.DigestContent,
		SpikeThreshold: user.SpikeThreshold,
	})
}

func (s *APIServer) handleUpdatePreferences(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	var prefs models.DigestPreferences
	if err := json.NewDecoder(r.Body).Decode(&prefs); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid JSON body", ErrCodeInvalidParameter, "")
		return
	}

	if errMsg := prefs.Validate(); errMsg != "" {
		writeAPIError(w, r, http.StatusBadRequest, errMsg, ErrCodeInvalidParameter, "")
		return
	}

	if err := s.userStore.UpdatePreferences(userID, prefs); err != nil {
		s.logger.Error().Err(err).Str("user_id", userID).Msg("failed to update preferences")
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to update preferences", ErrCodeInternalError, "")
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message":    "Preferences updated",
		"preferences": prefs,
	})
}

func (s *APIServer) handleGetWatchlist(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	user, err := s.userStore.GetUserByID(userID)
	if err != nil || user == nil {
		writeAPIError(w, r, http.StatusNotFound, "User not found", ErrCodeNotFound, "")
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"watchlist": user.Watchlist,
		"count":     len(user.Watchlist),
	})
}

func (s *APIServer) handleUpdateWatchlist(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	var req updateWatchlistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid JSON body", ErrCodeInvalidParameter, "")
		return
	}

	if len(req.Watchlist) > 100 {
		writeAPIError(w, r, http.StatusBadRequest, "Watchlist cannot exceed 100 pages", ErrCodeInvalidParameter, "")
		return
	}

	// Normalize: trim whitespace
	for i := range req.Watchlist {
		req.Watchlist[i] = strings.TrimSpace(req.Watchlist[i])
	}

	if err := s.userStore.UpdateWatchlist(userID, req.Watchlist); err != nil {
		s.logger.Error().Err(err).Str("user_id", userID).Msg("failed to update watchlist")
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to update watchlist", ErrCodeInternalError, "")
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message":   "Watchlist updated",
		"watchlist": req.Watchlist,
		"count":     len(req.Watchlist),
	})
}

// ---------------------------------------------------------------------------
// Unsubscribe (no auth — uses token from email link)
// ---------------------------------------------------------------------------

func (s *APIServer) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeAPIError(w, r, http.StatusBadRequest, "Missing unsubscribe token", ErrCodeInvalidParameter, "")
		return
	}

	if err := s.userStore.Unsubscribe(token); err != nil {
		writeAPIError(w, r, http.StatusNotFound, "Invalid or expired unsubscribe link", ErrCodeNotFound, "")
		return
	}

	// Return a simple HTML page for email click-throughs
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<!DOCTYPE html><html><body style="font-family:sans-serif;text-align:center;padding:60px">
		<h1>Unsubscribed</h1>
		<p>You've been unsubscribed from WikiSurge digest emails.</p>
		<p>You can re-subscribe anytime from your <a href="` + s.config.Email.DashboardURL + `">dashboard settings</a>.</p>
	</body></html>`))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func toUserResponse(u *models.User) userResponse {
	wl := u.Watchlist
	if wl == nil {
		wl = []string{}
	}
	return userResponse{
		ID:             u.ID,
		Email:          u.Email,
		Verified:       u.Verified,
		DigestFreq:     string(u.DigestFreq),
		DigestContent:  string(u.DigestContent),
		SpikeThreshold: u.SpikeThreshold,
		Watchlist:      wl,
	}
}

// userStoreInterface defines the methods used by user handlers.
// Useful for testing with mocks.
type userStoreInterface interface {
	CreateUser(email, passwordHash string) (*models.User, error)
	GetUserByEmail(email string) (*models.User, error)
	GetUserByID(id string) (*models.User, error)
	GetUserByUnsubToken(token string) (*models.User, error)
	UpdatePreferences(userID string, prefs models.DigestPreferences) error
	UpdateWatchlist(userID string, watchlist []string) error
	SetVerified(userID string) error
	Unsubscribe(token string) error
	GetUsersForDigest(freq models.DigestFrequency) ([]*models.User, error)
	MarkDigestSent(userID string, t interface{}) error
}

// Ensure *storage.UserStore satisfies the interface at compile time.
var _ interface {
	CreateUser(email, passwordHash string) (*models.User, error)
	GetUserByEmail(email string) (*models.User, error)
	GetUserByID(id string) (*models.User, error)
	UpdatePreferences(userID string, prefs models.DigestPreferences) error
	UpdateWatchlist(userID string, watchlist []string) error
	SetVerified(userID string) error
	Unsubscribe(token string) error
} = (*storage.UserStore)(nil)
