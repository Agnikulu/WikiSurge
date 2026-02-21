package digest

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// capturedEmail stores an email sent during tests.
type capturedEmail struct {
	To      string
	Subject string
	HTML    string
}

// captureSender records every sent email so tests can inspect them.
type captureSender struct {
	mu     sync.Mutex
	emails []capturedEmail
}

func (c *captureSender) Send(_ context.Context, to, subject, html string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.emails = append(c.emails, capturedEmail{To: to, Subject: subject, HTML: html})
	return nil
}

func (c *captureSender) all() []capturedEmail {
	c.mu.Lock()
	defer c.mu.Unlock()
	cpy := make([]capturedEmail, len(c.emails))
	copy(cpy, c.emails)
	return cpy
}

// ---------------------------------------------------------------------------
// E2E: register → preferences → seed data → trigger digest → verify email
// ---------------------------------------------------------------------------

func TestE2E_FullDigestPipeline(t *testing.T) {
	// ---- Infrastructure ----
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rc.Close()

	// SQLite in-memory for users
	userStore, err := storage.NewUserStore(":memory:")
	if err != nil {
		t.Fatalf("user store: %v", err)
	}
	defer userStore.Close()

	// Redis storage components
	trendingCfg := &config.TrendingConfig{
		Enabled: true, MaxPages: 100, HalfLifeMinutes: 30, PruneInterval: 5 * time.Minute,
	}
	hotPagesCfg := &config.HotPages{
		MaxTracked: 100, PromotionThreshold: 3, WindowDuration: 15 * time.Minute,
		MaxMembersPerPage: 50, HotThreshold: 2, CleanupInterval: 5 * time.Minute,
	}
	trending := storage.NewTrendingScorerForTest(rc, trendingCfg)
	hotPages := storage.NewHotPageTracker(rc, hotPagesCfg)
	alerts := storage.NewRedisAlerts(rc)
	stats := storage.NewStatsTracker(rc)
	defer trending.Stop()
	defer hotPages.Shutdown()

	logger := zerolog.Nop()
	collector := NewCollector(trending, alerts, hotPages, stats, logger)
	sender := &captureSender{}

	scheduler := NewScheduler(collector, sender, userStore, SchedulerConfig{
		DailySendHour:      8,
		WeeklySendDay:      1,
		WeeklySendHour:     8,
		MaxConcurrentSends: 5,
		DashboardURL:       "https://wikisurge.test",
		Enabled:            true,
	}, logger)

	ctx := context.Background()

	// ---- Step 1: Register users ----
	t.Log("Step 1: Creating users")

	alice, err := userStore.CreateUser("alice@example.com", "hashedpw1")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	bob, err := userStore.CreateUser("bob@example.com", "hashedpw2")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	// Carol: digest disabled (should not receive email)
	carol, err := userStore.CreateUser("carol@example.com", "hashedpw3")
	if err != nil {
		t.Fatalf("create carol: %v", err)
	}

	// Verify all users (required for digest eligibility)
	for _, u := range []*models.User{alice, bob, carol} {
		if err := userStore.SetVerified(u.ID); err != nil {
			t.Fatalf("verify %s: %v", u.Email, err)
		}
	}

	// ---- Step 2: Set preferences ----
	t.Log("Step 2: Setting digest preferences")

	// Alice: daily, all content, watches Bitcoin
	if err := userStore.UpdatePreferences(alice.ID, models.DigestPreferences{
		DigestFreq:     models.DigestFreqDaily,
		DigestContent:  models.DigestContentAll,
		SpikeThreshold: 2.0,
	}); err != nil {
		t.Fatalf("alice prefs: %v", err)
	}
	if err := userStore.UpdateWatchlist(alice.ID, []string{"Bitcoin", "Ethereum"}); err != nil {
		t.Fatalf("alice watchlist: %v", err)
	}

	// Bob: daily, global only (no watchlist content)
	if err := userStore.UpdatePreferences(bob.ID, models.DigestPreferences{
		DigestFreq:     models.DigestFreqDaily,
		DigestContent:  models.DigestContentGlobal,
		SpikeThreshold: 5.0,
	}); err != nil {
		t.Fatalf("bob prefs: %v", err)
	}

	// Carol: digest disabled
	if err := userStore.UpdatePreferences(carol.ID, models.DigestPreferences{
		DigestFreq:     models.DigestFreqNone,
		DigestContent:  models.DigestContentAll,
		SpikeThreshold: 2.0,
	}); err != nil {
		t.Fatalf("carol prefs: %v", err)
	}

	// ---- Step 3: Seed Redis data ----
	t.Log("Step 3: Seeding Redis with edit wars and stats")

	// Edit war alerts
	seedEditWarAlert(t, rc, "Bitcoin", 200)
	seedEditWarAlert(t, rc, "Pope Francis", 150)
	seedEditWarAlert(t, rc, "OpenAI", 500)

	// Language/edit stats
	dateStr := time.Now().UTC().Format("2006-01-02")
	langKey := fmt.Sprintf("stats:languages:%s", dateStr)
	rc.HSet(ctx, langKey, "en", 120000, "es", 25000, "ja", 18000, "__total__", 163000)

	// ---- Step 4: Trigger daily digest ----
	t.Log("Step 4: Triggering daily digest run")

	sent, skipped, errored := scheduler.RunNow(ctx, "daily")

	t.Logf("Results: sent=%d, skipped=%d, errored=%d", sent, skipped, errored)

	// ---- Step 5: Verify email output ----
	t.Log("Step 5: Verifying sent emails")

	emails := sender.all()

	// Should have sent to Alice and Bob (Carol has digest_frequency=none)
	if len(emails) != 2 {
		t.Fatalf("expected 2 emails, got %d", len(emails))
	}
	if sent != 2 {
		t.Errorf("sent count = %d, want 2", sent)
	}
	if errored != 0 {
		t.Errorf("errored count = %d, want 0", errored)
	}

	// Find Alice's email
	var aliceEmail, bobEmail *capturedEmail
	for i, e := range emails {
		if e.To == "alice@example.com" {
			aliceEmail = &emails[i]
		}
		if e.To == "bob@example.com" {
			bobEmail = &emails[i]
		}
	}

	if aliceEmail == nil {
		t.Fatal("Alice should have received an email")
	}
	if bobEmail == nil {
		t.Fatal("Bob should have received an email")
	}

	// ---- Verify Alice's email content ----
	t.Log("Verifying Alice's email...")

	// Subject should contain "Daily" and top highlight
	if !strings.Contains(aliceEmail.Subject, "Daily") {
		t.Errorf("Alice subject missing 'Daily': %s", aliceEmail.Subject)
	}

	// Alice wants "all" content → should have watchlist + global sections
	aliceChecks := []struct {
		label string
		want  string
	}{
		{"header", "WikiSurge"},
		{"watchlist heading", "Your Watchlist"},
		{"watchlist page", "Bitcoin"},
		{"global heading", "Global Highlights"},
		{"edit war page", "OpenAI"},
		{"stats heading", "Fun Stats"},
		{"total edits", "163.0K"},
		{"top language", "EN"},
		{"CTA", "See Live Dashboard"},
		{"dashboard url", "https://wikisurge.test"},
		{"unsubscribe", "unsubscribe"},
	}

	for _, c := range aliceChecks {
		if !strings.Contains(aliceEmail.HTML, c.want) {
			t.Errorf("Alice HTML missing %s (looking for %q)", c.label, c.want)
		}
	}

	// ---- Verify Bob's email content ----
	t.Log("Verifying Bob's email...")

	// Bob wants "global" only → should NOT have watchlist section
	if strings.Contains(bobEmail.HTML, "Your Watchlist") {
		t.Error("Bob (global-only) should NOT see Watchlist section")
	}
	if !strings.Contains(bobEmail.HTML, "Global Highlights") {
		t.Error("Bob should see Global Highlights")
	}
	if !strings.Contains(bobEmail.HTML, "Fun Stats") {
		t.Error("Bob should see Fun Stats")
	}

	// ---- Verify Carol was NOT emailed ----
	for _, e := range emails {
		if e.To == "carol@example.com" {
			t.Error("Carol (digest disabled) should NOT receive an email")
		}
	}
}

// TestE2E_WeeklyDigest verifies the weekly period works end-to-end.
func TestE2E_WeeklyDigest(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rc.Close()

	userStore, err := storage.NewUserStore(":memory:")
	if err != nil {
		t.Fatalf("user store: %v", err)
	}
	defer userStore.Close()

	trendingCfg := &config.TrendingConfig{
		Enabled: true, MaxPages: 100, HalfLifeMinutes: 30, PruneInterval: 5 * time.Minute,
	}
	hotPagesCfg := &config.HotPages{
		MaxTracked: 100, PromotionThreshold: 3, WindowDuration: 15 * time.Minute,
		MaxMembersPerPage: 50, HotThreshold: 2, CleanupInterval: 5 * time.Minute,
	}
	trending := storage.NewTrendingScorerForTest(rc, trendingCfg)
	hotPages := storage.NewHotPageTracker(rc, hotPagesCfg)
	alerts := storage.NewRedisAlerts(rc)
	stats := storage.NewStatsTracker(rc)
	defer trending.Stop()
	defer hotPages.Shutdown()

	logger := zerolog.Nop()
	collector := NewCollector(trending, alerts, hotPages, stats, logger)
	sender := &captureSender{}

	scheduler := NewScheduler(collector, sender, userStore, SchedulerConfig{
		DailySendHour:      8,
		WeeklySendDay:      1,
		WeeklySendHour:     8,
		MaxConcurrentSends: 5,
		DashboardURL:       "https://wikisurge.test",
		Enabled:            true,
	}, logger)

	ctx := context.Background()

	// Create weekly user
	user, err := userStore.CreateUser("weekly@example.com", "hashedpw")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := userStore.SetVerified(user.ID); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := userStore.UpdatePreferences(user.ID, models.DigestPreferences{
		DigestFreq:     models.DigestFreqWeekly,
		DigestContent:  models.DigestContentAll,
		SpikeThreshold: 2.0,
	}); err != nil {
		t.Fatalf("prefs: %v", err)
	}

	// Seed multi-day stats (simulating 3 days of activity)
	for i := 0; i < 3; i++ {
		d := time.Now().UTC().Add(-time.Duration(i) * 24 * time.Hour)
		dateStr := d.Format("2006-01-02")
		langKey := fmt.Sprintf("stats:languages:%s", dateStr)
		rc.HSet(ctx, langKey, "en", 50000, "__total__", 50000)
	}

	// Seed edit war
	seedEditWarAlert(t, rc, "Climate_change", 300)

	// Trigger weekly
	sent, _, errored := scheduler.RunNow(ctx, "weekly")

	if sent != 1 {
		t.Errorf("sent = %d, want 1", sent)
	}
	if errored != 0 {
		t.Errorf("errored = %d, want 0", errored)
	}

	emails := sender.all()
	if len(emails) != 1 {
		t.Fatalf("expected 1 email, got %d", len(emails))
	}

	e := emails[0]
	if !strings.Contains(e.Subject, "Weekly") {
		t.Errorf("subject should say Weekly: %s", e.Subject)
	}
	if !strings.Contains(e.HTML, "this week") {
		t.Error("weekly HTML should say 'this week'")
	}
	// Stats should reflect multi-day totals (150K = 3 * 50K)
	if !strings.Contains(e.HTML, "150.0K") {
		t.Errorf("weekly stats should show aggregated total, HTML extract: looking for 150.0K")
	}
}

// TestE2E_EmptyDataNoEmail verifies no emails sent when there's no data.
func TestE2E_EmptyDataNoEmail(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rc.Close()

	userStore, err := storage.NewUserStore(":memory:")
	if err != nil {
		t.Fatalf("user store: %v", err)
	}
	defer userStore.Close()

	trendingCfg := &config.TrendingConfig{
		Enabled: true, MaxPages: 100, HalfLifeMinutes: 30, PruneInterval: 5 * time.Minute,
	}
	hotPagesCfg := &config.HotPages{
		MaxTracked: 100, PromotionThreshold: 3, WindowDuration: 15 * time.Minute,
		MaxMembersPerPage: 50, HotThreshold: 2, CleanupInterval: 5 * time.Minute,
	}
	trending := storage.NewTrendingScorerForTest(rc, trendingCfg)
	hotPages := storage.NewHotPageTracker(rc, hotPagesCfg)
	alerts := storage.NewRedisAlerts(rc)
	stats := storage.NewStatsTracker(rc)
	defer trending.Stop()
	defer hotPages.Shutdown()

	logger := zerolog.Nop()
	collector := NewCollector(trending, alerts, hotPages, stats, logger)
	sender := &captureSender{}

	scheduler := NewScheduler(collector, sender, userStore, SchedulerConfig{
		DailySendHour:      8,
		MaxConcurrentSends: 5,
		DashboardURL:       "https://wikisurge.test",
		Enabled:            true,
	}, logger)

	ctx := context.Background()

	// Create user with daily digest — but no data in Redis
	user, err := userStore.CreateUser("nodata@example.com", "hashedpw")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := userStore.SetVerified(user.ID); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := userStore.UpdatePreferences(user.ID, models.DigestPreferences{
		DigestFreq:     models.DigestFreqDaily,
		DigestContent:  models.DigestContentAll,
		SpikeThreshold: 2.0,
	}); err != nil {
		t.Fatalf("prefs: %v", err)
	}

	sent, skipped, errored := scheduler.RunNow(ctx, "daily")

	if sent != 0 {
		t.Errorf("sent = %d, want 0 (no data = no email)", sent)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if errored != 0 {
		t.Errorf("errored = %d, want 0", errored)
	}

	if len(sender.all()) != 0 {
		t.Error("should not have sent any emails with empty Redis")
	}
}
