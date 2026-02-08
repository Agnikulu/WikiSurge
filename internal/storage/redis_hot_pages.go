package storage

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/metrics"
	"github.com/Agnikulu/WikiSurge/internal/models"
)

// HotPageTracker manages bounded hot page tracking to avoid memory explosion
type HotPageTracker struct {
	redis             *redis.Client
	config            *config.HotPages
	hotThreshold      int
	windowDuration    time.Duration
	maxHotPages       int
	maxMembersPerPage int
	// metrics would go here if needed
	cleanupInterval   time.Duration
	
	// Internal state
	shutdown       chan struct{}
	cleanupRunning bool
	mu             sync.RWMutex
	hotPagesCache  map[string]bool
	cacheExpiry    time.Time
}

// PageStats represents statistics for spike detection
type PageStats struct {
	EditsLastHour    int64     `json:"edits_last_hour"`
	EditsLast5Min    int64     `json:"edits_last_5min"`
	UniqueEditors    []string  `json:"unique_editors"`
	LastByteChange   int64     `json:"last_byte_change"`
	TotalEdits       int64     `json:"total_edits"`
}

// NewHotPageTracker creates a new hot page tracker with bounded memory
func NewHotPageTracker(client *redis.Client, cfg *config.HotPages) *HotPageTracker {
	// Set defaults if not configured
	hotThreshold := 2
	if cfg.HotThreshold > 0 {
		hotThreshold = cfg.HotThreshold
	}
	
	windowDuration := time.Hour
	if cfg.WindowDuration > 0 {
		windowDuration = cfg.WindowDuration
	}
	
	maxHotPages := 1000
	if cfg.MaxTracked > 0 {
		maxHotPages = cfg.MaxTracked
	}
	
	maxMembersPerPage := 100
	if cfg.MaxMembersPerPage > 0 {
		maxMembersPerPage = cfg.MaxMembersPerPage
	}
	
	cleanupInterval := 5 * time.Minute
	if cfg.CleanupInterval > 0 {
		cleanupInterval = cfg.CleanupInterval
	}

	tracker := &HotPageTracker{
		redis:             client,
		config:            cfg,
		hotThreshold:      hotThreshold,
		windowDuration:    windowDuration,
		maxHotPages:       maxHotPages,
		maxMembersPerPage: maxMembersPerPage,
		cleanupInterval:   cleanupInterval,
		shutdown:          make(chan struct{}),
		hotPagesCache:     make(map[string]bool),
	}

	// Start cleanup goroutine
	go tracker.StartCleanup()

	return tracker
}

// ProcessEdit - First Stage: Activity Counter (Promotion Gate)
// Purpose: Lightweight tracking before promotion
func (h *HotPageTracker) ProcessEdit(ctx context.Context, edit *models.WikipediaEdit) error {
	activityKey := fmt.Sprintf("activity:%s", edit.Title)
	
	// INCR the key
	count, err := h.redis.Incr(ctx, activityKey).Result()
	if err != nil {
		return fmt.Errorf("failed to increment activity counter: %w", err)
	}
	
	// If count = 1 (first edit), set TTL to 10 minutes
	if count == 1 {
		err = h.redis.Expire(ctx, activityKey, 10*time.Minute).Err()
		if err != nil {
			log.Printf("Failed to set TTL on activity key %s: %v", activityKey, err)
		}
	}
	
	// Increment activity counter metric
	metrics.ActivityCounterTotal.WithLabelValues().Inc()
	
	// If count >= hotThreshold, promote to hot tracking
	if count >= int64(h.hotThreshold) {
		return h.promoteToHot(ctx, edit)
	}
	
	// Otherwise, just return (don't create full tracking)
	return nil
}

// promoteToHot - Hot Page Promotion
// Purpose: Upgrade page to full tracking
func (h *HotPageTracker) promoteToHot(ctx context.Context, edit *models.WikipediaEdit) error {
	// Check circuit breaker: If current hot pages >= maxHotPages
	currentCount, err := h.GetHotPagesCount(ctx)
	if err != nil {
		log.Printf("Failed to get hot pages count during promotion: %v", err)
	}
	
	if currentCount >= h.maxHotPages {
		log.Printf("Circuit breaker: Rejecting promotion of page %s (current: %d, max: %d)", 
			edit.Title, currentCount, h.maxHotPages)
		metrics.PromotionRejectedTotal.WithLabelValues().Inc()
		return nil // Graceful degradation
	}
	
	windowKey := fmt.Sprintf("hot:window:%s", edit.Title)
	metadataKey := fmt.Sprintf("hot:meta:%s", edit.Title)
	
	// Use edit's timestamp if available, otherwise use current time
	timestamp := time.Now().Unix()
	if edit.Timestamp > 0 {
		timestamp = edit.Timestamp
	}
	
	// Use Redis pipeline for atomic operations
	pipe := h.redis.Pipeline()
	
	// Create member string: nanosecond timestamp:edit_id for uniqueness
	nanoTs := time.Now().UnixNano()
	member := fmt.Sprintf("%d:%d", nanoTs, edit.ID)
	
	// ZADD to window (score=timestamp, member=nanoTs:edit_id)
	pipe.ZAdd(ctx, windowKey, redis.Z{Score: float64(timestamp), Member: member})
	
	// ZREMRANGEBYSCORE to remove old entries (before window_duration)
	cutoffTime := timestamp - int64(h.windowDuration.Seconds())
	pipe.ZRemRangeByScore(ctx, windowKey, "-inf", fmt.Sprintf("%.0f", float64(cutoffTime)))
	
	// ZREMRANGEBYRANK to cap at maxMembersPerPage (keep newest entries)
	pipe.ZRemRangeByRank(ctx, windowKey, 0, int64(-h.maxMembersPerPage-1))
	
	// HINCRBY on metadata "edit_count"
	pipe.HIncrBy(ctx, metadataKey, "edit_count", 1)
	
	// HSET on metadata "last_edit" timestamp
	pipe.HSet(ctx, metadataKey, "last_edit", timestamp)
	
	// Update unique editors (append if new)
	if edit.User != "" {
		pipe.HSetNX(ctx, metadataKey, fmt.Sprintf("editor:%s", edit.User), timestamp)
	}
	
	// Set byte change
	byteChange := edit.Length.New - edit.Length.Old
	pipe.HSet(ctx, metadataKey, "last_byte_change", byteChange)
	
	// EXPIRE window key to windowDuration + buffer
	bufferDuration := h.windowDuration + (10 * time.Minute)
	pipe.Expire(ctx, windowKey, bufferDuration)
	
	// EXPIRE metadata key to windowDuration + buffer
	pipe.Expire(ctx, metadataKey, bufferDuration)
	
	// Execute pipeline
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to execute promotion pipeline: %w", err)
	}
	
	// Invalidate hot pages cache so GetHotPagesCount reflects the new page
	h.mu.Lock()
	if h.hotPagesCache != nil {
		h.hotPagesCache[edit.Title] = true
	}
	h.mu.Unlock()
	
	// Increment hot pages promoted metric
	metrics.HotPagesPromotedTotal.WithLabelValues().Inc()
	
	log.Printf("Page promoted to hot tracking: %s", edit.Title)
	return nil
}

// AddEditToWindow - Add edit to existing hot page window
// Purpose: Add edit to existing hot page window
func (h *HotPageTracker) AddEditToWindow(ctx context.Context, pageTitle string, edit *models.WikipediaEdit) error {
	windowKey := fmt.Sprintf("hot:window:%s", pageTitle)
	
	// Check if page is hot (EXISTS hot:window:{page})
	exists, err := h.redis.Exists(ctx, windowKey).Result()
	if err != nil {
		return fmt.Errorf("failed to check if page is hot: %w", err)
	}
	
	if exists == 0 {
		// Not hot, should not happen (defensive)
		return nil
	}
	
	metadataKey := fmt.Sprintf("hot:meta:%s", pageTitle)
	
	// Use edit's timestamp if available, otherwise use current time
	timestamp := time.Now().Unix()
	if edit.Timestamp > 0 {
		timestamp = edit.Timestamp
	}
	
	// Create member string: "{nanoTs}:{edit_id}" for uniqueness
	nanoTs := time.Now().UnixNano()
	member := fmt.Sprintf("%d:%d", nanoTs, edit.ID)
	
	// Pipeline operations
	pipe := h.redis.Pipeline()
	
	// ZADD to window
	pipe.ZAdd(ctx, windowKey, redis.Z{Score: float64(timestamp), Member: member})
	
	// ZREMRANGEBYSCORE (remove old)
	cutoffTime := timestamp - int64(h.windowDuration.Seconds())
	pipe.ZRemRangeByScore(ctx, windowKey, "-inf", fmt.Sprintf("%.0f", float64(cutoffTime)))
	
	// ZREMRANGEBYRANK (cap size)
	pipe.ZRemRangeByRank(ctx, windowKey, 0, int64(-h.maxMembersPerPage-1))
	
	// HINCRBY edit_count
	pipe.HIncrBy(ctx, metadataKey, "edit_count", 1)
	
	// HSET last_byte_change
	byteChange := edit.Length.New - edit.Length.Old
	pipe.HSet(ctx, metadataKey, "last_byte_change", byteChange)
	
	// HSET unique_editors (append if new)
	if edit.User != "" {
		pipe.HSetNX(ctx, metadataKey, fmt.Sprintf("editor:%s", edit.User), timestamp)
	}
	
	// EXPIRE window and metadata
	bufferDuration := h.windowDuration + (10 * time.Minute)
	pipe.Expire(ctx, windowKey, bufferDuration)
	pipe.Expire(ctx, metadataKey, bufferDuration)
	
	// Execute pipeline
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to execute add edit pipeline: %w", err)
	}
	
	return nil
}

// GetPageWindow - Retrieve edits in time window
// Purpose: Retrieve edits in time window
func (h *HotPageTracker) GetPageWindow(ctx context.Context, pageTitle string, startTime, endTime time.Time) ([]string, error) {
	windowKey := fmt.Sprintf("hot:window:%s", pageTitle)
	
	// ZRANGEBYSCORE with start and end timestamps
	startScore := float64(startTime.Unix())
	endScore := float64(endTime.Unix())
	
	members, err := h.redis.ZRangeByScore(ctx, windowKey, &redis.ZRangeBy{
		Min: fmt.Sprintf("%.0f", startScore),
		Max: fmt.Sprintf("%.0f", endScore),
	}).Result()
	
	if err != nil {
		if err == redis.Nil {
			return []string{}, nil // Empty slice if page not hot
		}
		return nil, fmt.Errorf("failed to get page window: %w", err)
	}
	
	// Parse members (extract timestamps and IDs)
	editRefs := make([]string, 0, len(members))
	for _, member := range members {
		editRefs = append(editRefs, member)
	}
	
	return editRefs, nil
}

// GetPageStats - Get statistics for spike detection
// Purpose: Get statistics for spike detection
func (h *HotPageTracker) GetPageStats(ctx context.Context, pageTitle string) (*PageStats, error) {
	windowKey := fmt.Sprintf("hot:window:%s", pageTitle)
	metadataKey := fmt.Sprintf("hot:meta:%s", pageTitle)
	
	// Check if hot page
	exists, err := h.redis.Exists(ctx, windowKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to check if page exists: %w", err)
	}
	
	if exists == 0 {
		// Return minimal stats for non-hot page
		return &PageStats{
			EditsLastHour:  0,
			EditsLast5Min:  0,
			UniqueEditors:  []string{},
			LastByteChange: 0,
			TotalEdits:     0,
		}, nil
	}
	
	// Get metadata hash
	metadata, err := h.redis.HGetAll(ctx, metadataKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get page metadata: %w", err)
	}
	
	now := time.Now().Unix()
	
	// Calculate stats
	// Edits in last 1 hour
	oneHourAgo := now - 3600
	editsLastHour, err := h.redis.ZCount(ctx, windowKey, fmt.Sprintf("%.0f", float64(oneHourAgo)), "+inf").Result()
	if err != nil {
		editsLastHour = 0
	}
	
	// Edits in last 5 minutes
	fiveMinAgo := now - 300
	editsLast5Min, err := h.redis.ZCount(ctx, windowKey, fmt.Sprintf("%.0f", float64(fiveMinAgo)), "+inf").Result()
	if err != nil {
		editsLast5Min = 0
	}
	
	// Parse unique editors from metadata
	uniqueEditors := make([]string, 0)
	for key := range metadata {
		if strings.HasPrefix(key, "editor:") {
			editor := strings.TrimPrefix(key, "editor:")
			uniqueEditors = append(uniqueEditors, editor)
		}
	}
	
	// Last byte change
	var lastByteChange int64
	if byteChangeStr, exists := metadata["last_byte_change"]; exists {
		lastByteChange, _ = strconv.ParseInt(byteChangeStr, 10, 64)
	}
	
	// Total edits
	var totalEdits int64
	if totalEditsStr, exists := metadata["edit_count"]; exists {
		totalEdits, _ = strconv.ParseInt(totalEditsStr, 10, 64)
	}
	
	return &PageStats{
		EditsLastHour:  editsLastHour,
		EditsLast5Min:  editsLast5Min,
		UniqueEditors:  uniqueEditors,
		LastByteChange: lastByteChange,
		TotalEdits:     totalEdits,
	}, nil
}

// StartCleanup - Background goroutine for cleanup
// Purpose: Background goroutine for cleanup
func (h *HotPageTracker) StartCleanup() {
	h.mu.Lock()
	if h.cleanupRunning {
		h.mu.Unlock()
		return
	}
	h.cleanupRunning = true
	h.mu.Unlock()
	
	ticker := time.NewTicker(h.cleanupInterval)
	defer ticker.Stop()
	
	log.Printf("Hot pages cleanup started with interval: %v", h.cleanupInterval)
	
	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			cleaned, err := h.cleanupStaleHotPages(ctx)
			if err != nil {
				log.Printf("Cleanup failed: %v", err)
			} else {
				log.Printf("Cleanup completed: removed %d stale hot pages", cleaned)
			}
			metrics.CleanupRunsTotal.WithLabelValues().Inc()
			cancel()
			
		case <-h.shutdown:
			log.Printf("Hot pages cleanup stopped")
			return
		}
	}
}

// cleanupStaleHotPages - Remove empty or expired hot pages
// Purpose: Remove empty or expired hot pages
func (h *HotPageTracker) cleanupStaleHotPages(ctx context.Context) (int, error) {
	var cursor uint64
	var cleanedCount int
	keysScanned := 0
	
	for {
		// SCAN for pattern "hot:window:*" 
		keys, nextCursor, err := h.redis.Scan(ctx, cursor, "hot:window:*", 100).Result()
		if err != nil {
			return cleanedCount, fmt.Errorf("failed to scan hot window keys: %w", err)
		}
		
		for _, key := range keys {
			keysScanned++
			
			// ZCARD to get member count
			count, err := h.redis.ZCard(ctx, key).Result()
			if err != nil {
				log.Printf("Failed to get count for key %s: %v", key, err)
				continue
			}
			
			// Check TTL
			ttl, err := h.redis.TTL(ctx, key).Result()
			if err != nil {
				log.Printf("Failed to get TTL for key %s: %v", key, err)
				continue
			}
			
			// If count = 0 OR TTL expired, delete
			if count == 0 || ttl < 0 {
				// Extract page title from key
				pageTitle := strings.TrimPrefix(key, "hot:window:")
				metadataKey := fmt.Sprintf("hot:meta:%s", pageTitle)
				
				// DELETE window key and metadata key
				pipe := h.redis.Pipeline()
				pipe.Del(ctx, key)
				pipe.Del(ctx, metadataKey)
				_, err = pipe.Exec(ctx)
				
				if err != nil {
					log.Printf("Failed to delete stale hot page %s: %v", pageTitle, err)
				} else {
					cleanedCount++
					metrics.HotPagesExpiredTotal.WithLabelValues().Inc()
				}
			}
		}
		
		cursor = nextCursor
		if cursor == 0 {
			break // Completed full scan
		}
		
		// Limit scan to prevent long blocking
		if keysScanned >= 1000 {
			break
		}
	}
	
	return cleanedCount, nil
}

// GetHotPagesCount - Get current number of hot pages
// Purpose: Get current number of hot pages
func (h *HotPageTracker) GetHotPagesCount(ctx context.Context) (int, error) {
	h.mu.RLock()
	// Check cache (10 seconds TTL)
	if time.Now().Before(h.cacheExpiry) {
		count := len(h.hotPagesCache)
		h.mu.RUnlock()
		return count, nil
	}
	h.mu.RUnlock()
	
	// Refresh cache
	var cursor uint64
	hotPages := make(map[string]bool)
	
	for {
		keys, nextCursor, err := h.redis.Scan(ctx, cursor, "hot:window:*", 100).Result()
		if err != nil {
			return 0, fmt.Errorf("failed to scan hot pages: %w", err)
		}
		
		for _, key := range keys {
			pageTitle := strings.TrimPrefix(key, "hot:window:")
			hotPages[pageTitle] = true
		}
		
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	
	// Update cache
	h.mu.Lock()
	h.hotPagesCache = hotPages
	h.cacheExpiry = time.Now().Add(10 * time.Second)
	count := len(hotPages)
	h.mu.Unlock()
	
	// Update gauge metric
	metrics.HotPagesTracked.WithLabelValues().Set(float64(count))
	
	return count, nil
}

// IsHot - Check if page currently hot
func (h *HotPageTracker) IsHot(ctx context.Context, pageTitle string) (bool, error) {
	windowKey := fmt.Sprintf("hot:window:%s", pageTitle)
	exists, err := h.redis.Exists(ctx, windowKey).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check if page is hot: %w", err)
	}
	return exists > 0, nil
}

// GetHotPagesList - Return list of all currently hot pages
func (h *HotPageTracker) GetHotPagesList(ctx context.Context) ([]string, error) {
	var cursor uint64
	var hotPages []string
	
	for {
		keys, nextCursor, err := h.redis.Scan(ctx, cursor, "hot:window:*", 100).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan for hot pages: %w", err)
		}
		
		for _, key := range keys {
			pageTitle := strings.TrimPrefix(key, "hot:window:")
			hotPages = append(hotPages, pageTitle)
		}
		
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	
	return hotPages, nil
}

// Shutdown gracefully stops the cleanup goroutine
func (h *HotPageTracker) Shutdown() {
	close(h.shutdown)
}

// Legacy compatibility methods (for existing code)

// HotPage represents a hot page with metadata (for compatibility)
type HotPage struct {
	PageName     string    `json:"page_name"`
	EditCount    int       `json:"edit_count"`
	EditorsCount int       `json:"editors_count"`
	LastActivity time.Time `json:"last_activity"`
}

// TrackEdit is a legacy method that calls ProcessEdit
func (h *HotPageTracker) TrackEdit(ctx context.Context, edit *models.WikipediaEdit) error {
	return h.ProcessEdit(ctx, edit)
}

// GetHotPages returns hot pages in legacy format
func (h *HotPageTracker) GetHotPages(ctx context.Context, limit int) ([]HotPage, error) {
	pagesList, err := h.GetHotPagesList(ctx)
	if err != nil {
		return nil, err
	}
	
	hotPages := make([]HotPage, 0, min(limit, len(pagesList)))
	
	for i, pageTitle := range pagesList {
		if i >= limit {
			break
		}
		
		stats, err := h.GetPageStats(ctx, pageTitle)
		if err != nil {
			log.Printf("Failed to get stats for page %s: %v", pageTitle, err)
			continue
		}
		
		hotPages = append(hotPages, HotPage{
			PageName:     pageTitle,
			EditCount:    int(stats.TotalEdits),
			EditorsCount: len(stats.UniqueEditors),
			LastActivity: time.Now(), // Approximate
		})
	}
	
	return hotPages, nil
}

// IsHotPage is a legacy method that calls IsHot 
func (h *HotPageTracker) IsHotPage(ctx context.Context, wiki, title string) (bool, error) {
	return h.IsHot(ctx, title)
}

// GetPageEditCount returns the current edit count for a page (legacy compatibility)
func (h *HotPageTracker) GetPageEditCount(ctx context.Context, wiki, title string) (int, error) {
	// First check if the page is hot
	isHot, err := h.IsHot(ctx, title)
	if err != nil {
		return 0, err
	}
	
	if !isHot {
		// Check activity counter for non-hot pages
		activityKey := fmt.Sprintf("activity:%s", title)
		count, err := h.redis.Get(ctx, activityKey).Int()
		if err == redis.Nil {
			return 0, nil
		}
		if err != nil {
			return 0, fmt.Errorf("failed to get activity count: %w", err)
		}
		return count, nil
	}
	
	// For hot pages, get from metadata
	metadataKey := fmt.Sprintf("hot:meta:%s", title)
	countStr, err := h.redis.HGet(ctx, metadataKey, "edit_count").Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get hot page edit count: %w", err)
	}
	
	count, err := strconv.Atoi(countStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse edit count: %w", err)
	}
	
	return count, nil
}

// Helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}