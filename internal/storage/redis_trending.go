package storage

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
)

// TimeProvider allows for mockable time in tests
type TimeProvider interface {
	Now() time.Time
}

// RealTimeProvider uses actual system time
type RealTimeProvider struct{}

func (r *RealTimeProvider) Now() time.Time {
	return time.Now()
}

// MockTimeProvider allows setting custom time for tests
type MockTimeProvider struct {
	currentTime time.Time
}

// NewMockTimeProvider creates a new mock time provider starting at the current time
func NewMockTimeProvider() *MockTimeProvider {
	return &MockTimeProvider{currentTime: time.Now()}
}

func (m *MockTimeProvider) Now() time.Time {
	return m.currentTime
}

func (m *MockTimeProvider) SetTime(t time.Time) {
	m.currentTime = t
}

func (m *MockTimeProvider) FastForward(d time.Duration) {
	m.currentTime = m.currentTime.Add(d)
}

// AdvanceTime is an alias for FastForward
func (m *MockTimeProvider) AdvanceTime(d time.Duration) {
	m.FastForward(d)
}

// TrendingScorer manages trending page scoring and tracking in Redis with lazy decay
type TrendingScorer struct {
	redis            *redis.Client
	config           *config.TrendingConfig
	timeProvider     TimeProvider
	halfLifeMinutes  float64
	maxPages         int
	pruneInterval    time.Duration
	metrics          *TrendingMetrics
	ctx              context.Context
	cancel           context.CancelFunc
	pruneWg          sync.WaitGroup
}

// TrendingEntry represents a trending page entry with computed scores
type TrendingEntry struct {
	PageTitle    string  `json:"page_title"`
	RawScore     float64 `json:"raw_score"`
	LastUpdated  int64   `json:"last_updated"`
	CurrentScore float64 `json:"current_score"`
	ServerURL    string  `json:"server_url,omitempty"`
}

// TrendingMetrics groups all trending-related Prometheus metrics
type TrendingMetrics struct {
	UpdatesTotal    prometheus.Counter
	PruneRunsTotal  prometheus.Counter
	PruneCountTotal prometheus.Counter
}

// NewTrendingScorer creates a new trending scorer with lazy decay
func NewTrendingScorer(redis *redis.Client, config *config.TrendingConfig) *TrendingScorer {
	return newTrendingScorer(redis, config, &RealTimeProvider{}, true)
}

// NewTrendingScorerForTest creates a new trending scorer for tests (no metrics registration)
func NewTrendingScorerForTest(redis *redis.Client, config *config.TrendingConfig) *TrendingScorer {
	return newTrendingScorer(redis, config, &RealTimeProvider{}, false)
}

// NewTrendingScorerWithTimeProvider creates a trending scorer with custom time provider for testing
func NewTrendingScorerWithTimeProvider(redis *redis.Client, config *config.TrendingConfig, timeProvider TimeProvider) *TrendingScorer {
	return newTrendingScorer(redis, config, timeProvider, false)
}

// newTrendingScorer is the internal constructor
func newTrendingScorer(redis *redis.Client, config *config.TrendingConfig, timeProvider TimeProvider, registerMetrics bool) *TrendingScorer {
	ctx, cancel := context.WithCancel(context.Background())
	
	// Set defaults
	halfLifeMinutes := 30.0
	if config.HalfLifeMinutes > 0 {
		halfLifeMinutes = config.HalfLifeMinutes
	}
	
	maxPages := 1000
	if config.MaxPages > 0 {
		maxPages = config.MaxPages
	}
	
	pruneInterval := 10 * time.Minute
	if config.PruneInterval > 0 {
		pruneInterval = config.PruneInterval
	}
	
	// Initialize metrics
	trendingMetrics := &TrendingMetrics{
		UpdatesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "trending_updates_total",
			Help: "Total trending score updates",
		}),
		PruneRunsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "trending_prune_runs_total", 
			Help: "Total trending prune runs",
		}),
		PruneCountTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "trending_pruned_total",
			Help: "Total trending entries pruned",
		}),
	}
	
	// Only register metrics if requested
	if registerMetrics {
		prometheus.MustRegister(trendingMetrics.UpdatesTotal)
		prometheus.MustRegister(trendingMetrics.PruneRunsTotal)
		prometheus.MustRegister(trendingMetrics.PruneCountTotal)
	}
	
	return &TrendingScorer{
		redis:           redis,
		config:          config,
		timeProvider:    timeProvider,
		halfLifeMinutes: halfLifeMinutes,
		maxPages:        maxPages,
		pruneInterval:   pruneInterval,
		metrics:         trendingMetrics,
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Stop gracefully shuts down the trending scorer
func (t *TrendingScorer) Stop() {
	t.cancel()
	t.pruneWg.Wait()
}

// IncrementScore adds score for an edit, applying decay lazily
func (t *TrendingScorer) IncrementScore(pageTitle string, scoreIncrement float64) error {
	now := t.timeProvider.Now().Unix()
	
	// Keys
	pageKey := fmt.Sprintf("trending:%s", pageTitle)
	globalKey := "trending:global"
	
	// Get current values
	ctx := context.Background()
	pipe := t.redis.Pipeline()
	
	rawScoreCmd := pipe.HGet(ctx, pageKey, "raw_score")
	lastUpdatedCmd := pipe.HGet(ctx, pageKey, "last_updated")
	
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return fmt.Errorf("failed to get current values: %w", err)
	}
	
	// Parse existing values
	var rawScore float64
	var lastUpdated int64
	
	if rawScoreCmd.Err() == nil {
		rawScore, _ = strconv.ParseFloat(rawScoreCmd.Val(), 64)
	}
	
	if lastUpdatedCmd.Err() == nil {
		lastUpdated, _ = strconv.ParseInt(lastUpdatedCmd.Val(), 10, 64)
	}
	
	// Apply lazy decay if entry exists
	if lastUpdated > 0 {
		elapsedMinutes := float64(now-lastUpdated) / 60.0
		decayFactor := math.Pow(0.5, elapsedMinutes/t.halfLifeMinutes)
		rawScore *= decayFactor
	}
	
	// Add increment
	rawScore += scoreIncrement
	
	// Update Redis with pipeline
	pipe2 := t.redis.Pipeline()
	pipe2.HSet(ctx, pageKey, "raw_score", rawScore)
	pipe2.HSet(ctx, pageKey, "last_updated", now)
	pipe2.Expire(ctx, pageKey, 24*time.Hour)
	pipe2.ZAdd(ctx, globalKey, redis.Z{Score: rawScore, Member: pageTitle})
	
	_, err = pipe2.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update trending score: %w", err)
	}
	
	// Update metrics
	t.metrics.UpdatesTotal.Inc()
	
	return nil
}

// GetTopTrending returns the top N trending pages with current decayed scores
func (t *TrendingScorer) GetTopTrending(limit int) ([]*TrendingEntry, error) {
	ctx := context.Background()
	globalKey := "trending:global"
	now := t.timeProvider.Now().Unix()
	
	// Get top pages from sorted set (may have stale scores)
	results, err := t.redis.ZRevRangeWithScores(ctx, globalKey, 0, int64(limit*2-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get trending pages: %w", err)
	}
	
	entries := make([]*TrendingEntry, 0, len(results))

	// Pipeline all HGetAll calls to avoid N+1 round-trips.
	type pageResult struct {
		title string
		cmd   *redis.MapStringStringCmd
	}
	pipe := t.redis.Pipeline()
	pageResults := make([]pageResult, 0, len(results))
	for _, result := range results {
		pageTitle := result.Member.(string)
		pageKey := fmt.Sprintf("trending:%s", pageTitle)
		cmd := pipe.HGetAll(ctx, pageKey)
		pageResults = append(pageResults, pageResult{title: pageTitle, cmd: cmd})
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to pipeline trending page data: %w", err)
	}

	// Process results from the pipeline
	for _, pr := range pageResults {
		data, err := pr.cmd.Result()
		if err != nil {
			continue // Skip pages that no longer exist
		}
		
		rawScore, err := strconv.ParseFloat(data["raw_score"], 64)
		if err != nil {
			continue
		}
		
		lastUpdated, err := strconv.ParseInt(data["last_updated"], 10, 64)
		if err != nil {
			continue
		}
		
		// Calculate current decayed score
		elapsedMinutes := float64(now-lastUpdated) / 60.0
		decayFactor := math.Pow(0.5, elapsedMinutes/t.halfLifeMinutes)
		currentScore := rawScore * decayFactor
		
		entries = append(entries, &TrendingEntry{
			PageTitle:    pr.title,
			RawScore:     rawScore,
			LastUpdated:  lastUpdated,
			CurrentScore: currentScore,
			ServerURL:    data["server_url"],
		})
	}
	
	// Sort by current score (may differ from stored score due to decay)
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].CurrentScore < entries[j].CurrentScore {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
	
	// Return only requested limit
	if len(entries) > limit {
		entries = entries[:limit]
	}
	
	return entries, nil
}

// GetTrendingRank returns the rank of a specific page (0-indexed, -1 if not found)
func (t *TrendingScorer) GetTrendingRank(pageTitle string) (int, error) {
	ctx := context.Background()
	globalKey := "trending:global"
	
	rank, err := t.redis.ZRevRank(ctx, globalKey, pageTitle).Result()
	if err == redis.Nil {
		return -1, nil // Page not found
	}
	if err != nil {
		return -1, fmt.Errorf("failed to get trending rank: %w", err)
	}
	
	return int(rank), nil
}

// GetPageRank returns the trending rank for a page (compatibility method for indexing strategy)
// Returns 1-based rank (like the old RedisTrending), or 0 if not found
func (t *TrendingScorer) GetPageRank(ctx context.Context, wiki, title string) (int, error) {
	// Create page key - in our new system we just use title, but keeping compatibility
	pageTitle := title
	rank, err := t.GetTrendingRank(pageTitle)
	if err != nil {
		return 0, err
	}
	if rank == -1 {
		return 0, nil // Not found
	}
	return rank + 1, nil // Convert to 1-based rank
}

// calculateIncrement determines score to add for an edit
func (t *TrendingScorer) calculateIncrement(edit *models.WikipediaEdit) float64 {
	baseScore := 1.0
	
	// Large edit bonus
	byteChange := edit.ByteChange()
	if byteChange < 0 {
		byteChange = -byteChange
	}
	if byteChange > 1000 {
		baseScore *= 1.5
	}
	
	// Bot penalty
	if edit.Bot {
		baseScore *= 0.5
	}
	
	// New page bonus
	if edit.Type == "new" {
		baseScore *= 2.0
	}
	
	return baseScore
}

// StartPruning starts background cleanup task
func (t *TrendingScorer) StartPruning() {
	if !t.config.Enabled {
		return
	}
	
	t.pruneWg.Add(1)
	go func() {
		defer t.pruneWg.Done()
		ticker := time.NewTicker(t.pruneInterval)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				count, err := t.pruneTrendingSet()
				if err != nil {
					// Log error but continue
					continue
				}
				t.metrics.PruneRunsTotal.Inc()
				t.metrics.PruneCountTotal.Add(float64(count))
				
			case <-t.ctx.Done():
				return
			}
		}
	}()
}

// pruneTrendingSet removes dead/low-score entries
func (t *TrendingScorer) pruneTrendingSet() (int, error) {
	ctx := context.Background()
	globalKey := "trending:global"
	now := t.timeProvider.Now().Unix()
	pruneCount := 0
	
	// Remove very low scores from global set
	removed, err := t.redis.ZRemRangeByScore(ctx, globalKey, "-inf", "0.01").Result()
	if err != nil {
		return 0, fmt.Errorf("failed to remove low scores: %w", err)
	}
	pruneCount += int(removed)
	
	// Cap set to max pages
	count, err := t.redis.ZCard(ctx, globalKey).Result()
	if err != nil {
		return pruneCount, fmt.Errorf("failed to get set size: %w", err)
	}
	
	if count > int64(t.maxPages) {
		toRemove := count - int64(t.maxPages)
		removed, err := t.redis.ZRemRangeByRank(ctx, globalKey, 0, toRemove-1).Result()
		if err != nil {
			return pruneCount, fmt.Errorf("failed to cap set size: %w", err)
		}
		pruneCount += int(removed)
	}
	
	// Clean up individual page keys for very old entries
	pattern := "trending:*"
	iter := t.redis.Scan(ctx, 0, pattern, 100).Iterator()
	
	for iter.Next(ctx) {
		pageKey := iter.Val()
		if pageKey == globalKey {
			continue
		}
		
		// Get page data
		data, err := t.redis.HGetAll(ctx, pageKey).Result()
		if err != nil {
			continue
		}
		
		rawScoreStr, hasScore := data["raw_score"]
		lastUpdatedStr, hasTime := data["last_updated"]
		
		if !hasScore || !hasTime {
			// Invalid data, remove it
			t.redis.Del(ctx, pageKey)
			pruneCount++
			continue
		}
		
		rawScore, err := strconv.ParseFloat(rawScoreStr, 64)
		if err != nil {
			t.redis.Del(ctx, pageKey)
			pruneCount++
			continue
		}
		
		lastUpdated, err := strconv.ParseInt(lastUpdatedStr, 10, 64)
		if err != nil {
			t.redis.Del(ctx, pageKey)
			pruneCount++
			continue
		}
		
		// Calculate current score
		elapsedMinutes := float64(now-lastUpdated) / 60.0
		decayFactor := math.Pow(0.5, elapsedMinutes/t.halfLifeMinutes)
		currentScore := rawScore * decayFactor
		
		// Remove if score too low
		if currentScore < 0.01 {
			// Extract page title from key
			pageTitle := pageKey[9:] // Remove "trending:" prefix
			
			// Remove from both places
			t.redis.Del(ctx, pageKey)
			t.redis.ZRem(ctx, globalKey, pageTitle)
			pruneCount++
		}
	}
	
	if err := iter.Err(); err != nil {
		return pruneCount, fmt.Errorf("scan error: %w", err)
	}
	
	return pruneCount, nil
}

// ProcessEdit processes an edit and updates trending scores (for aggregator)
func (t *TrendingScorer) ProcessEdit(edit *models.WikipediaEdit) error {
	if !t.config.Enabled {
		return nil
	}
	
	increment := t.calculateIncrement(edit)
	if err := t.IncrementScore(edit.Title, increment); err != nil {
		return err
	}

	// Persist server_url so API can build correct wiki links for any language
	if edit.ServerURL != "" {
		pageKey := fmt.Sprintf("trending:%s", edit.Title)
		_ = t.redis.HSetNX(context.Background(), pageKey, "server_url", edit.ServerURL).Err()
	}

	return nil
}