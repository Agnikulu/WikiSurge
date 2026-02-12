package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/metrics"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

var (
	editWarMetricsOnce sync.Once
	sharedEditWarMetrics *EditWarMetrics
)

// EditWarDetector detects ongoing editorial conflicts in real-time
type EditWarDetector struct {
	redis       *redis.Client
	hotPages    *storage.HotPageTracker
	config      *config.Config
	alertStream string
	metrics     *EditWarMetrics
	minEdits    int
	minEditors  int
	minReverts  int
	timeWindow  time.Duration
	logger      zerolog.Logger
	mu          sync.RWMutex
}

// EditWarAlert represents a detected edit war event
type EditWarAlert struct {
	PageTitle   string    `json:"page_title"`
	EditorCount int       `json:"editor_count"`
	EditCount   int       `json:"edit_count"`
	RevertCount int       `json:"revert_count"`
	Severity    string    `json:"severity"`
	StartTime   time.Time `json:"start_time"`
	Editors     []string  `json:"editors"`
}

// EditWarMetrics contains Prometheus metrics for edit war detection
type EditWarMetrics struct {
	EditWarsDetected  *prometheus.CounterVec
	ProcessedEdits    prometheus.Counter
	AlertsPublished   prometheus.Counter
	ProcessingTime    prometheus.Histogram
	ActiveEditWars    prometheus.Gauge
}

// NewEditWarDetector creates a new edit war detector instance
func NewEditWarDetector(hotPages *storage.HotPageTracker, redisClient *redis.Client, cfg *config.Config, logger zerolog.Logger) *EditWarDetector {
	editWarMetricsOnce.Do(func() {
		sharedEditWarMetrics = &EditWarMetrics{
			EditWarsDetected: prometheus.NewCounterVec(
				prometheus.CounterOpts{
					Name: "edit_war_detections_total",
					Help: "Total number of edit wars detected",
				},
				[]string{"severity"},
			),
			ProcessedEdits: prometheus.NewCounter(
				prometheus.CounterOpts{
					Name: "edit_war_processed_edits_total",
					Help: "Total number of edits processed by edit war detector",
				},
			),
			AlertsPublished: prometheus.NewCounter(
				prometheus.CounterOpts{
					Name: "edit_war_alerts_published_total",
					Help: "Total number of edit war alerts published to Redis stream",
				},
			),
			ProcessingTime: prometheus.NewHistogram(
				prometheus.HistogramOpts{
					Name:    "edit_war_processing_seconds",
					Help:    "Time spent processing edits for edit war detection",
					Buckets: prometheus.LinearBuckets(0.001, 0.001, 10),
				},
			),
			ActiveEditWars: prometheus.NewGauge(
				prometheus.GaugeOpts{
					Name: "edit_wars_active",
					Help: "Number of currently active edit wars",
				},
			),
		}

		prometheus.MustRegister(
			sharedEditWarMetrics.EditWarsDetected,
			sharedEditWarMetrics.ProcessedEdits,
			sharedEditWarMetrics.AlertsPublished,
			sharedEditWarMetrics.ProcessingTime,
			sharedEditWarMetrics.ActiveEditWars,
		)
	})

	return &EditWarDetector{
		redis:       redisClient,
		hotPages:    hotPages,
		config:      cfg,
		alertStream: "alerts:editwars",
		metrics:     sharedEditWarMetrics,
		minEdits:    5,
		minEditors:  2,
		minReverts:  1,
		timeWindow:  10 * time.Minute,
		logger:      logger.With().Str("component", "edit_war_detector").Logger(),
	}
}

// ProcessEdit analyzes each edit for edit war patterns - handler for Kafka consumer
func (ewd *EditWarDetector) ProcessEdit(ctx context.Context, edit *models.WikipediaEdit) error {
	start := time.Now()
	defer func() {
		ewd.metrics.ProcessingTime.Observe(time.Since(start).Seconds())
		ewd.metrics.ProcessedEdits.Inc()
	}()

	// Check if page is hot (only check hot pages)
	isHot, err := ewd.hotPages.IsHot(ctx, edit.Title)
	if err != nil {
		ewd.logger.Error().Err(err).Str("page", edit.Title).Msg("Failed to check if page is hot")
		return err
	}
	if !isHot {
		return nil
	}

	// Update editor tracking: HINCRBY for editor's edit count
	editorsKey := fmt.Sprintf("editwar:editors:%s", edit.Title)
	pipe := ewd.redis.Pipeline()
	pipe.HIncrBy(ctx, editorsKey, edit.User, 1)
	pipe.Expire(ctx, editorsKey, ewd.timeWindow)

	// Update byte change tracking for revert detection
	changesKey := fmt.Sprintf("editwar:changes:%s", edit.Title)
	byteChange := edit.ByteChange()
	pipe.RPush(ctx, changesKey, byteChange)
	pipe.LTrim(ctx, changesKey, -100, -1) // Keep last 100 changes
	pipe.Expire(ctx, changesKey, ewd.timeWindow)

	_, err = pipe.Exec(ctx)
	if err != nil {
		ewd.logger.Error().Err(err).Str("page", edit.Title).Msg("Failed to update editor/change tracking")
		return err
	}

	// Get editor hash to check counts
	editorMap, err := ewd.redis.HGetAll(ctx, editorsKey).Result()
	if err != nil {
		ewd.logger.Error().Err(err).Str("page", edit.Title).Msg("Failed to get editor hash")
		return err
	}

	// Calculate total edits and unique editors from our own tracking
	uniqueEditors := len(editorMap)
	totalEdits := 0
	for _, countStr := range editorMap {
		count, _ := strconv.Atoi(countStr)
		totalEdits += count
	}

	// Check if conditions warrant edit war analysis
	if uniqueEditors >= ewd.minEditors && totalEdits >= ewd.minEdits {
		alert, err := ewd.detectEditWar(ctx, edit.Title)
		if err != nil {
			ewd.logger.Error().Err(err).Str("page", edit.Title).Msg("Failed to detect edit war")
			return err
		}

		if alert != nil {
			if err := ewd.publishEditWarAlert(ctx, alert, edit.Wiki); err != nil {
				ewd.logger.Error().Err(err).Str("page", edit.Title).Msg("Failed to publish edit war alert")
				return err
			}

			ewd.metrics.EditWarsDetected.WithLabelValues(alert.Severity).Inc()
			metrics.EditWarsDetectedTotal.WithLabelValues().Inc()
			ewd.logger.Info().
				Str("page", edit.Title).
				Int("editors", alert.EditorCount).
				Int("reverts", alert.RevertCount).
				Str("severity", alert.Severity).
				Msg("Edit war detected")
		}
	}

	return nil
}

// detectEditWar performs statistical analysis for edit war patterns
func (ewd *EditWarDetector) detectEditWar(ctx context.Context, pageTitle string) (*EditWarAlert, error) {
	editorsKey := fmt.Sprintf("editwar:editors:%s", pageTitle)

	// Get all editors and their edit counts
	editorMap, err := ewd.redis.HGetAll(ctx, editorsKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get editor hash: %w", err)
	}

	// Parse editors and count total edits
	editors := make([]string, 0, len(editorMap))
	totalEdits := 0
	for editor, countStr := range editorMap {
		editors = append(editors, editor)
		count, _ := strconv.Atoi(countStr)
		totalEdits += count
	}

	uniqueEditors := len(editors)

	// Check minimum conditions
	if totalEdits < ewd.minEdits {
		return nil, nil
	}
	if uniqueEditors < ewd.minEditors {
		return nil, nil
	}

	// Count reverts using byte change pattern analysis
	revertCount, err := ewd.countReverts(ctx, pageTitle)
	if err != nil {
		ewd.logger.Warn().Err(err).Str("page", pageTitle).Msg("Failed to count reverts, defaulting to 0")
		revertCount = 0
	}

	// Also check edit comments for revert indicators
	// (this is a simple heuristic complement)

	if revertCount < ewd.minReverts {
		ewd.logger.Debug().
			Str("page", pageTitle).
			Int("editors", uniqueEditors).
			Int("edits", totalEdits).
			Int("reverts", revertCount).
			Int("min_reverts", ewd.minReverts).
			Msg("Edit war candidate failed: not enough reverts detected")
		return nil, nil
	}

	// All conditions met — calculate severity and create alert
	severity := ewd.calculateEditWarSeverity(totalEdits, uniqueEditors, revertCount)

	alert := &EditWarAlert{
		PageTitle:   pageTitle,
		EditorCount: uniqueEditors,
		EditCount:   totalEdits,
		RevertCount: revertCount,
		Severity:    severity,
		StartTime:   time.Now().Add(-ewd.timeWindow),
		Editors:     editors,
	}

	return alert, nil
}

// countReverts detects reversions in the edit sequence by analyzing byte change patterns
func (ewd *EditWarDetector) countReverts(ctx context.Context, pageTitle string) (int, error) {
	changesKey := fmt.Sprintf("editwar:changes:%s", pageTitle)

	// Get all byte changes in chronological order
	changesStr, err := ewd.redis.LRange(ctx, changesKey, 0, -1).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get byte changes: %w", err)
	}

	if len(changesStr) < 2 {
		return 0, nil
	}

	// Parse byte changes
	changes := make([]int, 0, len(changesStr))
	for _, cs := range changesStr {
		val, err := strconv.Atoi(cs)
		if err != nil {
			continue
		}
		changes = append(changes, val)
	}

	if len(changes) < 2 {
		return 0, nil
	}

	revertCount := 0

	// Look for alternating sign patterns with similar magnitude
	// Edit 1: +500 bytes
	// Edit 2: -480 bytes → potential revert (opposite sign, similar magnitude)
	// Edit 3: +490 bytes → potential revert of revert
	for i := 1; i < len(changes); i++ {
		prev := changes[i-1]
		curr := changes[i]

		// For very small changes (including zeros), count alternating patterns
		if prev == 0 && curr == 0 {
			// Two consecutive zero-byte edits often indicate reverts
			revertCount++
			continue
		}

		// Check for opposite signs
		if (prev > 0 && curr < 0) || (prev < 0 && curr > 0) {
			// For very small edits, be lenient
			if math.Abs(float64(prev)) < 10 && math.Abs(float64(curr)) < 10 {
				revertCount++
				continue
			}

			// Check if magnitudes are similar (within 30% tolerance for better detection)
			absPrev := math.Abs(float64(prev))
			absCurr := math.Abs(float64(curr))

			ratio := absCurr / absPrev
			if ratio > 1 {
				ratio = absPrev / absCurr
			}

			// If magnitudes are within 30% of each other, likely a revert
			if ratio >= 0.6 {
				revertCount++
			}
		}
	}

	return revertCount, nil
}

// calculateEditWarSeverity determines severity based on edit patterns
func (ewd *EditWarDetector) calculateEditWarSeverity(editCount, editorCount, revertCount int) string {
	switch {
	case editorCount > 5 || revertCount > 10:
		return "critical"
	case editorCount <= 5 && revertCount <= 10:
		if editorCount > 3 || revertCount > 5 {
			return "high"
		}
		if editorCount > 2 || revertCount > 3 {
			return "medium"
		}
		return "low"
	default:
		return "low"
	}
}

// publishEditWarAlert stores the alert in a Redis stream and marks the page
func (ewd *EditWarDetector) publishEditWarAlert(ctx context.Context, alert *EditWarAlert, wiki string) error {
	// Serialize to JSON
	alertData, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("failed to marshal edit war alert: %w", err)
	}

	// XADD to stream
	args := &redis.XAddArgs{
		Stream: ewd.alertStream,
		MaxLen: 1000,
		Approx: true,
		Values: map[string]interface{}{
			"data":     string(alertData),
			"severity": alert.Severity,
			"page":     alert.PageTitle,
		},
	}

	_, err = ewd.redis.XAdd(ctx, args).Result()
	if err != nil {
		return fmt.Errorf("failed to publish edit war alert to stream: %w", err)
	}

	// Mark page as having edit war (12 hour TTL)
	// Use editwar:{title} for simple lookups
	editWarKey := fmt.Sprintf("editwar:%s", alert.PageTitle)
	if err := ewd.redis.Set(ctx, editWarKey, 1, 12*time.Hour).Err(); err != nil {
		ewd.logger.Warn().Err(err).Str("page", alert.PageTitle).Msg("Failed to set editwar marker key")
	}

	// Persist the first-seen start time for this edit war so UI can show a
	// stable duration. Use SETNX to avoid overwriting an existing first-seen
	// timestamp, but refresh TTL on each publish so the key expires together
	// with the marker when the war ends.
	startKey := fmt.Sprintf("editwar:start:%s", alert.PageTitle)
	firstSeen := time.Now().UTC().Format(time.RFC3339)
	set, err := ewd.redis.SetNX(ctx, startKey, firstSeen, 12*time.Hour).Result()
	if err != nil {
		ewd.logger.Warn().Err(err).Str("page", alert.PageTitle).Msg("Failed to set editwar start key")
	} else if !set {
		// Key already exists — refresh TTL to keep it alive while war remains active
		_ = ewd.redis.Expire(ctx, startKey, 12*time.Hour).Err()
	}

	// Also set editwar:{wiki}:{title} for indexing strategy compatibility
	if wiki != "" {
		editWarWikiKey := fmt.Sprintf("editwar:%s:%s", wiki, alert.PageTitle)
		if err := ewd.redis.Set(ctx, editWarWikiKey, 1, 12*time.Hour).Err(); err != nil {
			ewd.logger.Warn().Err(err).Str("page", alert.PageTitle).Msg("Failed to set editwar wiki marker key")
		}
	}

	ewd.metrics.AlertsPublished.Inc()
	return nil
}

// GetActiveEditWars retrieves all currently active edit wars
func (ewd *EditWarDetector) GetActiveEditWars(ctx context.Context) ([]*EditWarAlert, error) {
	var cursor uint64
	var activeWars []*EditWarAlert

	for {
		keys, nextCursor, err := ewd.redis.Scan(ctx, cursor, "editwar:editors:*", 100).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan for active edit wars: %w", err)
		}

		for _, key := range keys {
			// Extract page title from key
			pageTitle := strings.TrimPrefix(key, "editwar:editors:")

			// Check if this page is actually marked as having an edit war
			editWarKey := fmt.Sprintf("editwar:%s", pageTitle)
			exists, err := ewd.redis.Exists(ctx, editWarKey).Result()
			if err != nil || exists == 0 {
				continue
			}

			// Get editor hash
			editorMap, err := ewd.redis.HGetAll(ctx, key).Result()
			if err != nil {
				continue
			}

			editors := make([]string, 0, len(editorMap))
			totalEdits := 0
			for editor, countStr := range editorMap {
				editors = append(editors, editor)
				count, _ := strconv.Atoi(countStr)
				totalEdits += count
			}

			revertCount, _ := ewd.countReverts(ctx, pageTitle)

			severity := ewd.calculateEditWarSeverity(totalEdits, len(editors), revertCount)

			activeWars = append(activeWars, &EditWarAlert{
				PageTitle:   pageTitle,
				EditorCount: len(editors),
				EditCount:   totalEdits,
				RevertCount: revertCount,
				Severity:    severity,
				StartTime:   time.Now().Add(-ewd.timeWindow),
				Editors:     editors,
			})
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	if activeWars == nil {
		activeWars = make([]*EditWarAlert, 0)
	}

	ewd.metrics.ActiveEditWars.Set(float64(len(activeWars)))
	return activeWars, nil
}

// GetRecentAlerts fetches recent edit war alerts from the Redis stream
func (ewd *EditWarDetector) GetRecentAlerts(ctx context.Context, since time.Time, limit int64) ([]*EditWarAlert, error) {
	sinceID := fmt.Sprintf("%d-0", since.UnixMilli())

	result, err := ewd.redis.XRevRangeN(ctx, ewd.alertStream, "+", sinceID, limit).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to read edit war alerts from stream: %w", err)
	}

	alerts := make([]*EditWarAlert, 0, len(result))
	for _, msg := range result {
		if alertData, ok := msg.Values["data"].(string); ok {
			var alert EditWarAlert
			if err := json.Unmarshal([]byte(alertData), &alert); err != nil {
				ewd.logger.Warn().Err(err).Str("stream_id", msg.ID).Msg("Failed to unmarshal edit war alert")
				continue
			}
			alerts = append(alerts, &alert)
		}
	}

	return alerts, nil
}
