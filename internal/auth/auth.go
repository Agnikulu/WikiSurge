package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Errors returned by the auth package.
var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrTokenExpired       = errors.New("token has expired")
	ErrTokenInvalid       = errors.New("invalid token")
	ErrMissingToken       = errors.New("missing authorization token")
)

// Claims are the JWT claims embedded in each token.
type Claims struct {
	UserID string `json:"uid"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// TokenPair holds an access token and its expiry.
type TokenPair struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// JWTService handles token creation and validation.
type JWTService struct {
	secret []byte
	expiry time.Duration
	issuer string
}

// NewJWTService creates a new JWT service.
//   - secret: HMAC signing key (should be ≥32 bytes in production)
//   - expiry: how long tokens are valid (e.g. 24*time.Hour)
func NewJWTService(secret string, expiry time.Duration) *JWTService {
	if expiry == 0 {
		expiry = 24 * time.Hour
	}
	return &JWTService{
		secret: []byte(secret),
		expiry: expiry,
		issuer: "wikisurge",
	}
}

// GenerateToken creates a signed JWT for the given user.
func (s *JWTService) GenerateToken(userID, email string) (*TokenPair, error) {
	now := time.Now()
	expiresAt := now.Add(s.expiry)

	claims := Claims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return nil, fmt.Errorf("sign token: %w", err)
	}

	return &TokenPair{
		AccessToken: signed,
		ExpiresAt:   expiresAt,
	}, nil
}

// ValidateToken parses and validates a JWT, returning the claims if valid.
func (s *JWTService) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.secret, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrTokenInvalid
	}

	return claims, nil
}

// ExtractTokenFromRequest pulls the Bearer token from the Authorization header.
func ExtractTokenFromRequest(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", ErrMissingToken
	}

	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", ErrTokenInvalid
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", ErrMissingToken
	}
	return token, nil
}

// --- Password hashing ---

// HashPassword hashes a plaintext password using bcrypt.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword compares a plaintext password with a bcrypt hash.
func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
