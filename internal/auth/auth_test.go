package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- JWT Tests ---

func TestGenerateAndValidateToken(t *testing.T) {
	svc := NewJWTService("test-secret-key-that-is-long-enough", 1*time.Hour)

	pair, err := svc.GenerateToken("user-123", "alice@example.com")
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if pair.AccessToken == "" {
		t.Fatal("expected non-empty access token")
	}
	if pair.ExpiresAt.Before(time.Now()) {
		t.Error("token already expired")
	}

	// Validate the token
	claims, err := svc.ValidateToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Errorf("UserID = %q, want user-123", claims.UserID)
	}
	if claims.Email != "alice@example.com" {
		t.Errorf("Email = %q, want alice@example.com", claims.Email)
	}
}

func TestValidateToken_Invalid(t *testing.T) {
	svc := NewJWTService("secret-key-abcdef", 1*time.Hour)

	_, err := svc.ValidateToken("not.a.valid.token")
	if err == nil {
		t.Error("expected error for invalid token")
	}
	if err != ErrTokenInvalid {
		t.Errorf("expected ErrTokenInvalid, got: %v", err)
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	svc1 := NewJWTService("secret-one-aaaaa", 1*time.Hour)
	svc2 := NewJWTService("secret-two-bbbbb", 1*time.Hour)

	pair, _ := svc1.GenerateToken("user-1", "a@b.com")
	_, err := svc2.ValidateToken(pair.AccessToken)
	if err == nil {
		t.Error("expected error when validating with wrong secret")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	svc := NewJWTService("secret-key-abcdef", 1*time.Millisecond)

	pair, _ := svc.GenerateToken("user-1", "a@b.com")
	time.Sleep(10 * time.Millisecond)

	_, err := svc.ValidateToken(pair.AccessToken)
	if err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got: %v", err)
	}
}

func TestDefaultExpiry(t *testing.T) {
	svc := NewJWTService("secret-key-abcdef", 0)
	if svc.expiry != 24*time.Hour {
		t.Errorf("default expiry = %v, want 24h", svc.expiry)
	}
}

// --- Password Tests ---

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("my-secret-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if hash == "my-secret-password" {
		t.Error("hash should not equal plaintext")
	}

	if !CheckPassword("my-secret-password", hash) {
		t.Error("CheckPassword should return true for correct password")
	}
	if CheckPassword("wrong-password", hash) {
		t.Error("CheckPassword should return false for wrong password")
	}
}

// --- ExtractToken Tests ---

func TestExtractTokenFromRequest(t *testing.T) {
	tests := []struct {
		name      string
		authHdr   string
		wantToken string
		wantErr   error
	}{
		{"valid bearer", "Bearer abc123", "abc123", nil},
		{"bearer lowercase", "bearer abc123", "abc123", nil},
		{"BEARER uppercase", "BEARER abc123", "abc123", nil},
		{"missing header", "", "", ErrMissingToken},
		{"no bearer prefix", "abc123", "", ErrTokenInvalid},
		{"empty token", "Bearer ", "", ErrMissingToken},
		{"basic auth", "Basic dXNlcjpwYXNz", "", ErrTokenInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			if tt.authHdr != "" {
				r.Header.Set("Authorization", tt.authHdr)
			}

			token, err := ExtractTokenFromRequest(r)
			if err != tt.wantErr {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if token != tt.wantToken {
				t.Errorf("token = %q, want %q", token, tt.wantToken)
			}
		})
	}
}

// --- Middleware Tests ---

func TestMiddleware_ValidToken(t *testing.T) {
	svc := NewJWTService("test-middleware-secret-key-long", 1*time.Hour)
	pair, _ := svc.GenerateToken("user-42", "test@example.com")

	called := false
	handler := Middleware(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		uid := UserIDFromContext(r.Context())
		email := EmailFromContext(r.Context())
		if uid != "user-42" {
			t.Errorf("UserID from context = %q, want user-42", uid)
		}
		if email != "test@example.com" {
			t.Errorf("Email from context = %q, want test@example.com", email)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("handler was not called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestMiddleware_MissingToken(t *testing.T) {
	svc := NewJWTService("test-middleware-secret-key-long", 1*time.Hour)

	handler := Middleware(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called without token")
	}))

	req := httptest.NewRequest("GET", "/protected", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestMiddleware_ExpiredToken(t *testing.T) {
	svc := NewJWTService("test-middleware-secret-key-long", 1*time.Millisecond)
	pair, _ := svc.GenerateToken("user-1", "a@b.com")
	time.Sleep(10 * time.Millisecond)

	handler := Middleware(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with expired token")
	}))

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// --- Context helper tests ---

func TestContextHelpers_NoAuth(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	if uid := UserIDFromContext(req.Context()); uid != "" {
		t.Errorf("expected empty UserID, got %q", uid)
	}
	if email := EmailFromContext(req.Context()); email != "" {
		t.Errorf("expected empty Email, got %q", email)
	}
}
