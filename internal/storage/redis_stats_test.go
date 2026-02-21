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
