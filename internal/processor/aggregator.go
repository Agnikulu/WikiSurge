package processor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
)

// TrendingAggregator processes edits to update trending scores
type TrendingAggregator struct {
	scorer       *storage.TrendingScorer
	statsTracker *storage.StatsTracker
	config       *config.Config
	metrics      *AggregatorMetrics
	logger       zerolog.Logger
}

// AggregatorMetrics contains metrics for the trending aggregator
type AggregatorMetrics struct {
	EditsProcessed prometheus.Counter
	ProcessError   prometheus.Counter
	UpdateLatency  prometheus.Histogram
}

// NewTrendingAggregator creates a new trending aggregator
func NewTrendingAggregator(scorer *storage.TrendingScorer, statsTracker *storage.StatsTracker, cfg *config.Config, logger zerolog.Logger) *TrendingAggregator {
	return newTrendingAggregator(scorer, statsTracker, cfg, logger, true)
}

// NewTrendingAggregatorForTest creates a new trending aggregator for tests (no metrics registration)
func NewTrendingAggregatorForTest(scorer *storage.TrendingScorer, cfg *config.Config, logger zerolog.Logger) *TrendingAggregator {
	return newTrendingAggregator(scorer, nil, cfg, logger, false)
}

// newTrendingAggregator is the internal constructor
func newTrendingAggregator(scorer *storage.TrendingScorer, statsTracker *storage.StatsTracker, cfg *config.Config, logger zerolog.Logger, registerMetrics bool) *TrendingAggregator {
	metrics := &AggregatorMetrics{
		EditsProcessed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "trending_edits_processed_total",
			Help: "Total edits processed by trending aggregator",
		}),
		ProcessError: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "trending_process_errors_total",
			Help: "Total trending processing errors",
		}),
		UpdateLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "trending_update_duration_seconds",
			Help:    "Trending update latency",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0},
		}),
	}

	// Register metrics only if requested
	if registerMetrics {
		prometheus.MustRegister(metrics.EditsProcessed)
		prometheus.MustRegister(metrics.ProcessError)
		prometheus.MustRegister(metrics.UpdateLatency)
	}

	return &TrendingAggregator{
		scorer:       scorer,
		statsTracker: statsTracker,
		config:       cfg,
		metrics:      metrics,
		logger:       logger.With().Str("component", "trending-aggregator").Logger(),
	}
}

// ProcessMessage processes a single Kafka message containing Wikipedia edit data
func (t *TrendingAggregator) ProcessMessage(message []byte) error {
	timer := prometheus.NewTimer(t.metrics.UpdateLatency)
	defer timer.ObserveDuration()

	// Parse the edit
	var edit models.WikipediaEdit
	if err := json.Unmarshal(message, &edit); err != nil {
		t.metrics.ProcessError.Inc()
		return fmt.Errorf("failed to unmarshal edit: %w", err)
	}

	// Process the edit
	if err := t.ProcessEdit(context.Background(), &edit); err != nil {
		t.metrics.ProcessError.Inc()
		t.logger.Error().
			Err(err).
			Str("title", edit.Title).
			Str("user", edit.User).
			Msg("Failed to process edit for trending")
		return err
	}

	t.metrics.EditsProcessed.Inc()
	return nil
}

// ProcessEdit updates trending scores for a single edit (implements MessageHandler)
func (t *TrendingAggregator) ProcessEdit(ctx context.Context, edit *models.WikipediaEdit) error {
	// Process the edit through the scorer
	if err := t.scorer.ProcessEdit(edit); err != nil {
		return fmt.Errorf("failed to update trending score: %w", err)
	}

	// Record per-language and timeline stats
	if t.statsTracker != nil {
		lang := edit.Language()
		if lang == "" {
			lang = "unknown"
		}
		if err := t.statsTracker.RecordEdit(ctx, lang, edit.Bot); err != nil {
			t.logger.Warn().Err(err).Msg("Failed to record edit stats")
		}
	}

	t.logger.Debug().
		Str("title", edit.Title).
		Str("user", edit.User).
		Bool("bot", edit.Bot).
		Int("byte_change", edit.ByteChange()).
		Msg("Updated trending score")

	return nil
}

// GetMetrics returns the aggregator metrics
func (t *TrendingAggregator) GetMetrics() *AggregatorMetrics {
	return t.metrics
}