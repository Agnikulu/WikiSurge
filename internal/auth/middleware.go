package auth

import (
	"context"
	"net/http"
)

// contextKey is unexported to prevent collisions with other packages.
type contextKey string

const (
	userIDKey contextKey = "auth_user_id"
	emailKey  contextKey = "auth_email"
)

// Middleware returns an HTTP middleware that validates JWT tokens
// and injects user info into the request context.
// Protected routes wrapped with this middleware can use UserIDFromContext().
func Middleware(jwtSvc *JWTService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr, err := ExtractTokenFromRequest(r)
			if err != nil {
				http.Error(w, `{"error":"unauthorized","message":"missing or invalid token"}`, http.StatusUnauthorized)
				return
			}

			claims, err := jwtSvc.ValidateToken(tokenStr)
			if err != nil {
				status := http.StatusUnauthorized
				msg := `{"error":"unauthorized","message":"invalid token"}`
				if err == ErrTokenExpired {
					msg = `{"error":"token_expired","message":"token has expired, please login again"}`
				}
				http.Error(w, msg, status)
				return
			}

			// Inject user info into context
			ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
			ctx = context.WithValue(ctx, emailKey, claims.Email)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromContext extracts the authenticated user's ID from the request context.
// Returns empty string if not authenticated.
func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(userIDKey).(string); ok {
		return v
	}
	return ""
}

// EmailFromContext extracts the authenticated user's email from the request context.
func EmailFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(emailKey).(string); ok {
		return v
	}
	return ""
}
