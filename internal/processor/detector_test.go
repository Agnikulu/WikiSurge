package processor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSpikeDetectionScenarios tests various spike detection scenarios
func TestSpikeDetectionScenarios(t *testing.T) {
	// Setup Redis client for testing (assumes Redis running locally)
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})
	defer redisClient.Close()

	// Clear test database
	redisClient.FlushDB(context.Background())

	// Setup configuration
	cfg := &config.Config{
		Redis: config.Redis{
			HotPages: config.HotPages{
				MaxTracked:         100,
				PromotionThreshold: 2,
				WindowDuration:     time.Hour,
				MaxMembersPerPage:  50,
				HotThreshold:       2,
				CleanupInterval:    5 * time.Minute,
			},
		},
	}

	// Setup components
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	hotPageTracker := storage.NewHotPageTracker(redisClient, &cfg.Redis.HotPages)
	spikeDetector := NewSpikeDetector(hotPageTracker, redisClient, cfg, logger)

	t.Run("Scenario 1: Clear spike detection", func(t *testing.T) {
		ctx := context.Background()
		pageTitle := "Test_Page_Clear_Spike"

		// Simulate normal activity: 1 edit/hour for 1 hour (spread over time)
		baseTime := time.Now().Add(-time.Hour)
		for i := 0; i < 4; i++ {
			edit := &models.WikipediaEdit{
				Title:     pageTitle,
				User:      "user1",
				Timestamp: baseTime.Add(time.Duration(i) * 15 * time.Minute).Unix(),
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 150},
				Bot: false,
			}
			err := spikeDetector.ProcessEdit(ctx, edit)
			require.NoError(t, err)
		}

		// Wait for promotion to hot page
		time.Sleep(100 * time.Millisecond)

		// Now simulate spike: 20 edits in 5 minutes
		spikeStartTime := time.Now()
		for i := 0; i < 20; i++ {
			edit := &models.WikipediaEdit{
				Title:     pageTitle,
				User:      "spiker" + string(rune(i%5)), // 5 different users
				Timestamp: spikeStartTime.Add(time.Duration(i) * 15 * time.Second).Unix(),
					Length: struct {
						Old int `json:"old"`
						New int `json:"new"`
					}{Old: 100, New: 200},
					Bot: false,
			}
			err := spikeDetector.ProcessEdit(ctx, edit)
			require.NoError(t, err)
		}

		// Check if alerts were generated
		alerts, err := spikeDetector.GetRecentAlerts(ctx, spikeStartTime.Add(-time.Minute), 10)
		require.NoError(t, err)
		assert.True(t, len(alerts) > 0, "Expected spike to be detected")

		if len(alerts) > 0 {
			alert := alerts[0]
			assert.Equal(t, pageTitle, alert.PageTitle)
			assert.True(t, alert.SpikeRatio >= 5.0, "Expected high spike ratio, got %f", alert.SpikeRatio)
			assert.Contains(t, []string{"medium", "high", "critical"}, alert.Severity,
				"Expected significant severity, got %s (ratio: %f)", alert.Severity, alert.SpikeRatio)
		}
	})

	t.Run("Scenario 2: Gradual increase - no spike", func(t *testing.T) {
		ctx := context.Background()
		pageTitle := "Test_Page_Gradual_Increase"

		// Simulate gradual increase from 1/hour to 10/hour over 30 minutes
		baseTime := time.Now().Add(-30 * time.Minute)
		totalEdits := 0
		for minute := 0; minute < 30; minute++ {
			// Gradually increase edits per minute
			editsThisMinute := 1 + (minute * 9 / 30) // Linear increase to ~10 per minute
			for edit := 0; edit < editsThisMinute; edit++ {
				totalEdits++
				editObj := &models.WikipediaEdit{
					Title:     pageTitle,
					User:      "gradual_user" + string(rune(totalEdits%3)),
					Timestamp: baseTime.Add(time.Duration(minute) * time.Minute).Add(time.Duration(edit) * 10 * time.Second).Unix(),
						Length: struct {
							Old int `json:"old"`
							New int `json:"new"`
						}{Old: 100, New: 120},
						Bot: false,
				}
				err := spikeDetector.ProcessEdit(ctx, editObj)
				require.NoError(t, err)
			}
		}

		// Check alerts - should be minimal or none for gradual increase
		alerts, err := spikeDetector.GetRecentAlerts(ctx, baseTime, 10)
		require.NoError(t, err)
		
		// Should have no high-severity spikes (gradual increase should not trigger)
		highSeverityAlerts := 0
		for _, alert := range alerts {
			if alert.PageTitle == pageTitle && (alert.Severity == "high" || alert.Severity == "critical") {
				highSeverityAlerts++
			}
		}
		assert.Equal(t, 0, highSeverityAlerts, "Gradual increase should not trigger high-severity spikes")
	})

	t.Run("Scenario 3: False positive prevention", func(t *testing.T) {
		ctx := context.Background()
		pageTitle := "Test_Page_False_Positive"

		// Simulate already high hourly rate (12 edits in last hour)
		baseTime := time.Now().Add(-time.Hour)
		for i := 0; i < 12; i++ {
			edit := &models.WikipediaEdit{
				Title:     pageTitle,
				User:      "regular_user" + string(rune(i%4)),
				Timestamp: baseTime.Add(time.Duration(i) * 5 * time.Minute).Unix(),
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 110},
				Bot: false,
			}
			err := spikeDetector.ProcessEdit(ctx, edit)
			require.NoError(t, err)
		}

		// Wait for page to become hot
		time.Sleep(100 * time.Millisecond)

		// Now 3 edits in 5 minutes (which would normally be low)
		recent := time.Now()
		for i := 0; i < 3; i++ {
			edit := &models.WikipediaEdit{
				Title:     pageTitle,
				User:      "recent_user",
				Timestamp: recent.Add(time.Duration(i) * time.Minute).Unix(),
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 105},
				Bot: false,
			}
			err := spikeDetector.ProcessEdit(ctx, edit)
			require.NoError(t, err)
		}

		// Check alerts - should not trigger spike (ratio ~1.25x, below threshold)
		alerts, err := spikeDetector.GetRecentAlerts(ctx, recent.Add(-time.Minute), 10)
		require.NoError(t, err)

		spikeAlerts := 0
		for _, alert := range alerts {
			if alert.PageTitle == pageTitle {
				spikeAlerts++
			}
		}
		assert.Equal(t, 0, spikeAlerts, "Should not detect spike when recent activity matches baseline")
	})

	t.Run("Scenario 4: Minimum threshold test", func(t *testing.T) {
		ctx := context.Background()
		pageTitle := "Test_Page_Minimum_Threshold"

		// Very low hourly rate (1 edit in last hour)
		hourAgo := time.Now().Add(-time.Hour)
		edit := &models.WikipediaEdit{
			Title:     pageTitle,
			User:      "minimal_user",
			Timestamp: hourAgo.Unix(),
			Length: struct {
				Old int `json:"old"`
				New int `json:"new"`
			}{Old: 100, New: 101},
			Bot: false,
		}
		err := spikeDetector.ProcessEdit(ctx, edit)
		require.NoError(t, err)

		// Wait for promotion
		time.Sleep(100 * time.Millisecond)

		// Only 2 edits in 5 minutes (below minimum threshold of 3)
		recent := time.Now()
		for i := 0; i < 2; i++ {
			edit := &models.WikipediaEdit{
				Title:     pageTitle,
				User:      "threshold_user",
				Timestamp: recent.Add(time.Duration(i) * time.Minute).Unix(),
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 120},
				Bot: false,
			}
			err := spikeDetector.ProcessEdit(ctx, edit)
			require.NoError(t, err)
		}

		// Check alerts - should not trigger (below minimum edits)
		alerts, err := spikeDetector.GetRecentAlerts(ctx, recent.Add(-time.Minute), 10)
		require.NoError(t, err)

		spikeAlerts := 0
		for _, alert := range alerts {
			if alert.PageTitle == pageTitle {
				spikeAlerts++
			}
		}
		assert.Equal(t, 0, spikeAlerts, "Should not detect spike below minimum edit threshold")
	})
}

// TestSeverityCalculation tests the severity calculation logic
func TestSeverityCalculation(t *testing.T) {
	cfg := &config.Config{}
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	detector := NewSpikeDetector(nil, nil, cfg, logger)

	tests := []struct {
		ratio    float64
		expected string
	}{
		{4.9, "low"},     // Below threshold, but this shouldn't reach severity calc
		{5.0, "low"},     // Exactly at threshold
		{9.9, "low"},     // Just below medium
		{10.0, "medium"}, // Exactly medium
		{19.9, "medium"}, // Just below high
		{20.0, "high"},   // Exactly high
		{49.9, "high"},   // Just below critical
		{50.0, "critical"}, // Exactly critical
		{100.0, "critical"}, // Way above critical
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("ratio_%.1f", test.ratio), func(t *testing.T) {
			severity := detector.calculateSeverity(test.ratio)
			assert.Equal(t, test.expected, severity)
		})
	}
}

// BenchmarkSpikeDetection benchmarks the spike detection performance
func BenchmarkSpikeDetection(b *testing.B) {
	// Setup
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1,
	})
	defer redisClient.Close()

	cfg := &config.Config{
		Redis: config.Redis{
			HotPages: config.HotPages{
				MaxTracked:         1000,
				PromotionThreshold: 2,
				WindowDuration:     time.Hour,
				MaxMembersPerPage:  100,
				HotThreshold:       2,
				CleanupInterval:    5 * time.Minute,
			},
		},
	}

	logger := zerolog.New(nil).Level(zerolog.Disabled)
	hotPageTracker := storage.NewHotPageTracker(redisClient, &cfg.Redis.HotPages)
	spikeDetector := NewSpikeDetector(hotPageTracker, redisClient, cfg, logger)

	edit := &models.WikipediaEdit{
		Title:     "Benchmark_Page",
		User:      "benchmark_user",
		Timestamp: time.Now().Unix(),
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 150},
		Bot: false,
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		edit.Timestamp = time.Now().Add(time.Duration(i) * time.Second).Unix()
		err := spikeDetector.ProcessEdit(ctx, edit)
		if err != nil {
			b.Fatal(err)
		}
	}
}