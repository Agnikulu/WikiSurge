package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"

	"github.com/Agnikulu/WikiSurge/internal/models"
)

// UserStore manages user persistence in SQLite.
type UserStore struct {
	db *sql.DB
}

// NewUserStore opens (or creates) the SQLite database at path and runs migrations.
func NewUserStore(dbPath string) (*UserStore, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Connection pool settings for SQLite
	db.SetMaxOpenConns(1) // SQLite doesn't support concurrent writes
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &UserStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return store, nil
}

// migrate creates the schema if it doesn't exist.
func (s *UserStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id             TEXT PRIMARY KEY,
		email          TEXT UNIQUE NOT NULL,
		password_hash  TEXT NOT NULL,
		watchlist      TEXT NOT NULL DEFAULT '[]',
		digest_freq    TEXT NOT NULL DEFAULT 'daily',
		digest_content TEXT NOT NULL DEFAULT 'both',
		spike_threshold REAL NOT NULL DEFAULT 2.0,
		unsub_token    TEXT UNIQUE NOT NULL,
		verified       INTEGER NOT NULL DEFAULT 0,
		created_at     TEXT NOT NULL,
		last_digest_at TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
	CREATE INDEX IF NOT EXISTS idx_users_digest_freq ON users(digest_freq);
	CREATE INDEX IF NOT EXISTS idx_users_unsub_token ON users(unsub_token);
	CREATE INDEX IF NOT EXISTS idx_users_verified ON users(verified);
	`
	_, err := s.db.Exec(schema)
	return err
}

// Close closes the database connection.
func (s *UserStore) Close() error {
	return s.db.Close()
}

// CreateUser inserts a new user. Returns the created User (with generated ID and unsub token).
func (s *UserStore) CreateUser(email, passwordHash string) (*models.User, error) {
	user := &models.User{
		ID:             uuid.New().String(),
		Email:          email,
		PasswordHash:   passwordHash,
		Watchlist:      []string{},
		DigestFreq:     models.DigestFreqDaily,
		DigestContent:  models.DigestContentAll,
		SpikeThreshold: 2.0,
		UnsubToken:     uuid.New().String(),
		Verified:       false,
		CreatedAt:      time.Now().UTC(),
		LastDigestAt:   time.Time{}, // zero value = never sent
	}

	watchlistJSON, _ := json.Marshal(user.Watchlist)

	_, err := s.db.Exec(`
		INSERT INTO users (id, email, password_hash, watchlist, digest_freq, digest_content,
		                    spike_threshold, unsub_token, verified, created_at, last_digest_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Email, user.PasswordHash, string(watchlistJSON),
		string(user.DigestFreq), string(user.DigestContent),
		user.SpikeThreshold, user.UnsubToken, boolToInt(user.Verified),
		user.CreatedAt.Format(time.RFC3339), user.LastDigestAt.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}
	return user, nil
}

// GetUserByEmail fetches a user by email address. Returns nil, nil if not found.
func (s *UserStore) GetUserByEmail(email string) (*models.User, error) {
	return s.scanUser(s.db.QueryRow(`SELECT * FROM users WHERE email = ?`, email))
}

// GetUserByID fetches a user by ID. Returns nil, nil if not found.
func (s *UserStore) GetUserByID(id string) (*models.User, error) {
	return s.scanUser(s.db.QueryRow(`SELECT * FROM users WHERE id = ?`, id))
}

// GetUserByUnsubToken fetches a user by their unsubscribe token.
func (s *UserStore) GetUserByUnsubToken(token string) (*models.User, error) {
	return s.scanUser(s.db.QueryRow(`SELECT * FROM users WHERE unsub_token = ?`, token))
}

// GetUsersForDigest returns all verified users who should receive a digest of the given frequency.
func (s *UserStore) GetUsersForDigest(freq models.DigestFrequency) ([]*models.User, error) {
	// "both" users get both daily AND weekly digests
	rows, err := s.db.Query(`
		SELECT * FROM users
		WHERE verified = 1 AND (digest_freq = ? OR digest_freq = 'both')`,
		string(freq),
	)
	if err != nil {
		return nil, fmt.Errorf("query digest users: %w", err)
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		u, err := s.scanUserFromRows(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// UpdatePreferences updates a user's digest preferences.
func (s *UserStore) UpdatePreferences(userID string, prefs models.DigestPreferences) error {
	result, err := s.db.Exec(`
		UPDATE users SET digest_freq = ?, digest_content = ?, spike_threshold = ?
		WHERE id = ?`,
		string(prefs.DigestFreq), string(prefs.DigestContent), prefs.SpikeThreshold, userID,
	)
	if err != nil {
		return fmt.Errorf("update preferences: %w", err)
	}
	return checkRowsAffected(result, "user not found")
}

// UpdateWatchlist replaces a user's watchlist entirely.
func (s *UserStore) UpdateWatchlist(userID string, watchlist []string) error {
	data, _ := json.Marshal(watchlist)
	result, err := s.db.Exec(`UPDATE users SET watchlist = ? WHERE id = ?`, string(data), userID)
	if err != nil {
		return fmt.Errorf("update watchlist: %w", err)
	}
	return checkRowsAffected(result, "user not found")
}

// SetVerified marks a user as email-verified.
func (s *UserStore) SetVerified(userID string) error {
	result, err := s.db.Exec(`UPDATE users SET verified = 1 WHERE id = ?`, userID)
	if err != nil {
		return fmt.Errorf("verify user: %w", err)
	}
	return checkRowsAffected(result, "user not found")
}

// MarkDigestSent records that a digest was sent to this user.
func (s *UserStore) MarkDigestSent(userID string, sentAt time.Time) error {
	result, err := s.db.Exec(`UPDATE users SET last_digest_at = ? WHERE id = ?`,
		sentAt.Format(time.RFC3339), userID)
	if err != nil {
		return fmt.Errorf("mark digest sent: %w", err)
	}
	return checkRowsAffected(result, "user not found")
}

// Unsubscribe sets digest frequency to "none" for the user with the given unsubscribe token.
func (s *UserStore) Unsubscribe(token string) error {
	result, err := s.db.Exec(`UPDATE users SET digest_freq = 'none' WHERE unsub_token = ?`, token)
	if err != nil {
		return fmt.Errorf("unsubscribe: %w", err)
	}
	return checkRowsAffected(result, "invalid unsubscribe token")
}

// DeleteUser removes a user by ID.
func (s *UserStore) DeleteUser(userID string) error {
	result, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return checkRowsAffected(result, "user not found")
}

// UserCount returns the total number of users.
func (s *UserStore) UserCount() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// --- internal helpers ---

func (s *UserStore) scanUser(row *sql.Row) (*models.User, error) {
	u := &models.User{}
	var watchlistJSON string
	var digestFreq, digestContent string
	var verified int
	var createdAt, lastDigestAt string

	err := row.Scan(
		&u.ID, &u.Email, &u.PasswordHash, &watchlistJSON,
		&digestFreq, &digestContent, &u.SpikeThreshold,
		&u.UnsubToken, &verified, &createdAt, &lastDigestAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}

	_ = json.Unmarshal([]byte(watchlistJSON), &u.Watchlist)
	u.DigestFreq = models.DigestFrequency(digestFreq)
	u.DigestContent = models.DigestContent(digestContent)
	u.Verified = verified == 1
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	u.LastDigestAt, _ = time.Parse(time.RFC3339, lastDigestAt)

	return u, nil
}

func (s *UserStore) scanUserFromRows(rows *sql.Rows) (*models.User, error) {
	u := &models.User{}
	var watchlistJSON string
	var digestFreq, digestContent string
	var verified int
	var createdAt, lastDigestAt string

	err := rows.Scan(
		&u.ID, &u.Email, &u.PasswordHash, &watchlistJSON,
		&digestFreq, &digestContent, &u.SpikeThreshold,
		&u.UnsubToken, &verified, &createdAt, &lastDigestAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan user row: %w", err)
	}

	_ = json.Unmarshal([]byte(watchlistJSON), &u.Watchlist)
	u.DigestFreq = models.DigestFrequency(digestFreq)
	u.DigestContent = models.DigestContent(digestContent)
	u.Verified = verified == 1
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	u.LastDigestAt, _ = time.Parse(time.RFC3339, lastDigestAt)

	return u, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func checkRowsAffected(result sql.Result, notFoundMsg string) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s", notFoundMsg)
	}
	return nil
}
