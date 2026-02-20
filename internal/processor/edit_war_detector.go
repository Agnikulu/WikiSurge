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
	"github.com/Agnikulu/WikiSurge/internal/llm"
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
	redis            *redis.Client
	hotPages         *storage.HotPageTracker
	config           *config.Config
	alertStream      string
	metrics          *EditWarMetrics
	analysisService  *llm.AnalysisService
	minEdits         int
	minEditors       int
	minReverts       int
	timeWindow       time.Duration
	logger           zerolog.Logger
	mu               sync.RWMutex
	cooldowns        map[string]time.Time // page -> last alert time
	cooldownDuration time.Duration
	reanalyzeEvery   int // re-run LLM analysis every N edits on active wars (0=disabled)
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
	ServerURL   string    `json:"server_url,omitempty"`
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
		redis:            redisClient,
		hotPages:         hotPages,
		config:           cfg,
		alertStream:      "alerts:editwars",
		metrics:          sharedEditWarMetrics,
		minEdits:         5,
		minEditors:       2,
		minReverts:       2,
		timeWindow:       10 * time.Minute,
		logger:           logger.With().Str("component", "edit_war_detector").Logger(),
		cooldowns:        make(map[string]time.Time),
		cooldownDuration: 5 * time.Minute, // Suppress duplicate alerts for 5 minutes per page
		reanalyzeEvery:   cfg.LLM.ReanalyzeEvery,
	}
}

// SetAnalysisService attaches an LLM analysis service so edit wars are
// analysed automatically on detection.
func (ewd *EditWarDetector) SetAnalysisService(svc *llm.AnalysisService) {
	ewd.analysisService = svc
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

	// Track edit timeline (user, comment, byte change, timestamp) for LLM analysis.
	// Only stored for hot pages already in the edit-war tracking path.
	// Use 12h TTL (matching the edit war marker) so timeline data remains
	// available for LLM analysis as long as the edit war is shown as active.
	timelineKey := fmt.Sprintf("editwar:timeline:%s", edit.Title)
	timelineEntry, _ := json.Marshal(map[string]interface{}{
		"user":        edit.User,
		"comment":     edit.Comment,
		"byte_change": byteChange,
		"timestamp":   edit.Timestamp,
		"revision_id": edit.Revision.New,
		"server_url":  edit.ServerURL,
	})
	pipe.RPush(ctx, timelineKey, string(timelineEntry))
	pipe.LTrim(ctx, timelineKey, -100, -1) // Keep last 100 entries
	pipe.Expire(ctx, timelineKey, 12*time.Hour)

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
			// Check cooldown to prevent duplicate alerts for the same page
			ewd.mu.Lock()
			if lastAlert, exists := ewd.cooldowns[edit.Title]; exists && time.Since(lastAlert) < ewd.cooldownDuration {
				ewd.mu.Unlock()
				// War already known — check if it's time for periodic re-analysis
				ewd.maybeReanalyze(ctx, edit.Title)
				return nil // Still in cooldown, suppress duplicate
			}
			ewd.cooldowns[edit.Title] = time.Now()
			// Clean up expired cooldowns periodically
			if len(ewd.cooldowns) > 500 {
				now := time.Now()
				for page, t := range ewd.cooldowns {
					if now.Sub(t) > ewd.cooldownDuration {
						delete(ewd.cooldowns, page)
					}
				}
			}
			ewd.mu.Unlock()

			alert.ServerURL = edit.ServerURL
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

	// Derive start time from the first timeline entry's actual edit timestamp.
	// This gives the real time the first edit occurred, not when we detected it.
	startTime := time.Now().Add(-ewd.timeWindow) // fallback
	timelineKey := fmt.Sprintf("editwar:timeline:%s", pageTitle)
	if firstRaw, tlErr := ewd.redis.LIndex(ctx, timelineKey, 0).Result(); tlErr == nil && firstRaw != "" {
		var firstEntry struct {
			Timestamp int64 `json:"timestamp"`
		}
		if json.Unmarshal([]byte(firstRaw), &firstEntry) == nil && firstEntry.Timestamp > 0 {
			startTime = time.Unix(firstEntry.Timestamp, 0)
		}
	}

	// Also check for a persisted first-seen timestamp (set on initial detection)
	startKey := fmt.Sprintf("editwar:start:%s", pageTitle)
	if s, sErr := ewd.redis.Get(ctx, startKey).Result(); sErr == nil && s != "" {
		if t, pErr := time.Parse(time.RFC3339, s); pErr == nil {
			// Use the earlier of the two — persisted vs timeline-derived
			if t.Before(startTime) {
				startTime = t
			}
		}
	}

	alert := &EditWarAlert{
		PageTitle:   pageTitle,
		EditorCount: uniqueEditors,
		EditCount:   totalEdits,
		RevertCount: revertCount,
		Severity:    severity,
		StartTime:   startTime,
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

	// Persist server_url so the frontend can build correct wiki links
	if wiki != "" {
		lang := strings.TrimSuffix(wiki, "wiki")
		serverURL := fmt.Sprintf("https://%s.wikipedia.org", lang)
		urlKey := fmt.Sprintf("editwar:serverurl:%s", alert.PageTitle)
		_ = ewd.redis.Set(ctx, urlKey, serverURL, 12*time.Hour).Err()
	}

	// Persist the first-seen start time for this edit war so UI can show a
	// stable duration. Use SETNX to avoid overwriting an existing first-seen
	// timestamp, but refresh TTL on each publish so the key expires together
	// with the marker when the war ends.
	// Persist the actual start time from the alert (derived from edit data)
	// rather than time.Now() so that duration tracking reflects reality.
	startKey := fmt.Sprintf("editwar:start:%s", alert.PageTitle)
	firstSeen := alert.StartTime.UTC().Format(time.RFC3339)
	set, err := ewd.redis.SetNX(ctx, startKey, firstSeen, 12*time.Hour).Result()
	if err != nil {
		ewd.logger.Warn().Err(err).Str("page", alert.PageTitle).Msg("Failed to set editwar start key")
	} else if !set {
		// Key already exists — refresh TTL to keep it alive while war remains active
		_ = ewd.redis.Expire(ctx, startKey, 12*time.Hour).Err()
	}

	// Refresh the timeline key TTL to match the 12h edit war marker so
	// LLM analysis data stays available as long as the war is active.
	timelineKey := fmt.Sprintf("editwar:timeline:%s", alert.PageTitle)
	_ = ewd.redis.Expire(ctx, timelineKey, 12*time.Hour).Err()

	// Also set editwar:{wiki}:{title} for indexing strategy compatibility
	if wiki != "" {
		editWarWikiKey := fmt.Sprintf("editwar:%s:%s", wiki, alert.PageTitle)
		if err := ewd.redis.Set(ctx, editWarWikiKey, 1, 12*time.Hour).Err(); err != nil {
			ewd.logger.Warn().Err(err).Str("page", alert.PageTitle).Msg("Failed to set editwar wiki marker key")
		}
	}

	ewd.metrics.AlertsPublished.Inc()

	// Trigger LLM analysis in the background when a new edit war is detected.
	// The result is cached in Redis so the API can serve it immediately.
	if ewd.analysisService != nil {
		go func(page string) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, err := ewd.analysisService.Analyze(ctx, page); err != nil {
				ewd.logger.Warn().Err(err).Str("page", page).Msg("Auto-analysis failed for new edit war")
			} else {
				ewd.logger.Info().Str("page", page).Msg("Auto-analysis completed for edit war")
			}
		}(alert.PageTitle)
	}

	return nil
}

// maybeReanalyze triggers a fresh LLM re-analysis every N edits on an active
// edit war, using a Redis counter. This keeps the analysis current as the
// conflict evolves without calling the LLM on every single edit.
func (ewd *EditWarDetector) maybeReanalyze(ctx context.Context, pageTitle string) {
	if ewd.analysisService == nil || ewd.reanalyzeEvery <= 0 {
		return
	}

	counterKey := fmt.Sprintf("editwar:reanalyze_ctr:%s", pageTitle)
	count, err := ewd.redis.Incr(ctx, counterKey).Result()
	if err != nil {
		return
	}
	// Expire with the edit war marker so it cleans itself up
	if count == 1 {
		_ = ewd.redis.Expire(ctx, counterKey, 12*time.Hour).Err()
	}

	if count%int64(ewd.reanalyzeEvery) != 0 {
		return
	}

	go func(page string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := ewd.analysisService.Reanalyze(ctx, page); err != nil {
			ewd.logger.Warn().Err(err).Str("page", page).Msg("Periodic re-analysis failed")
		} else {
			ewd.logger.Info().Str("page", page).Int64("edit_num", count).Msg("Periodic re-analysis completed")
		}
	}(pageTitle)
}

// StartDeactivationSweeper launches a background goroutine that periodically
// checks for edit wars that have become inactive (editor data expired) and
// triggers a final LLM analysis before the timeline data also expires.
// Call this once after wiring up the analysis service. The sweeper stops when
// ctx is cancelled.
func (ewd *EditWarDetector) StartDeactivationSweeper(ctx context.Context, interval time.Duration) {
	if ewd.analysisService == nil {
		ewd.logger.Info().Msg("No analysis service configured; skipping deactivation sweeper")
		return
	}
	if interval <= 0 {
		interval = 2 * time.Minute
	}

	// Redis set that tracks page titles of currently-active edit wars.
	// The sweeper compares this set on each tick to discover newly-inactive wars.
	const trackingKey = "editwar:active_set"

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		ewd.logger.Info().Dur("interval", interval).Msg("Edit war deactivation sweeper started")

		for {
			select {
			case <-ctx.Done():
				ewd.logger.Info().Msg("Edit war deactivation sweeper stopped")
				return
			case <-ticker.C:
				ewd.sweepDeactivatedWars(ctx, trackingKey)
			}
		}
	}()
}

// sweepDeactivatedWars discovers edit wars that were previously active but
// whose editor data has now expired, runs a final analysis, and removes them
// from the tracking set.
func (ewd *EditWarDetector) sweepDeactivatedWars(ctx context.Context, trackingKey string) {
	// 1. Collect all currently-active war page titles by scanning marker keys.
	currentlyActive := make(map[string]bool)
	var cursor uint64
	for {
		keys, next, err := ewd.redis.Scan(ctx, cursor, "editwar:editors:*", 200).Result()
		if err != nil {
			ewd.logger.Warn().Err(err).Msg("Sweeper: failed to scan editor keys")
			return
		}
		for _, k := range keys {
			page := strings.TrimPrefix(k, "editwar:editors:")
			// Only consider pages that also have a marker key (actual wars).
			mk := fmt.Sprintf("editwar:%s", page)
			if ex, _ := ewd.redis.Exists(ctx, mk).Result(); ex > 0 {
				currentlyActive[page] = true
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}

	// 2. Add all currently-active wars to the tracking set (so we remember them).
	for page := range currentlyActive {
		_ = ewd.redis.SAdd(ctx, trackingKey, page).Err()
	}
	// Keep the tracking set alive as long as we have active wars.
	if len(currentlyActive) > 0 {
		_ = ewd.redis.Expire(ctx, trackingKey, 24*time.Hour).Err()
	}

	// 3. Read previously-tracked wars and find the ones no longer active.
	previouslyActive, err := ewd.redis.SMembers(ctx, trackingKey).Result()
	if err != nil {
		ewd.logger.Warn().Err(err).Msg("Sweeper: failed to read tracking set")
		return
	}

	for _, page := range previouslyActive {
		if currentlyActive[page] {
			continue // still active, nothing to do
		}

		// This war just became inactive — run a final analysis.
		ewd.logger.Info().Str("page", page).Msg("Edit war deactivated, running final analysis")

		go func(p string) {
			fCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			if _, err := ewd.analysisService.FinalizeAnalysis(fCtx, p); err != nil {
				ewd.logger.Warn().Err(err).Str("page", p).Msg("Final analysis failed for deactivated edit war")
			} else {
				ewd.logger.Info().Str("page", p).Msg("Final analysis completed for deactivated edit war")
			}
		}(page)

		// Remove from tracking set so we don't re-trigger.
		_ = ewd.redis.SRem(ctx, trackingKey, page).Err()
	}
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

			// Derive start time from persisted key or TTL
			warStart := time.Now().Add(-ewd.timeWindow) // fallback
			startKey := fmt.Sprintf("editwar:start:%s", pageTitle)
			if s, sErr := ewd.redis.Get(ctx, startKey).Result(); sErr == nil && s != "" {
				if t, pErr := time.Parse(time.RFC3339, s); pErr == nil {
					warStart = t
				}
			} else {
				// Try TTL-based approximation
				ttl, ttlErr := ewd.redis.TTL(ctx, key).Result()
				if ttlErr == nil && ttl > 0 && ttl <= ewd.timeWindow {
					elapsed := ewd.timeWindow - ttl
					warStart = time.Now().Add(-elapsed)
				}
			}

			activeWars = append(activeWars, &EditWarAlert{
				PageTitle:   pageTitle,
				EditorCount: len(editors),
				EditCount:   totalEdits,
				RevertCount: revertCount,
				Severity:    severity,
				StartTime:   warStart,
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

// TimelineEntry represents a single edit in the edit war timeline, stored in Redis.
type TimelineEntry struct {
	User       string `json:"user"`
	Comment    string `json:"comment"`
	ByteChange int    `json:"byte_change"`
	Timestamp  int64  `json:"timestamp"`
	RevisionID int64  `json:"revision_id,omitempty"`
	ServerURL  string `json:"server_url,omitempty"`
}

// GetTimeline retrieves the edit timeline for a page's edit war from Redis.
func (ewd *EditWarDetector) GetTimeline(ctx context.Context, pageTitle string) ([]TimelineEntry, error) {
	timelineKey := fmt.Sprintf("editwar:timeline:%s", pageTitle)
	raw, err := ewd.redis.LRange(ctx, timelineKey, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get timeline for %s: %w", pageTitle, err)
	}

	entries := make([]TimelineEntry, 0, len(raw))
	for _, r := range raw {
		var entry TimelineEntry
		if err := json.Unmarshal([]byte(r), &entry); err != nil {
			ewd.logger.Warn().Err(err).Msg("Failed to unmarshal timeline entry")
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
