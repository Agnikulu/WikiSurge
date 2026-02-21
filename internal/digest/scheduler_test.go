package digest

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// Mock email sender for testing
// ---------------------------------------------------------------------------

type mockSender struct {
	mu       sync.Mutex
	sent     []mockEmail
	failFor  map[string]bool // emails that should fail
}

type mockEmail struct {
	To      string
	Subject string
	Body    string
}

func newMockSender() *mockSender {
	return &mockSender{failFor: make(map[string]bool)}
}

func (m *mockSender) Send(_ context.Context, to, subject, htmlBody string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failFor[to] {
		return fmt.Errorf("mock send failure for %s", to)
	}
	m.sent = append(m.sent, mockEmail{To: to, Subject: subject, Body: htmlBody})
	return nil
}

func (m *mockSender) getSent() []mockEmail {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]mockEmail, len(m.sent))
	copy(cp, m.sent)
	return cp
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func setupTestScheduler(t *testing.T) (*Scheduler, *mockSender, *storage.UserStore, *Collector) {
	t.Helper()

	collector, rc, _ := setupTestCollector(t)
	sender := newMockSender()

	// Create a temp SQLite DB for users
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	userStore, err := storage.NewUserStore(dbPath)
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	t.Cleanup(func() { userStore.Close() })

	// Seed some trending data so global highlights are non-empty
	ctx := context.Background()
	rc.ZAdd(ctx, "trending:scores", redis.Z{Score: 100, Member: "Trending_Page_1"})
	rc.ZAdd(ctx, "trending:scores", redis.Z{Score: 50, Member: "Trending_Page_2"})

	cfg := SchedulerConfig{
		DailySendHour:      9,
		WeeklySendDay:      1, // Monday
		WeeklySendHour:     10,
		MaxConcurrentSends: 2,
		DashboardURL:       "https://wikisurge.example.com",
		Enabled:            true,
	}

	logger := zerolog.Nop()
	sched := NewScheduler(collector, sender, userStore, cfg, logger)

	return sched, sender, userStore, collector
}

func createTestUser(t *testing.T, userStore *storage.UserStore, emailAddr string, freq models.DigestFrequency, watchlist []string) *models.User {
	t.Helper()

	user, err := userStore.CreateUser(emailAddr, "$2a$10$testhashdoesntmatterforschedulertests")
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", emailAddr, err)
	}

	// Verify the user (required for digest eligibility)
	if err := userStore.SetVerified(user.ID); err != nil {
		t.Fatalf("SetVerified(%s): %v", emailAddr, err)
	}

	// Set digest preferences
	if err := userStore.UpdatePreferences(user.ID, models.DigestPreferences{
		DigestFreq:     freq,
		DigestContent:  models.DigestContentAll,
		SpikeThreshold: 2.0,
	}); err != nil {
		t.Fatalf("UpdatePreferences(%s): %v", emailAddr, err)
	}

	// Set watchlist if provided
	if len(watchlist) > 0 {
		if err := userStore.UpdateWatchlist(user.ID, watchlist); err != nil {
			t.Fatalf("UpdateWatchlist(%s): %v", emailAddr, err)
		}
	}

	// Re-fetch so user struct is up to date
	user, err = userStore.GetUserByID(user.ID)
	if err != nil {
		t.Fatalf("GetUserByID(%s): %v", emailAddr, err)
	}
	return user
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestScheduler_RunNow_NoUsers(t *testing.T) {
	sched, sender, _, _ := setupTestScheduler(t)

	sent, skipped, errored := sched.RunNow(context.Background(), "daily")

	if sent != 0 || skipped != 0 || errored != 0 {
		t.Errorf("expected (0,0,0), got (%d,%d,%d)", sent, skipped, errored)
	}
	if len(sender.getSent()) != 0 {
		t.Errorf("expected no emails sent")
	}
}

func TestScheduler_RunNow_SingleDailyUser(t *testing.T) {
	sched, _, userStore, _ := setupTestScheduler(t)

	createTestUser(t, userStore, "alice@example.com", models.DigestFreqDaily, nil)

	sent, skipped, errored := sched.RunNow(context.Background(), "daily")

	// The user may get skipped if there's not enough content (no watchlist, thresholds)
	// or sent if there are global highlights
	total := sent + skipped + errored
	if total != 1 {
		t.Errorf("expected 1 user processed, got %d (sent=%d, skipped=%d, err=%d)", total, sent, skipped, errored)
	}
}

func TestScheduler_RunNow_MultipleUsers(t *testing.T) {
	sched, _, userStore, _ := setupTestScheduler(t)

	createTestUser(t, userStore, "alice@example.com", models.DigestFreqDaily, nil)
	createTestUser(t, userStore, "bob@example.com", models.DigestFreqDaily, nil)
	createTestUser(t, userStore, "charlie@example.com", models.DigestFreqDaily, nil)

	sent, skipped, errored := sched.RunNow(context.Background(), "daily")

	total := sent + skipped + errored
	if total != 3 {
		t.Errorf("expected 3 users processed, got %d (sent=%d, skipped=%d, err=%d)", total, sent, skipped, errored)
	}
	if errored != 0 {
		t.Errorf("expected 0 errors, got %d", errored)
	}
}

func TestScheduler_RunNow_FrequencyFiltering(t *testing.T) {
	sched, _, userStore, _ := setupTestScheduler(t)

	createTestUser(t, userStore, "daily@example.com", models.DigestFreqDaily, nil)
	createTestUser(t, userStore, "weekly@example.com", models.DigestFreqWeekly, nil)
	createTestUser(t, userStore, "both@example.com", models.DigestFreqBoth, nil)
	createTestUser(t, userStore, "none@example.com", models.DigestFreqNone, nil)

	// Daily run: should process daily + both users (not weekly, not none)
	sent, skipped, _ := sched.RunNow(context.Background(), "daily")
	dailyTotal := sent + skipped
	if dailyTotal != 2 {
		t.Errorf("daily run: expected 2 users, got %d", dailyTotal)
	}

	// Weekly run: should process weekly + both users
	sent, skipped, _ = sched.RunNow(context.Background(), "weekly")
	weeklyTotal := sent + skipped
	if weeklyTotal != 2 {
		t.Errorf("weekly run: expected 2 users, got %d", weeklyTotal)
	}
}

func TestScheduler_RunNow_EmailSendFailure(t *testing.T) {
	sched, sender, userStore, _ := setupTestScheduler(t)

	createTestUser(t, userStore, "fail@example.com", models.DigestFreqDaily, nil)
	sender.failFor["fail@example.com"] = true

	sent, skipped, errored := sched.RunNow(context.Background(), "daily")

	// The user is either skipped (no content) or errored (send fails)
	total := sent + skipped + errored
	if total != 1 {
		t.Errorf("expected 1 user, got %d", total)
	}
	if sent != 0 {
		t.Errorf("expected 0 sent (email should fail), got %d", sent)
	}
}

func TestScheduler_RunNow_MarkDigestSent(t *testing.T) {
	sched, sender, userStore, _ := setupTestScheduler(t)

	user := createTestUser(t, userStore, "mark@example.com", models.DigestFreqDaily, nil)

	before := time.Now().UTC().Add(-1 * time.Second)
	sched.RunNow(context.Background(), "daily")

	// Re-fetch user
	updated, err := userStore.GetUserByID(user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}

	emails := sender.getSent()
	if len(emails) > 0 {
		// If email was sent (not skipped), LastDigestAt should be updated
		if updated.LastDigestAt.Before(before) {
			t.Errorf("expected LastDigestAt to be updated after sending")
		}
	}
}

func TestScheduler_RunNow_ConcurrentSends(t *testing.T) {
	sched, _, userStore, _ := setupTestScheduler(t)
	sched.config.MaxConcurrentSends = 2

	// Create enough users to exercise the worker pool
	for i := 0; i < 10; i++ {
		createTestUser(t, userStore, fmt.Sprintf("user%d@example.com", i), models.DigestFreqDaily, nil)
	}

	sent, skipped, errored := sched.RunNow(context.Background(), "daily")
	total := sent + skipped + errored
	if total != 10 {
		t.Errorf("expected 10 users processed, got %d", total)
	}
	if errored != 0 {
		t.Errorf("expected no errors, got %d", errored)
	}
}

func TestScheduler_StartStop(t *testing.T) {
	sched, _, _, _ := setupTestScheduler(t)

	// Start and immediately stop — should not panic or hang
	sched.Start()

	// Give it a moment to start the goroutine
	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		sched.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK — stopped cleanly
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler Stop() hung for > 5 seconds")
	}
}

func TestScheduler_DefaultConcurrency(t *testing.T) {
	collector, _, _ := setupTestCollector(t)
	sender := newMockSender()

	cfg := SchedulerConfig{
		MaxConcurrentSends: 0, // Should be corrected to default
	}

	sched := NewScheduler(collector, sender, nil, cfg, zerolog.Nop())
	if sched.config.MaxConcurrentSends != 10 {
		t.Errorf("expected default concurrency 10, got %d", sched.config.MaxConcurrentSends)
	}
}

func TestScheduler_RunNow_MixedSuccessFailure(t *testing.T) {
	sched, sender, userStore, _ := setupTestScheduler(t)

	createTestUser(t, userStore, "ok1@example.com", models.DigestFreqDaily, nil)
	createTestUser(t, userStore, "fail@example.com", models.DigestFreqDaily, nil)
	createTestUser(t, userStore, "ok2@example.com", models.DigestFreqDaily, nil)

	sender.failFor["fail@example.com"] = true

	sent, skipped, errored := sched.RunNow(context.Background(), "daily")
	total := sent + skipped + errored
	if total != 3 {
		t.Errorf("expected 3 users, got %d (sent=%d, skipped=%d, err=%d)", total, sent, skipped, errored)
	}
}

func TestScheduler_RunNow_WeeklyPeriod(t *testing.T) {
	sched, _, userStore, _ := setupTestScheduler(t)

	createTestUser(t, userStore, "weekly@example.com", models.DigestFreqWeekly, nil)

	sent, skipped, errored := sched.RunNow(context.Background(), "weekly")
	total := sent + skipped + errored
	if total != 1 {
		t.Errorf("expected 1 user processed, got %d", total)
	}
	if errored != 0 {
		t.Errorf("expected no errors, got %d", errored)
	}
}

func TestScheduler_RunNow_ContextCancellation(t *testing.T) {
	sched, _, userStore, _ := setupTestScheduler(t)

	for i := 0; i < 5; i++ {
		createTestUser(t, userStore, fmt.Sprintf("cancel%d@example.com", i), models.DigestFreqDaily, nil)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should handle cancelled context gracefully
	sent, skipped, errored := sched.RunNow(ctx, "daily")
	total := sent + skipped + errored
	// Could process some or none — the important thing is no panic
	_ = total
}
