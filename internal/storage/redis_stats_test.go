package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupStatsTest(t *testing.T) (*StatsTracker, *redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rc.Close() })

	return NewStatsTracker(rc), rc, mr
}

func TestGetEditCountForPeriod_SingleDay(t *testing.T) {
	st, rc, _ := setupStatsTest(t)
	ctx := context.Background()

	dateStr := time.Now().UTC().Format("2006-01-02")
	langKey := fmt.Sprintf("stats:languages:%s", dateStr)
	rc.HSet(ctx, langKey, "__total__", 5000)

	total, err := st.GetEditCountForPeriod(ctx, time.Now().UTC().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("GetEditCountForPeriod: %v", err)
	}
	if total != 5000 {
		t.Errorf("total = %d, want 5000", total)
	}
}

func TestGetEditCountForPeriod_MultiDay(t *testing.T) {
	st, rc, _ := setupStatsTest(t)
	ctx := context.Background()

	// Seed 3 days of data
	for i := 0; i < 3; i++ {
		d := time.Now().UTC().Add(-time.Duration(i) * 24 * time.Hour)
		dateStr := d.Format("2006-01-02")
		langKey := fmt.Sprintf("stats:languages:%s", dateStr)
		rc.HSet(ctx, langKey, "__total__", 1000*(i+1))
	}

	since := time.Now().UTC().Add(-3 * 24 * time.Hour)
	total, err := st.GetEditCountForPeriod(ctx, since)
	if err != nil {
		t.Fatalf("GetEditCountForPeriod: %v", err)
	}
	// 1000 + 2000 + 3000 = 6000
	if total != 6000 {
		t.Errorf("total = %d, want 6000", total)
	}
}

func TestGetLanguageCountsForPeriod(t *testing.T) {
	st, rc, _ := setupStatsTest(t)
	ctx := context.Background()

	// Day 1: en=100, es=50
	d1 := time.Now().UTC().Format("2006-01-02")
	rc.HSet(ctx, fmt.Sprintf("stats:languages:%s", d1), "en", 100, "es", 50, "__total__", 150)

	// Day 2: en=200, ja=30
	d2 := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02")
	rc.HSet(ctx, fmt.Sprintf("stats:languages:%s", d2), "en", 200, "ja", 30, "__total__", 230)

	since := time.Now().UTC().Add(-2 * 24 * time.Hour)
	counts, grandTotal, err := st.GetLanguageCountsForPeriod(ctx, since)
	if err != nil {
		t.Fatalf("GetLanguageCountsForPeriod: %v", err)
	}

	if grandTotal != 380 {
		t.Errorf("grandTotal = %d, want 380", grandTotal)
	}

	// en should be first (300 total), then es (50), then ja (30)
	if len(counts) < 3 {
		t.Fatalf("expected at least 3 languages, got %d", len(counts))
	}
	if counts[0].Language != "en" || counts[0].Count != 300 {
		t.Errorf("top language: %s=%d, want en=300", counts[0].Language, counts[0].Count)
	}
}

func TestGetEditCountForPeriod_NoDates(t *testing.T) {
	st, _, _ := setupStatsTest(t)
	ctx := context.Background()

	total, err := st.GetEditCountForPeriod(ctx, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("GetEditCountForPeriod: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0 for empty redis", total)
	}
}

// ---------------------------------------------------------------------------
// Per-page daily edit counter tests
// ---------------------------------------------------------------------------

func TestRecordPageEdit_BasicIncrement(t *testing.T) {
	st, _, _ := setupStatsTest(t)
	ctx := context.Background()

	// Record 5 edits for the same page
	for i := 0; i < 5; i++ {
		if err := st.RecordPageEdit(ctx, "Go_(programming_language)"); err != nil {
			t.Fatalf("RecordPageEdit: %v", err)
		}
	}

	count, err := st.GetPageEditCount(ctx, "Go_(programming_language)", time.Now().UTC().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("GetPageEditCount: %v", err)
	}
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}
}

func TestRecordPageEdit_MultiplePages(t *testing.T) {
	st, _, _ := setupStatsTest(t)
	ctx := context.Background()

	st.RecordPageEdit(ctx, "Bitcoin")
	st.RecordPageEdit(ctx, "Bitcoin")
	st.RecordPageEdit(ctx, "Bitcoin")
	st.RecordPageEdit(ctx, "Ethereum")

	btcCount, _ := st.GetPageEditCount(ctx, "Bitcoin", time.Now().UTC().Add(-1*time.Hour))
	ethCount, _ := st.GetPageEditCount(ctx, "Ethereum", time.Now().UTC().Add(-1*time.Hour))

	if btcCount != 3 {
		t.Errorf("Bitcoin count = %d, want 3", btcCount)
	}
	if ethCount != 1 {
		t.Errorf("Ethereum count = %d, want 1", ethCount)
	}
}

func TestGetPageEditCount_MultiDay(t *testing.T) {
	st, rc, _ := setupStatsTest(t)
	ctx := context.Background()

	// Seed 3 days of page data manually
	for i := 0; i < 3; i++ {
		d := time.Now().UTC().Add(-time.Duration(i) * 24 * time.Hour)
		dateStr := d.Format("2006-01-02")
		key := fmt.Sprintf("stats:page:%s:%s", "Bitcoin", dateStr)
		rc.HSet(ctx, key, "edits", 10*(i+1))
	}

	// Query across all 3 days
	since := time.Now().UTC().Add(-3 * 24 * time.Hour)
	total, err := st.GetPageEditCount(ctx, "Bitcoin", since)
	if err != nil {
		t.Fatalf("GetPageEditCount: %v", err)
	}
	// 10 + 20 + 30 = 60
	if total != 60 {
		t.Errorf("total = %d, want 60", total)
	}
}

func TestGetPageEditCount_NoData(t *testing.T) {
	st, _, _ := setupStatsTest(t)
	ctx := context.Background()

	count, err := st.GetPageEditCount(ctx, "NonExistentPage", time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("GetPageEditCount: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 for nonexistent page", count)
	}
}

func TestGetPageEditCount_OnlyCountsRequestedPeriod(t *testing.T) {
	st, rc, _ := setupStatsTest(t)
	ctx := context.Background()

	// Seed today and 5 days ago
	today := time.Now().UTC().Format("2006-01-02")
	fiveDaysAgo := time.Now().UTC().Add(-5 * 24 * time.Hour).Format("2006-01-02")

	rc.HSet(ctx, fmt.Sprintf("stats:page:%s:%s", "Bitcoin", today), "edits", 100)
	rc.HSet(ctx, fmt.Sprintf("stats:page:%s:%s", "Bitcoin", fiveDaysAgo), "edits", 200)

	// Query only last 2 days — should NOT include 5-day-old data
	since := time.Now().UTC().Add(-2 * 24 * time.Hour)
	count, err := st.GetPageEditCount(ctx, "Bitcoin", since)
	if err != nil {
		t.Fatalf("GetPageEditCount: %v", err)
	}
	if count != 100 {
		t.Errorf("count = %d, want 100 (only today's data)", count)
	}

	// Query last 7 days — should include both
	since7 := time.Now().UTC().Add(-7 * 24 * time.Hour)
	count7, err := st.GetPageEditCount(ctx, "Bitcoin", since7)
	if err != nil {
		t.Fatalf("GetPageEditCount 7d: %v", err)
	}
	if count7 != 300 {
		t.Errorf("count = %d, want 300 (today + 5 days ago)", count7)
	}
}

func TestRecordPageEdit_TTL(t *testing.T) {
	st, rc, mr := setupStatsTest(t)
	ctx := context.Background()

	st.RecordPageEdit(ctx, "TestPage")

	dateStr := time.Now().UTC().Format("2006-01-02")
	key := fmt.Sprintf("stats:page:%s:%s", "TestPage", dateStr)

	// Verify key exists
	val, err := rc.HGet(ctx, key, "edits").Result()
	if err != nil {
		t.Fatalf("key should exist: %v", err)
	}
	if val != "1" {
		t.Errorf("edits = %q, want 1", val)
	}

	// Fast-forward past TTL (8 days + 1 hour)
	mr.FastForward(193 * time.Hour)

	// Key should have expired
	exists, _ := rc.Exists(ctx, key).Result()
	if exists != 0 {
		t.Error("key should have expired after 8+ days")
	}
}

func TestRecordPageEdit_SpecialCharactersInTitle(t *testing.T) {
	st, _, _ := setupStatsTest(t)
	ctx := context.Background()

	// Titles with colons, parens, Unicode
	titles := []string{
		"C++",
		"Go_(programming_language)",
		"São_Paulo",
		"Category:Living_people",
		"User:Admin/sandbox",
	}

	for _, title := range titles {
		if err := st.RecordPageEdit(ctx, title); err != nil {
			t.Errorf("RecordPageEdit(%q) failed: %v", title, err)
		}
	}

	for _, title := range titles {
		count, err := st.GetPageEditCount(ctx, title, time.Now().UTC().Add(-1*time.Hour))
		if err != nil {
			t.Errorf("GetPageEditCount(%q) failed: %v", title, err)
		}
		if count != 1 {
			t.Errorf("GetPageEditCount(%q) = %d, want 1", title, count)
		}
	}
}
