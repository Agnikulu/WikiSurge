package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/kafka"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/processor"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	kafkago "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSpikeDetectorIntegration tests the full pipeline from Kafka to Redis alerts
func TestSpikeDetectorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup configuration
	cfg := &config.Config{
		Kafka: config.Kafka{
			Brokers:       []string{"localhost:9092"},
			ConsumerGroup: "test-spike-detector",
		},
		Redis: config.Redis{
			URL: "redis://localhost:6379/2", // Use test database
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

	// Setup Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   2, // Use test database
	})
	defer redisClient.Close()

	// Check Redis is available
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		t.Skip("Redis not available for integration tests")
	}

	// Check Kafka is available
	conn, err := net.DialTimeout("tcp", "localhost:9092", 2*time.Second)
	if err != nil {
		t.Skip("Kafka not available for integration tests")
	}
	conn.Close()

	// Clear test database
	err = redisClient.FlushDB(context.Background()).Err()
	require.NoError(t, err)

	// Test Redis connection
	err = redisClient.Ping(context.Background()).Err()
	require.NoError(t, err, "Redis must be available for integration tests")

	// Setup logger
	logger := zerolog.New(nil).Level(zerolog.InfoLevel)

	// Setup components
	hotPageTracker := storage.NewHotPageTracker(redisClient, &cfg.Redis.HotPages)
	spikeDetector := processor.NewSpikeDetector(hotPageTracker, redisClient, cfg, logger)

	// Setup Kafka producer for test events
	writer := &kafkago.Writer{
		Addr:     kafkago.TCP("localhost:9092"),
		Topic:    "wikipedia.edits",
		Balancer: &kafkago.LeastBytes{},
	}
	defer writer.Close()

	// Setup Kafka consumer
	consumerCfg := kafka.ConsumerConfig{
		Brokers:        cfg.Kafka.Brokers,
		Topic:          "wikipedia.edits",
		GroupID:        cfg.Kafka.ConsumerGroup,
		StartOffset:    kafkago.LastOffset, // Start from latest for test
		MinBytes:       1,
		MaxBytes:       1024 * 1024,
		CommitInterval: 100 * time.Millisecond,
		MaxWait:        100 * time.Millisecond,
	}

	consumer, err := kafka.NewConsumer(cfg, consumerCfg, spikeDetector, logger)
	require.NoError(t, err)

	// Start consumer
	err = consumer.Start()
	require.NoError(t, err)
	defer consumer.Stop()

	// Wait for consumer to be ready
	time.Sleep(500 * time.Millisecond)

	t.Run("Full Pipeline Spike Detection", func(t *testing.T) {
		ctx := context.Background()
		pageTitle := "Integration_Test_Page_" + time.Now().Format("20060102150405")

		// Produce baseline events (normal activity)
		baseTime := time.Now().Add(-time.Hour)
		for i := 0; i < 5; i++ {
			edit := &models.WikipediaEdit{
				ID:        int64(1000 + i),
				Title:     pageTitle,
				User:      "baseline_user",
				Timestamp: baseTime.Add(time.Duration(i) * 10 * time.Minute).Unix(),
				Length:    struct { Old int `json:"old"`; New int `json:"new"` }{Old: 100, New: 110},
				Bot:       false,
				Type:      "edit",
				Wiki:      "enwiki",
			}

			editData, err := json.Marshal(edit)
			require.NoError(t, err)

			message := kafkago.Message{
				Key:   []byte(pageTitle),
				Value: editData,
			}

			err = writer.WriteMessages(ctx, message)
			require.NoError(t, err)
		}

		// Wait for baseline processing
		time.Sleep(1 * time.Second)

		// Produce spike events (rapid activity)
		spikeStart := time.Now()
		for i := 0; i < 15; i++ {
			edit := &models.WikipediaEdit{
				ID:        int64(2000 + i),
				Title:     pageTitle,
				User:      "spike_user_" + string(rune(48+i%3)), // 3 different users
				Timestamp: spikeStart.Add(time.Duration(i) * 20 * time.Second).Unix(),
				Length:    struct { Old int `json:"old"`; New int `json:"new"` }{Old: 100, New: 200},
				Bot:       false,
				Type:      "edit",
				Wiki:      "enwiki",
			}

			editData, err := json.Marshal(edit)
			require.NoError(t, err)

			message := kafkago.Message{
				Key:   []byte(pageTitle),
				Value: editData,
			}

			err = writer.WriteMessages(ctx, message)
			require.NoError(t, err)
		}

		// Wait for spike processing
		time.Sleep(2 * time.Second)

		// Check if alerts were created in Redis stream
		alerts, err := redisClient.XRevRangeN(ctx, "alerts:spikes", "+", "-", 10).Result()
		require.NoError(t, err)

		// Look for our page in the alerts
		var foundAlert *processor.SpikeAlert
		for _, msg := range alerts {
			if alertData, ok := msg.Values["data"].(string); ok {
				var alert processor.SpikeAlert
				if err := json.Unmarshal([]byte(alertData), &alert); err == nil {
					if alert.PageTitle == pageTitle {
						foundAlert = &alert
						break
					}
				}
			}
		}

		require.NotNil(t, foundAlert, "Expected to find spike alert for test page")
		assert.True(t, foundAlert.SpikeRatio >= 5.0, "Expected high spike ratio, got %f", foundAlert.SpikeRatio)
		assert.True(t, foundAlert.Edits5Min >= 3, "Expected significant recent edits")
		assert.Contains(t, []string{"low", "medium", "high", "critical"}, foundAlert.Severity)

		// Check if page is marked as spiking
		spikeKey := "spike:" + pageTitle
		result, err := redisClient.Get(ctx, spikeKey).Result()
		if err == nil {
			assert.Equal(t, "1", result, "Page should be marked as spiking")
		}

		// Verify TTL on spike marker
		ttl, err := redisClient.TTL(ctx, spikeKey).Result()
		if err == nil {
			assert.True(t, ttl > 0 && ttl <= time.Hour, "Spike marker should have reasonable TTL")
		}
	})

	t.Run("Performance Test", func(t *testing.T) {
		ctx := context.Background()
		startTime := time.Now()
		eventCount := 100

		// Produce many events quickly
		for i := 0; i < eventCount; i++ {
			edit := &models.WikipediaEdit{
				ID:        int64(3000 + i),
				Title:     "Performance_Test_Page",
				User:      "perf_user",
				Timestamp: time.Now().Unix(),
				Length:    struct { Old int `json:"old"`; New int `json:"new"` }{Old: 100, New: 105},
				Bot:       false,
				Type:      "edit",
				Wiki:      "enwiki",
			}

			editData, err := json.Marshal(edit)
			require.NoError(t, err)

			message := kafkago.Message{
				Key:   []byte("Performance_Test_Page"),
				Value: editData,
			}

			err = writer.WriteMessages(ctx, message)
			require.NoError(t, err)
		}

		// Wait for all processing to complete
		time.Sleep(3 * time.Second)

		processingTime := time.Since(startTime)
		avgTimePerEvent := processingTime / time.Duration(eventCount)

		t.Logf("Processed %d events in %v (avg: %v per event)", 
			eventCount, processingTime, avgTimePerEvent)

		// Should process each event in under 100ms on average
		assert.True(t, avgTimePerEvent < 100*time.Millisecond, 
			"Average processing time %v exceeds 100ms threshold", avgTimePerEvent)
	})

	t.Run("Consumer Lag Test", func(t *testing.T) {
		// Get consumer stats
		stats := consumer.GetStats()
		
		// Lag should be reasonable (under 100 messages)
		assert.True(t, stats.Lag < 100, "Consumer lag %d exceeds acceptable threshold", stats.Lag)
		
		// Should have processed some messages
		assert.True(t, stats.Messages > 0, "Consumer should have processed some messages")
		
		t.Logf("Consumer stats - Messages: %d, Lag: %d, Errors: %d", 
			stats.Messages, stats.Lag, stats.Errors)
	})
}