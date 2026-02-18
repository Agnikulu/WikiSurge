package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

var (
	spikeDetectorMetricsOnce sync.Once
	sharedSpikeMetrics       *SpikeDetectorMetrics
)

// SpikeDetector handles real-time spike detection using hot page windows
type SpikeDetector struct {
	hotPages             *storage.HotPageTracker
	redis                *redis.Client
	config               *config.Config
	alertStream          string
	metrics              *SpikeDetectorMetrics
	spikeRatioThreshold  float64
	minimumEdits         int
	logger               zerolog.Logger
	mu                   sync.RWMutex
	cooldowns            map[string]time.Time // page -> last alert time
	cooldownDuration     time.Duration
}

// SpikeAlert represents a detected spike event
type SpikeAlert struct {
	PageTitle      string    `json:"page_title"`
	SpikeRatio     float64   `json:"spike_ratio"`
	Edits5Min      int64     `json:"edits_5min"`
	Edits1Hour     int64     `json:"edits_1hour"`
	Severity       string    `json:"severity"`
	Timestamp      time.Time `json:"timestamp"`
	UniqueEditors  int       `json:"unique_editors"`
	ServerURL      string    `json:"server_url,omitempty"`
}

// SpikeDetectorMetrics contains Prometheus metrics for spike detection
type SpikeDetectorMetrics struct {
	SpikesDetected   *prometheus.CounterVec
	ProcessedEdits   prometheus.Counter
	AlertsPublished  prometheus.Counter
	ProcessingTime   prometheus.Histogram
	SpikeRatioGauge  prometheus.Gauge
}

// NewSpikeDetector creates a new spike detector instance
func NewSpikeDetector(hotPages *storage.HotPageTracker, redis *redis.Client, cfg *config.Config, logger zerolog.Logger) *SpikeDetector {
	spikeDetectorMetricsOnce.Do(func() {
		sharedSpikeMetrics = &SpikeDetectorMetrics{
			SpikesDetected: prometheus.NewCounterVec(
				prometheus.CounterOpts{
					Name: "spikes_detected_total",
					Help: "Total number of spikes detected",
				},
				[]string{"severity"},
			),
		ProcessedEdits: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "processed_edits_total",
				Help: "Total number of edits processed by spike detector",
			},
		),
		AlertsPublished: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "alerts_published_total",
				Help: "Total number of alerts published to Redis stream",
			},
		),
		ProcessingTime: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name: "spike_detection_processing_seconds",
				Help: "Time spent processing edits for spike detection",
				Buckets: prometheus.LinearBuckets(0.001, 0.001, 10),
			},
		),
			SpikeRatioGauge: prometheus.NewGauge(
				prometheus.GaugeOpts{
					Name: "last_spike_ratio",
					Help: "Ratio of the last detected spike",
				},
			),
		}

		// Register metrics
		prometheus.MustRegister(
			sharedSpikeMetrics.SpikesDetected,
			sharedSpikeMetrics.ProcessedEdits,
			sharedSpikeMetrics.AlertsPublished,
			sharedSpikeMetrics.ProcessingTime,
			sharedSpikeMetrics.SpikeRatioGauge,
		)
	})

	return &SpikeDetector{
		hotPages:             hotPages,
		redis:                redis,
		config:               cfg,
		alertStream:          "alerts:spikes",
		metrics:              sharedSpikeMetrics,
		spikeRatioThreshold:  5.0, // Default threshold
		minimumEdits:         3,   // Minimum edits in 5 minutes to consider
		logger:               logger.With().Str("component", "spike_detector").Logger(),
		cooldowns:            make(map[string]time.Time),
		cooldownDuration:     10 * time.Minute, // Suppress duplicate alerts for 10 minutes per page
	}
}

// ProcessEdit analyzes each edit for spike potential - handler for Kafka consumer
func (sd *SpikeDetector) ProcessEdit(ctx context.Context, edit *models.WikipediaEdit) error {
	start := time.Now()
	defer func() {
		sd.metrics.ProcessingTime.Observe(time.Since(start).Seconds())
		sd.metrics.ProcessedEdits.Inc()
	}()

	// Update hot page tracker with this edit
	if err := sd.hotPages.ProcessEdit(ctx, edit); err != nil {
		sd.logger.Error().Err(err).Str("page", edit.Title).Msg("Failed to process edit in hot page tracker")
		return err
	}

	// Check if page is hot (promoted to detailed tracking)
	isHot, err := sd.hotPages.IsHot(ctx, edit.Title)
	if err != nil {
		sd.logger.Error().Err(err).Str("page", edit.Title).Msg("Failed to check if page is hot")
		return err
	}

	if !isHot {
		// Page not hot yet, wait for promotion
		return nil
	}

	// Get page statistics for spike detection
	stats, err := sd.hotPages.GetPageStats(ctx, edit.Title)
	if err != nil {
		sd.logger.Error().Err(err).Str("page", edit.Title).Msg("Failed to get page statistics")
		return err
	}

	// Detect spike
	alert := sd.detectSpike(edit.Title, stats)
	if alert != nil {
		// Check cooldown to prevent duplicate alerts
		sd.mu.Lock()
		if lastAlert, exists := sd.cooldowns[edit.Title]; exists && time.Since(lastAlert) < sd.cooldownDuration {
			sd.mu.Unlock()
			return nil // Still in cooldown, suppress duplicate
		}
		sd.cooldowns[edit.Title] = time.Now()
		// Clean up expired cooldowns periodically
		if len(sd.cooldowns) > 500 {
			now := time.Now()
			for page, t := range sd.cooldowns {
				if now.Sub(t) > sd.cooldownDuration {
					delete(sd.cooldowns, page)
				}
			}
		}
		sd.mu.Unlock()

		// Spike detected - publish alert
		if err := sd.publishAlert(ctx, alert); err != nil {
			sd.logger.Error().Err(err).Str("page", edit.Title).Msg("Failed to publish spike alert")
			return err
		}

		// Mark page as spiking for ES indexing strategy
		if err := sd.markPageSpiking(ctx, edit.Title); err != nil {
			sd.logger.Warn().Err(err).Str("page", edit.Title).Msg("Failed to mark page as spiking")
			// Don't fail the entire operation for this
		}

		sd.metrics.SpikesDetected.WithLabelValues(alert.Severity).Inc()
		sd.metrics.SpikeRatioGauge.Set(alert.SpikeRatio)
		sd.logger.Info().Str("page", edit.Title).Float64("ratio", alert.SpikeRatio).Str("severity", alert.Severity).Msg("Spike detected")
	}

	return nil
}

// detectSpike performs statistical spike detection
func (sd *SpikeDetector) detectSpike(pageTitle string, stats *storage.PageStats) *SpikeAlert {
	// Check minimum edits threshold
	if stats.EditsLast5Min < int64(sd.minimumEdits) {
		return nil // Not significant enough
	}

	// Calculate rates (edits per minute)
	rate1h := float64(stats.EditsLastHour) / 60.0  // edits per minute over last hour
	rate5m := float64(stats.EditsLast5Min) / 5.0   // edits per minute over last 5 minutes

	// Calculate baseline to avoid division by zero
	baseline := rate1h
	if baseline < 0.1 {
		baseline = 0.1 // Minimum baseline to prevent false positives
	}

	// Calculate spike ratio
	ratio := rate5m / baseline

	// Check if ratio exceeds threshold
	if ratio < sd.spikeRatioThreshold {
		return nil // No spike detected
	}

	// Create spike alert
	alert := &SpikeAlert{
		PageTitle:     pageTitle,
		SpikeRatio:    ratio,
		Edits5Min:     stats.EditsLast5Min,
		Edits1Hour:    stats.EditsLastHour,
		Severity:      sd.calculateSeverity(ratio),
		Timestamp:     time.Now(),
		UniqueEditors: len(stats.UniqueEditors),
		ServerURL:     stats.ServerURL,
	}

	return alert
}

// calculateSeverity determines spike severity based on ratio
func (sd *SpikeDetector) calculateSeverity(ratio float64) string {
	switch {
	case ratio >= 50:
		return "critical"
	case ratio >= 20:
		return "high"
	case ratio >= 10:
		return "medium"
	default:
		return "low"
	}
}

// publishAlert stores alert in Redis stream for API consumption
func (sd *SpikeDetector) publishAlert(ctx context.Context, alert *SpikeAlert) error {
	// Serialize alert to JSON
	alertData, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("failed to marshal alert: %w", err)
	}

	// Publish to Redis stream with approximate maxlen to trim old entries
	args := &redis.XAddArgs{
		Stream: sd.alertStream,
		MaxLen: 1000, // Keep approximately 1000 recent alerts
		Approx: true, // Use approximate trimming for better performance
		Values: map[string]interface{}{
			"data":     string(alertData),
			"severity": alert.Severity,
			"page":     alert.PageTitle,
		},
	}

	_, err = sd.redis.XAdd(ctx, args).Result()
	if err != nil {
		return fmt.Errorf("failed to publish alert to stream: %w", err)
	}

	sd.metrics.AlertsPublished.Inc()
	return nil
}

// markPageSpiking marks a page as currently spiking for indexing strategy
func (sd *SpikeDetector) markPageSpiking(ctx context.Context, pageTitle string) error {
	spikeKey := fmt.Sprintf("spike:%s", pageTitle)
	return sd.redis.Set(ctx, spikeKey, 1, time.Hour).Err() // 1 hour TTL
}

// GetRecentAlerts fetches recent alerts from Redis stream
func (sd *SpikeDetector) GetRecentAlerts(ctx context.Context, since time.Time, limit int64) ([]*SpikeAlert, error) {
	// Convert timestamp to Redis stream ID format
	sinceID := fmt.Sprintf("%d-0", since.UnixMilli())

	// Read from stream
	result, err := sd.redis.XRevRangeN(ctx, sd.alertStream, "+", sinceID, limit).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to read alerts from stream: %w", err)
	}

	// Parse alerts
	alerts := make([]*SpikeAlert, 0, len(result))
	for _, msg := range result {
		if alertData, ok := msg.Values["data"].(string); ok {
			var alert SpikeAlert
			if err := json.Unmarshal([]byte(alertData), &alert); err != nil {
				sd.logger.Warn().Err(err).Str("stream_id", msg.ID).Msg("Failed to unmarshal alert")
				continue
			}
			alerts = append(alerts, &alert)
		}
	}

	return alerts, nil
}