// Quick preview tool — renders the digest email with mock data and serves it.
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/digest"
	"github.com/Agnikulu/WikiSurge/internal/models"
)

func main() {
	data := &digest.DigestData{
		Period:      "daily",
		PeriodStart: time.Now().Add(-24 * time.Hour),
		PeriodEnd:   time.Now(),
		GlobalHighlights: []digest.GlobalHighlight{
			{Rank: 1, Title: "2025 Turkish earthquake", EditCount: 847, EventType: "edit_war", Summary: "Massive revert war over casualty figures — 23 editors involved",
				EditorCount: 23, Editors: []string{"GeoTracker99", "SeismicFacts", "TurkWatcher", "ReliefNow", "FactCheck2025"},
				RevertCount: 34, Severity: "critical", LLMSummary: "A fierce editorial conflict has erupted over the reported casualty figures, with two opposing camps: one citing Turkish government sources and the other relying on international relief organization estimates. Editors are repeatedly reverting each other's numbers, with the dispute centering on whether to use 'confirmed' vs 'estimated' death tolls.", ContentArea: "Casualty figures and sourcing"},
			{Rank: 2, Title: "Pope Francis", EditCount: 312, EventType: "edit_war", Summary: "Edit war over legacy section after Vatican announcement",
				EditorCount: 8, Editors: []string{"VaticanWatch", "CatholicEdit", "HistoryBuff42"},
				RevertCount: 12, Severity: "high", LLMSummary: "Disagreement over how to characterize Pope Francis's legacy following a major Vatican announcement. Conservative and progressive editors are clashing over the framing of his papacy's impact on church doctrine.", ContentArea: "Legacy and doctrinal impact"},
			{Rank: 3, Title: "OpenAI", EditCount: 589, EventType: "edit_war", Summary: "Conflicting edits on board restructuring details",
				EditorCount: 15, Editors: []string{"TechInsider", "AIWatcher", "BoardroomLeaks", "NeutralPOV"},
				RevertCount: 22, Severity: "high", LLMSummary: "Multiple editors are disputing the characterization of OpenAI's board restructuring, with disagreements over whether certain executive departures were voluntary or forced. Sources from competing news outlets are being used to support opposing narratives.", ContentArea: "Corporate governance"},
			{Rank: 4, Title: "Boeing 737 MAX", EditCount: 156, EventType: "edit_war", Summary: "Dispute over safety record phrasing",
				EditorCount: 6, Editors: []string{"AviationPro", "SafetyFirst", "BoeingFan"},
				RevertCount: 8, Severity: "moderate", LLMSummary: "Ongoing dispute about how to phrase the safety record section, particularly around whether recent incidents should be characterized as 'design flaws' or 'operational issues'.", ContentArea: "Safety record"},
		},
		EditWarHighlights: []digest.GlobalHighlight{
			{Rank: 1, Title: "2025 Turkish earthquake", EditCount: 847, EventType: "edit_war",
				EditorCount: 23, Editors: []string{"GeoTracker99", "SeismicFacts", "TurkWatcher", "ReliefNow", "FactCheck2025"},
				RevertCount: 34, Severity: "critical", LLMSummary: "A fierce editorial conflict has erupted over the reported casualty figures, with two opposing camps: one citing Turkish government sources and the other relying on international relief organization estimates. Editors are repeatedly reverting each other's numbers, with the dispute centering on whether to use 'confirmed' vs 'estimated' death tolls.", ContentArea: "Casualty figures and sourcing",
				Summary: "A fierce editorial conflict has erupted over the reported casualty figures, with two opposing camps: one citing Turkish government sources and the other relying on international relief organization estimates."},
			{Rank: 2, Title: "Pope Francis", EditCount: 312, EventType: "edit_war",
				EditorCount: 8, Editors: []string{"VaticanWatch", "CatholicEdit", "HistoryBuff42"},
				RevertCount: 12, Severity: "high", LLMSummary: "Disagreement over how to characterize Pope Francis's legacy following a major Vatican announcement. Conservative and progressive editors are clashing over the framing of his papacy's impact on church doctrine.", ContentArea: "Legacy and doctrinal impact",
				Summary: "Disagreement over how to characterize Pope Francis's legacy following a major Vatican announcement."},
			{Rank: 3, Title: "OpenAI", EditCount: 589, EventType: "edit_war",
				EditorCount: 15, Editors: []string{"TechInsider", "AIWatcher", "BoardroomLeaks", "NeutralPOV"},
				RevertCount: 22, Severity: "high", LLMSummary: "Multiple editors are disputing the characterization of OpenAI's board restructuring, with disagreements over whether certain executive departures were voluntary or forced.", ContentArea: "Corporate governance",
				Summary: "Multiple editors are disputing the characterization of OpenAI's board restructuring."},
			{Rank: 4, Title: "Boeing 737 MAX", EditCount: 156, EventType: "edit_war",
				EditorCount: 6, Editors: []string{"AviationPro", "SafetyFirst", "BoeingFan"},
				RevertCount: 8, Severity: "moderate", LLMSummary: "Ongoing dispute about how to phrase the safety record section, particularly around whether recent incidents should be characterized as 'design flaws' or 'operational issues'.", ContentArea: "Safety record",
				Summary: "Ongoing dispute about how to phrase the safety record section."},
		},
		TrendingHighlights: []digest.GlobalHighlight{
			{Rank: 1, Title: "Taylor Swift", EditCount: 204, EventType: "trending", Summary: "Trending after surprise album drop — discography updates"},
			{Rank: 2, Title: "Mars Rover Curiosity", EditCount: 89, EventType: "trending", Summary: "Trending (score: 1250) — new mineral discovery announcement"},
			{Rank: 3, Title: "2026 FIFA World Cup", EditCount: 156, EventType: "trending", Summary: "Trending (score: 980) — host city updates"},
		},
		WatchlistEvents: []digest.WatchlistEvent{
			{Title: "Bitcoin", EditCount: 267, IsNotable: true, SpikeRatio: 4.2, EventType: "edit_war", Summary: "Edit war over ETF price impact section"},
			{Title: "Ethereum", EditCount: 89, IsNotable: true, SpikeRatio: 2.1, EventType: "trending", Summary: "Trending — merge anniversary edits"},
			{Title: "Solana", EditCount: 12, IsNotable: false, EventType: "active", Summary: "Minor copyedits"},
			{Title: "Cardano", EditCount: 3, IsNotable: false, EventType: "quiet", Summary: "Quiet — no significant changes"},
		},
		Stats: digest.FunStats{
			TotalEdits: 2_487_319,
			EditWars:   17,
			TopLanguages: []digest.LanguageStat{
				{Language: "en", Count: 892000, Percentage: 35.9},
				{Language: "de", Count: 412000, Percentage: 16.6},
				{Language: "ja", Count: 298000, Percentage: 12.0},
				{Language: "es", Count: 245000, Percentage: 9.8},
				{Language: "fr", Count: 198000, Percentage: 8.0},
			},
		},
	}

	user := &models.User{
		Email:         "agnik@example.com",
		DigestContent: models.DigestContentAll,
		UnsubToken:    "demo-unsub-token",
		Watchlist:     []string{"Bitcoin", "Ethereum", "Solana", "Cardano"},
	}

	_, html, err := digest.RenderDigestEmail(data, user, "https://wikisurge.net", "demo-unsub-token")
	if err != nil {
		fmt.Fprintf(os.Stderr, "render error: %v\n", err)
		os.Exit(1)
	}

	// Also render a weekly version
	weeklyData := &digest.DigestData{
		Period:      "weekly",
		PeriodStart: time.Now().Add(-7 * 24 * time.Hour),
		PeriodEnd:   time.Now(),
		GlobalHighlights: data.GlobalHighlights,
		WatchlistEvents:  data.WatchlistEvents,
		Stats: digest.FunStats{
			TotalEdits: 17_420_000,
			EditWars:   94,
			TopLanguages: data.Stats.TopLanguages,
		},
	}

	_, weeklyHTML, err := digest.RenderDigestEmail(weeklyData, user, "https://wikisurge.net", "demo-unsub-token")
	if err != nil {
		fmt.Fprintf(os.Stderr, "render weekly error: %v\n", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	})
	mux.HandleFunc("/weekly", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, weeklyHTML)
	})

	fmt.Println("📧 Email preview server running:")
	fmt.Println("   Daily  → http://localhost:9999")
	fmt.Println("   Weekly → http://localhost:9999/weekly")
	if err := http.ListenAndServe(":9999", mux); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
