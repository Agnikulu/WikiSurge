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

	subject, html, err := RenderDigestEmail(data, user, "https://wikisurge.net", user.UnsubToken)
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
		{"global heading", "Most Popular Edit Wars"},
		{"global #1", "2026 Turkish earthquake"},
		{"global #3", "Boeing"},
		{"stats - total", "2.4M"},
		{"stats - wars", "17"},
		{"stats - language", "EN"},
		{"CTA button", "See Live Dashboard"},
		{"unsubscribe link", "test-unsub-token-123"},
		{"dashboard link", "https://wikisurge.net"},
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

	if strings.Contains(html, "Most Popular Edit Wars") || strings.Contains(html, "Most Trending Pages") {
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
	if !strings.Contains(html, "Most Popular Edit Wars") {
		t.Error("global-only user should see edit wars section")
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

func TestRenderDigestEmail_EditWarDetails(t *testing.T) {
	data := &DigestData{
		Period:      "daily",
		PeriodStart: time.Now().Add(-24 * time.Hour),
		PeriodEnd:   time.Now(),
		EditWarHighlights: []GlobalHighlight{
			{
				Rank: 1, Title: "Climate_change", EditCount: 500, EventType: "edit_war",
				EditorCount: 12, Editors: []string{"Alice", "Bob", "Charlie", "Diana", "Eve"},
				RevertCount: 18, Severity: "critical",
				LLMSummary:  "Editors are clashing over the attribution of recent temperature data to human activity vs natural cycles.",
				ContentArea: "Attribution of climate data",
				Summary:     "Editors are clashing over the attribution of recent temperature data to human activity vs natural cycles.",
			},
		},
		TrendingHighlights: []GlobalHighlight{
			{Rank: 1, Title: "Mars Rover", EditCount: 80, EventType: "trending", Summary: "Trending (score: 900)"},
		},
		Stats: FunStats{TotalEdits: 500000},
	}

	user := &models.User{
		Email:         "test@example.com",
		DigestContent: models.DigestContentAll,
		UnsubToken:    "t",
	}

	_, html, err := RenderDigestEmail(data, user, "http://localhost", "t")
	if err != nil {
		t.Fatalf("RenderDigestEmail: %v", err)
	}

	checks := []struct {
		label string
		want  string
	}{
		{"edit wars heading", "Most Popular Edit Wars"},
		{"trending heading", "Most Trending Pages"},
		{"edit war title", "Climate_change"},
		{"LLM summary", "attribution of recent temperature data"},
		{"editor count", "12 editors"},
		{"revert count", "18 reverts"},
		{"severity", "CRITICAL"},
		{"content area", "Attribution of climate data"},
		{"editors list", "Alice"},
		{"trending title", "Mars Rover"},
	}

	for _, c := range checks {
		if !strings.Contains(html, c.want) {
			t.Errorf("HTML missing %s (looking for %q)", c.label, c.want)
		}
	}
}

func TestRenderDigestEmail_EditWarWithoutLLM(t *testing.T) {
	data := &DigestData{
		Period:      "daily",
		PeriodStart: time.Now().Add(-24 * time.Hour),
		PeriodEnd:   time.Now(),
		EditWarHighlights: []GlobalHighlight{
			{Rank: 1, Title: "Bitcoin", EditCount: 200, EventType: "edit_war", Summary: "Edit war detected"},
		},
		Stats: FunStats{TotalEdits: 100},
	}

	user := &models.User{
		Email:         "test@example.com",
		DigestContent: models.DigestContentAll,
		UnsubToken:    "t",
	}

	_, html, err := RenderDigestEmail(data, user, "http://localhost", "t")
	if err != nil {
		t.Fatalf("RenderDigestEmail: %v", err)
	}

	if !strings.Contains(html, "Most Popular Edit Wars") {
		t.Error("should show edit wars section")
	}
	if !strings.Contains(html, "Bitcoin") {
		t.Error("should show Bitcoin")
	}
	if !strings.Contains(html, "Edit war detected") {
		t.Error("should show fallback summary")
	}
}

func TestRenderDigestEmail_TrendingOnly(t *testing.T) {
	data := &DigestData{
		Period:      "daily",
		PeriodStart: time.Now().Add(-24 * time.Hour),
		PeriodEnd:   time.Now(),
		TrendingHighlights: []GlobalHighlight{
			{Rank: 1, Title: "Taylor Swift", EditCount: 200, EventType: "trending", Summary: "Album drop edits"},
		},
		Stats: FunStats{TotalEdits: 100},
	}

	user := &models.User{
		Email:         "test@example.com",
		DigestContent: models.DigestContentGlobal,
		UnsubToken:    "t",
	}

	_, html, err := RenderDigestEmail(data, user, "http://localhost", "t")
	if err != nil {
		t.Fatalf("RenderDigestEmail: %v", err)
	}

	if strings.Contains(html, "Most Popular Edit Wars") {
		t.Error("should NOT show edit wars section when no edit wars exist")
	}
	if !strings.Contains(html, "Most Trending Pages") {
		t.Error("should show trending section")
	}
	if !strings.Contains(html, "Taylor Swift") {
		t.Error("should show Taylor Swift")
	}
}

func TestSeverityBadge(t *testing.T) {
	fns := TemplateFuncs()
	fn := fns["severityBadge"].(func(string) string)

	tests := []struct {
		input string
		want  string
	}{
		{"critical", "CRITICAL"},
		{"high", "HIGH"},
		{"moderate", "MODERATE"},
		{"low", "LOW"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		got := fn(tt.input)
		if tt.want != "" && !strings.Contains(got, tt.want) {
			t.Errorf("severityBadge(%q) = %q, want contains %q", tt.input, got, tt.want)
		}
		if tt.want == "" && got != "" {
			t.Errorf("severityBadge(%q) = %q, want empty", tt.input, got)
		}
	}
}

func TestJoinEditors(t *testing.T) {
	fns := TemplateFuncs()
	fn := fns["joinEditors"].(func([]string, int) string)

	tests := []struct {
		editors  []string
		maxShow  int
		want     string
	}{
		{nil, 3, ""},
		{[]string{"Alice"}, 3, "Alice"},
		{[]string{"Alice", "Bob"}, 3, "Alice, Bob"},
		{[]string{"Alice", "Bob", "Charlie", "Diana", "Eve"}, 3, "Alice, Bob, Charlie +2 more"},
	}

	for _, tt := range tests {
		got := fn(tt.editors, tt.maxShow)
		if got != tt.want {
			t.Errorf("joinEditors(%v, %d) = %q, want %q", tt.editors, tt.maxShow, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	fns := TemplateFuncs()
	fn := fns["truncate"].(func(string, int) string)

	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"short", 10, "short"},
		{"this is a long string", 10, "this is..."},
		{"abc", 3, "abc"},
		{"abcd", 4, "abcd"},
	}

	for _, tt := range tests {
		got := fn(tt.input, tt.n)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
		}
	}
}
