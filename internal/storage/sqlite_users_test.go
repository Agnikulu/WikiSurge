package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agnikulu/WikiSurge/internal/models"
)

func newTestUserStore(t *testing.T) *UserStore {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := NewUserStore(dbPath)
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestCreateAndGetUser(t *testing.T) {
	store := newTestUserStore(t)

	user, err := store.CreateUser("alice@example.com", "hashed_pw_123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if user.ID == "" {
		t.Error("expected non-empty user ID")
	}
	if user.Email != "alice@example.com" {
		t.Errorf("email = %q, want alice@example.com", user.Email)
	}
	if user.DigestFreq != models.DigestFreqDaily {
		t.Errorf("default digest freq = %q, want daily", user.DigestFreq)
	}
	if user.SpikeThreshold != 2.0 {
		t.Errorf("default spike threshold = %f, want 2.0", user.SpikeThreshold)
	}
	if user.Verified {
		t.Error("new user should not be verified")
	}
	if user.UnsubToken == "" {
		t.Error("expected non-empty unsub token")
	}

	// Get by email
	fetched, err := store.GetUserByEmail("alice@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if fetched == nil {
		t.Fatal("expected user, got nil")
	}
	if fetched.ID != user.ID {
		t.Errorf("ID mismatch: %q != %q", fetched.ID, user.ID)
	}
	if fetched.PasswordHash != "hashed_pw_123" {
		t.Errorf("password hash mismatch")
	}

	// Get by ID
	fetched2, err := store.GetUserByID(user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if fetched2.Email != "alice@example.com" {
		t.Errorf("email mismatch via GetUserByID")
	}

	// Not found
	notFound, err := store.GetUserByEmail("nobody@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail not found: %v", err)
	}
	if notFound != nil {
		t.Error("expected nil for non-existent user")
	}
}

func TestDuplicateEmail(t *testing.T) {
	store := newTestUserStore(t)

	_, err := store.CreateUser("dup@example.com", "pw1")
	if err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}

	_, err = store.CreateUser("dup@example.com", "pw2")
	if err == nil {
		t.Error("expected error for duplicate email")
	}
}

func TestUpdatePreferences(t *testing.T) {
	store := newTestUserStore(t)

	user, _ := store.CreateUser("prefs@example.com", "pw")

	prefs := models.DigestPreferences{
		DigestFreq:     models.DigestFreqWeekly,
		DigestContent:  models.DigestContentWatchlist,
		SpikeThreshold: 5.0,
	}

	if err := store.UpdatePreferences(user.ID, prefs); err != nil {
		t.Fatalf("UpdatePreferences: %v", err)
	}

	fetched, _ := store.GetUserByID(user.ID)
	if fetched.DigestFreq != models.DigestFreqWeekly {
		t.Errorf("freq = %q, want weekly", fetched.DigestFreq)
	}
	if fetched.DigestContent != models.DigestContentWatchlist {
		t.Errorf("content = %q, want watchlist", fetched.DigestContent)
	}
	if fetched.SpikeThreshold != 5.0 {
		t.Errorf("threshold = %f, want 5.0", fetched.SpikeThreshold)
	}
}

func TestUpdateWatchlist(t *testing.T) {
	store := newTestUserStore(t)

	user, _ := store.CreateUser("wl@example.com", "pw")

	watchlist := []string{"Bitcoin", "Taylor Swift", "OpenAI"}
	if err := store.UpdateWatchlist(user.ID, watchlist); err != nil {
		t.Fatalf("UpdateWatchlist: %v", err)
	}

	fetched, _ := store.GetUserByID(user.ID)
	if len(fetched.Watchlist) != 3 {
		t.Fatalf("watchlist len = %d, want 3", len(fetched.Watchlist))
	}
	if fetched.Watchlist[0] != "Bitcoin" {
		t.Errorf("watchlist[0] = %q, want Bitcoin", fetched.Watchlist[0])
	}
}

func TestSetVerified(t *testing.T) {
	store := newTestUserStore(t)

	user, _ := store.CreateUser("verify@example.com", "pw")

	if err := store.SetVerified(user.ID); err != nil {
		t.Fatalf("SetVerified: %v", err)
	}

	fetched, _ := store.GetUserByID(user.ID)
	if !fetched.Verified {
		t.Error("expected user to be verified")
	}
}

func TestGetUsersForDigest(t *testing.T) {
	store := newTestUserStore(t)

	// Create several users with different settings
	u1, _ := store.CreateUser("daily@example.com", "pw")
	store.SetVerified(u1.ID)
	store.UpdatePreferences(u1.ID, models.DigestPreferences{
		DigestFreq: models.DigestFreqDaily, DigestContent: models.DigestContentAll, SpikeThreshold: 2.0,
	})

	u2, _ := store.CreateUser("weekly@example.com", "pw")
	store.SetVerified(u2.ID)
	store.UpdatePreferences(u2.ID, models.DigestPreferences{
		DigestFreq: models.DigestFreqWeekly, DigestContent: models.DigestContentAll, SpikeThreshold: 2.0,
	})

	u3, _ := store.CreateUser("both@example.com", "pw")
	store.SetVerified(u3.ID)
	store.UpdatePreferences(u3.ID, models.DigestPreferences{
		DigestFreq: models.DigestFreqBoth, DigestContent: models.DigestContentAll, SpikeThreshold: 2.0,
	})

	u4, _ := store.CreateUser("none@example.com", "pw")
	store.SetVerified(u4.ID)
	store.UpdatePreferences(u4.ID, models.DigestPreferences{
		DigestFreq: models.DigestFreqNone, DigestContent: models.DigestContentAll, SpikeThreshold: 2.0,
	})

	// Unverified user with daily freq — should NOT appear
	store.CreateUser("unverified@example.com", "pw")

	// Daily digest: should get u1 (daily) + u3 (both)
	dailyUsers, err := store.GetUsersForDigest(models.DigestFreqDaily)
	if err != nil {
		t.Fatalf("GetUsersForDigest daily: %v", err)
	}
	if len(dailyUsers) != 2 {
		t.Errorf("daily users = %d, want 2", len(dailyUsers))
	}

	// Weekly digest: should get u2 (weekly) + u3 (both)
	weeklyUsers, err := store.GetUsersForDigest(models.DigestFreqWeekly)
	if err != nil {
		t.Fatalf("GetUsersForDigest weekly: %v", err)
	}
	if len(weeklyUsers) != 2 {
		t.Errorf("weekly users = %d, want 2", len(weeklyUsers))
	}
}

func TestUnsubscribe(t *testing.T) {
	store := newTestUserStore(t)

	user, _ := store.CreateUser("unsub@example.com", "pw")

	if err := store.Unsubscribe(user.UnsubToken); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	fetched, _ := store.GetUserByID(user.ID)
	if fetched.DigestFreq != models.DigestFreqNone {
		t.Errorf("after unsubscribe, freq = %q, want none", fetched.DigestFreq)
	}

	// Bad token
	if err := store.Unsubscribe("bad-token"); err == nil {
		t.Error("expected error for bad unsubscribe token")
	}
}

func TestDeleteUser(t *testing.T) {
	store := newTestUserStore(t)

	user, _ := store.CreateUser("delete@example.com", "pw")

	if err := store.DeleteUser(user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	fetched, _ := store.GetUserByEmail("delete@example.com")
	if fetched != nil {
		t.Error("expected nil after delete")
	}
}

func TestUserCount(t *testing.T) {
	store := newTestUserStore(t)

	count, _ := store.UserCount()
	if count != 0 {
		t.Errorf("initial count = %d, want 0", count)
	}

	store.CreateUser("a@example.com", "pw")
	store.CreateUser("b@example.com", "pw")

	count, _ = store.UserCount()
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestNewUserStoreCreatesFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "subdir", "test.db")

	// Ensure parent dir exists (SQLite needs it)
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	store, err := NewUserStore(dbPath)
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	defer store.Close()

	// Verify we can use it
	_, err = store.CreateUser("test@example.com", "pw")
	if err != nil {
		t.Fatalf("CreateUser after fresh init: %v", err)
	}
}
