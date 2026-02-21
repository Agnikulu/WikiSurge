package digest

import (
	"strings"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/models"
)

func TestRenderDigestEmail_Basic(t *testing.T) {
	data := &DigestData{
		Period:      "daily",
		PeriodStart: time.Now().Add(-24 * time.Hour),
		PeriodEnd:   time.Now(),
		GlobalHighlights: []GlobalHighlight{
			{Rank: 1, Title: "2026 Turkish earthquake", EditCount: 1200, EventType: "edit_war", Summary: "Edit war detected"},
			{Rank: 2, Title: "Pope Francis", EditCount: 800, EventType: "edit_war", Summary: "Edit war detected"},
			{Rank: 3, Title: "Boeing", EditCount: 50, EventType: "edit_war", Summary: "Edit war detected"},
		},
		WatchlistEvents: []WatchlistEvent{
			{Title: "Bitcoin", EditCount: 47, IsNotable: true, EventType: "edit_war", Summary: "Edit war detected"},
			{Title: "Taylor Swift", EditCount: 4, IsNotable: false, EventType: "quiet", Summary: "4 edits (quiet)"},
		},
		Stats: FunStats{
			TotalEdits: 2400000,
			EditWars:   17,
			TopLanguages: []LanguageStat{
				{Language: "en", Count: 980000, Percentage: 41.0},
				{Language: "ja", Count: 192000, Percentage: 8.0},
			},
		},
	}

	user := &models.User{
		Email:          "alice@example.com",
		DigestContent:  models.DigestContentAll,
		SpikeThreshold: 2.0,
		UnsubToken:     "test-unsub-token-123",
	}

	subject, html, err := RenderDigestEmail(data, user, "https://wikisurge.app", user.UnsubToken)
	if err != nil {
		t.Fatalf("RenderDigestEmail: %v", err)
	}

	if !strings.Contains(subject, "2026 Turkish earthquake") {
		t.Errorf("subject should mention top highlight, got: %s", subject)
	}
	if !strings.Contains(subject, "Daily") {
		t.Errorf("subject should contain period, got: %s", subject)
	}

	checks := []struct {
		label string
		want  string
	}{
		{"header", "WikiSurge"},
		{"period", "Daily Digest"},
		{"watchlist heading", "Your Watchlist"},
		{"notable event", "Bitcoin"},
		{"edit war summary", "Edit war detected"},
		{"quiet event", "Taylor Swift"},
		{"global heading", "Global Highlights"},
		{"global #1", "2026 Turkish earthquake"},
		{"global #3", "Boeing"},
		{"stats - total", "2.4M"},
		{"stats - wars", "17"},
		{"stats - language", "EN"},
		{"CTA button", "See Live Dashboard"},
		{"unsubscribe link", "test-unsub-token-123"},
		{"dashboard link", "https://wikisurge.app"},
	}

	for _, c := range checks {
		if !strings.Contains(html, c.want) {
			t.Errorf("HTML missing %s (looking for %q)", c.label, c.want)
		}
	}
}

func TestRenderDigestEmail_WeeklyPeriod(t *testing.T) {
	data := &DigestData{
		Period:      "weekly",
		PeriodStart: time.Now().Add(-7 * 24 * time.Hour),
		PeriodEnd:   time.Now(),
		Stats:       FunStats{TotalEdits: 100},
	}

	user := &models.User{
		Email:         "bob@example.com",
		DigestContent: models.DigestContentGlobal,
		UnsubToken:    "t",
	}

	subject, html, err := RenderDigestEmail(data, user, "http://localhost", "t")
	if err != nil {
		t.Fatalf("RenderDigestEmail: %v", err)
	}

	if !strings.Contains(subject, "Weekly") {
		t.Errorf("weekly subject should say Weekly, got: %s", subject)
	}
	if !strings.Contains(html, "this week") {
		t.Error("weekly HTML should say 'this week'")
	}
}

func TestRenderDigestEmail_WatchlistOnly(t *testing.T) {
	data := &DigestData{
		Period:      "daily",
		PeriodStart: time.Now().Add(-24 * time.Hour),
		PeriodEnd:   time.Now(),
		GlobalHighlights: []GlobalHighlight{
			{Rank: 1, Title: "ShouldBeHidden", EditCount: 999, EventType: "edit_war", Summary: "Edit war detected"},
		},
		Stats: FunStats{TotalEdits: 100},
	}

	user := &models.User{
		Email:         "watchonly@example.com",
		DigestContent: models.DigestContentWatchlist,
		UnsubToken:    "t",
	}

	_, html, err := RenderDigestEmail(data, user, "http://localhost", "t")
	if err != nil {
		t.Fatalf("RenderDigestEmail: %v", err)
	}

	if strings.Contains(html, "Global Highlights") {
		t.Error("watchlist-only user should not see Global Highlights section")
	}
}

func TestRenderDigestEmail_GlobalOnly(t *testing.T) {
	data := &DigestData{
		Period:      "daily",
		PeriodStart: time.Now().Add(-24 * time.Hour),
		PeriodEnd:   time.Now(),
		WatchlistEvents: []WatchlistEvent{
			{Title: "ShouldBeHidden", IsNotable: true, EventType: "edit_war", Summary: "x"},
		},
		GlobalHighlights: []GlobalHighlight{
			{Rank: 1, Title: "Visible", EditCount: 100, EventType: "edit_war", Summary: "Edit war detected"},
		},
		Stats: FunStats{TotalEdits: 100},
	}

	user := &models.User{
		Email:         "globalonly@example.com",
		DigestContent: models.DigestContentGlobal,
		UnsubToken:    "t",
	}

	_, html, err := RenderDigestEmail(data, user, "http://localhost", "t")
	if err != nil {
		t.Fatalf("RenderDigestEmail: %v", err)
	}

	if strings.Contains(html, "Your Watchlist") {
		t.Error("global-only user should not see Watchlist section")
	}
	if !strings.Contains(html, "Global Highlights") {
		t.Error("global-only user should see Global Highlights")
	}
}

func TestRenderDigestEmail_EmptyData(t *testing.T) {
	data := &DigestData{
		Period:      "daily",
		PeriodStart: time.Now().Add(-24 * time.Hour),
		PeriodEnd:   time.Now(),
		Stats:       FunStats{},
	}

	user := &models.User{
		Email:         "empty@example.com",
		DigestContent: models.DigestContentAll,
		UnsubToken:    "t",
	}

	_, html, err := RenderDigestEmail(data, user, "http://localhost", "t")
	if err != nil {
		t.Fatalf("RenderDigestEmail: %v", err)
	}

	if !strings.Contains(html, "WikiSurge") {
		t.Error("should still contain WikiSurge header")
	}
	if !strings.Contains(html, "Fun Stats") {
		t.Error("should still contain stats section")
	}
}

func TestFormatNumber(t *testing.T) {
	fns := TemplateFuncs()
	formatNum := fns["formatNumber"].(func(int64) string)

	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0K"},
		{2400000, "2.4M"},
		{50, "50"},
	}

	for _, tt := range tests {
		got := formatNum(tt.input)
		if got != tt.want {
			t.Errorf("formatNumber(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEventIcon(t *testing.T) {
	fns := TemplateFuncs()
	iconFn := fns["eventIcon"].(func(string) string)

	if iconFn("spike") != "📈" {
		t.Error("spike icon wrong")
	}
	if iconFn("edit_war") != "⚔️" {
		t.Error("edit_war icon wrong")
	}
	if iconFn("unknown") != "📄" {
		t.Error("unknown icon wrong")
	}
}
