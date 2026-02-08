package storage

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
)

// IndexingStrategy determines which edits should be indexed in Elasticsearch
type IndexingStrategy struct {
	config            *config.SelectiveCriteria
	redis             *redis.Client
	trending          *RedisTrending
	hotPages          *RedisHotPages
	watchlist         map[string]bool
	watchlistMu       sync.RWMutex
	contextCache      map[string]*PageContext
	contextCacheMu    sync.RWMutex
	contextCacheTTL   time.Duration
}

// PageContext contains the current status of a page for indexing decisions
type PageContext struct {
	TrendingRank    int       `json:"trending_rank"`
	IsHotPage       bool      `json:"is_hot_page"`
	IsEditWar       bool      `json:"is_edit_war"`
	IsSpiking       bool      `json:"is_spiking"`
	SpikeRatio      float64   `json:"spike_ratio"`
	EditCount       int       `json:"edit_count"`
	LastUpdated     time.Time `json:"last_updated"`
}

// IndexingDecision contains the result of an indexing decision
type IndexingDecision struct {
	ShouldIndex bool   `json:"should_index"`
	Reason      string `json:"reason"`
	Context     *PageContext `json:"context,omitempty"`
}

// NewIndexingStrategy creates a new indexing strategy
func NewIndexingStrategy(cfg *config.SelectiveCriteria, redisClient *redis.Client, trending *RedisTrending, hotPages *RedisHotPages) *IndexingStrategy {
	strategy := &IndexingStrategy{
		config:          cfg,
		redis:           redisClient,
		trending:        trending,
		hotPages:        hotPages,
		watchlist:       make(map[string]bool),
		contextCache:    make(map[string]*PageContext),
		contextCacheTTL: 1 * time.Second, // Brief caching to reduce Redis queries
	}

	// Initialize watchlist from Redis if it exists
	strategy.loadWatchlist(context.Background())

	return strategy
}

// ShouldIndex determines if an edit should be indexed in Elasticsearch
func (s *IndexingStrategy) ShouldIndex(ctx context.Context, edit *models.WikipediaEdit) (*IndexingDecision, error) {
	pageKey := fmt.Sprintf("%s:%s", edit.Wiki, edit.Title)

	// Check watchlist first (highest priority)
	if s.isInWatchlist(pageKey) {
		return &IndexingDecision{
			ShouldIndex: true,
			Reason:      "watchlist",
		}, nil
	}

	// Get page context (with caching)
	pageContext, err := s.getPageContext(ctx, edit.Wiki, edit.Title)
	if err != nil {
		log.Printf("Failed to get page context for %s: %v", pageKey, err)
		// Don't fail indexing decision on context errors, default to not indexing
		return &IndexingDecision{
			ShouldIndex: false,
			Reason:      "context_error",
		}, nil
	}

	decision := &IndexingDecision{
		Context: pageContext,
	}

	// Check trending status
	if pageContext.TrendingRank > 0 && pageContext.TrendingRank <= s.config.TrendingTopN {
		decision.ShouldIndex = true
		decision.Reason = fmt.Sprintf("trending_top_%d", pageContext.TrendingRank)
		return decision, nil
	}

	// Check spiking status
	if pageContext.IsSpiking && pageContext.SpikeRatio >= s.config.SpikeRatioMin {
		decision.ShouldIndex = true
		decision.Reason = fmt.Sprintf("spiking_%.2f", pageContext.SpikeRatio)
		return decision, nil
	}

	// Check edit war status
	if s.config.EditWarEnabled && pageContext.IsEditWar {
		decision.ShouldIndex = true
		decision.Reason = "edit_war"
		return decision, nil
	}

	// Check hot page status
	if pageContext.IsHotPage {
		decision.ShouldIndex = true
		decision.Reason = "hot_page"
		return decision, nil
	}

	// Default: don't index
	decision.ShouldIndex = false
	decision.Reason = "not_significant"
	return decision, nil
}

// AddToWatchlist adds a page to the watchlist for always indexing
func (s *IndexingStrategy) AddToWatchlist(ctx context.Context, wiki, title string) error {
	pageKey := fmt.Sprintf("%s:%s", wiki, title)
	
	s.watchlistMu.Lock()
	s.watchlist[pageKey] = true
	s.watchlistMu.Unlock()
	
	// Persist to Redis
	watchlistKey := "indexing:watchlist"
	err := s.redis.SAdd(ctx, watchlistKey, pageKey).Err()
	if err != nil {
		return fmt.Errorf("failed to add to watchlist: %w", err)
	}
	
	log.Printf("Added page to indexing watchlist: %s", pageKey)
	return nil
}

// RemoveFromWatchlist removes a page from the watchlist
func (s *IndexingStrategy) RemoveFromWatchlist(ctx context.Context, wiki, title string) error {
	pageKey := fmt.Sprintf("%s:%s", wiki, title)
	
	s.watchlistMu.Lock()
	delete(s.watchlist, pageKey)
	s.watchlistMu.Unlock()
	
	// Remove from Redis
	watchlistKey := "indexing:watchlist"
	err := s.redis.SRem(ctx, watchlistKey, pageKey).Err()
	if err != nil {
		return fmt.Errorf("failed to remove from watchlist: %w", err)
	}
	
	log.Printf("Removed page from indexing watchlist: %s", pageKey)
	return nil
}

// GetWatchlist returns the current watchlist
func (s *IndexingStrategy) GetWatchlist(ctx context.Context) ([]string, error) {
	s.watchlistMu.RLock()
	defer s.watchlistMu.RUnlock()
	
	pages := make([]string, 0, len(s.watchlist))
	for page := range s.watchlist {
		pages = append(pages, page)
	}
	
	return pages, nil
}

// GetIndexingStats returns statistics about indexing decisions
func (s *IndexingStrategy) GetIndexingStats(ctx context.Context) (*IndexingStats, error) {
	// Get counts from Redis counters (these would be incremented by the processor)
	statsKey := "indexing:stats"
	
	stats, err := s.redis.HGetAll(ctx, statsKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get indexing stats: %w", err)
	}
	
	indexingStats := &IndexingStats{
		TotalEdits:       parseInt64(stats["total_edits"]),
		IndexedEdits:     parseInt64(stats["indexed_edits"]),
		SkippedEdits:     parseInt64(stats["skipped_edits"]),
		WatchlistIndex:   parseInt64(stats["watchlist_index"]),
		TrendingIndex:    parseInt64(stats["trending_index"]),
		SpikingIndex:     parseInt64(stats["spiking_index"]),
		EditWarIndex:     parseInt64(stats["editwar_index"]),
		HotPageIndex:     parseInt64(stats["hotpage_index"]),
	}
	
	if indexingStats.TotalEdits > 0 {
		indexingStats.IndexingRate = float64(indexingStats.IndexedEdits) / float64(indexingStats.TotalEdits)
	}
	
	return indexingStats, nil
}

// UpdateIndexingStats updates statistics counters
func (s *IndexingStrategy) UpdateIndexingStats(ctx context.Context, decision *IndexingDecision) error {
	statsKey := "indexing:stats"
	
	// Increment total edits
	s.redis.HIncrBy(ctx, statsKey, "total_edits", 1)
	
	if decision.ShouldIndex {
		s.redis.HIncrBy(ctx, statsKey, "indexed_edits", 1)
		
		// Increment specific reason counters
		switch {
		case strings.HasPrefix(decision.Reason, "watchlist"):
			s.redis.HIncrBy(ctx, statsKey, "watchlist_index", 1)
		case strings.HasPrefix(decision.Reason, "trending"):
			s.redis.HIncrBy(ctx, statsKey, "trending_index", 1)
		case strings.HasPrefix(decision.Reason, "spiking"):
			s.redis.HIncrBy(ctx, statsKey, "spiking_index", 1)
		case decision.Reason == "edit_war":
			s.redis.HIncrBy(ctx, statsKey, "editwar_index", 1)
		case decision.Reason == "hot_page":
			s.redis.HIncrBy(ctx, statsKey, "hotpage_index", 1)
		}
	} else {
		s.redis.HIncrBy(ctx, statsKey, "skipped_edits", 1)
	}
	
	return nil
}

// getPageContext retrieves current page status with caching
func (s *IndexingStrategy) getPageContext(ctx context.Context, wiki, title string) (*PageContext, error) {
	pageKey := fmt.Sprintf("%s:%s", wiki, title)
	
	// Check cache first
	s.contextCacheMu.RLock()
	if cached, exists := s.contextCache[pageKey]; exists {
		if time.Since(cached.LastUpdated) < s.contextCacheTTL {
			s.contextCacheMu.RUnlock()
			return cached, nil
		}
	}
	s.contextCacheMu.RUnlock()
	
	// Build fresh context
	context := &PageContext{
		LastUpdated: time.Now(),
	}
	
	// Get trending rank
	if s.trending != nil {
		rank, err := s.trending.GetPageRank(ctx, wiki, title)
		if err != nil {
			log.Printf("Failed to get trending rank for %s: %v", pageKey, err)
		} else {
			context.TrendingRank = rank
		}
	}
	
	// Check if hot page
	if s.hotPages != nil {
		isHot, err := s.hotPages.IsHotPage(ctx, wiki, title)
		if err != nil {
			log.Printf("Failed to check hot page status for %s: %v", pageKey, err)
		} else {
			context.IsHotPage = isHot
		}
		
		// Get edit count if hot
		if isHot {
			editCount, err := s.hotPages.GetPageEditCount(ctx, wiki, title)
			if err != nil {
				log.Printf("Failed to get edit count for %s: %v", pageKey, err)
			} else {
				context.EditCount = editCount
			}
		}
	}
	
	// Check spiking status
	spikingKey := fmt.Sprintf("spike:%s:%s", wiki, title)
	spikeExists, err := s.redis.Exists(ctx, spikingKey).Result()
	if err != nil {
		log.Printf("Failed to check spike status for %s: %v", pageKey, err)
	} else {
		context.IsSpiking = spikeExists == 1
		
		if context.IsSpiking {
			// Get spike ratio
			ratioStr, err := s.redis.Get(ctx, spikingKey).Result()
			if err == nil {
				fmt.Sscanf(ratioStr, "%f", &context.SpikeRatio)
			}
		}
	}
	
	// Check edit war status
	editWarKey := fmt.Sprintf("editwar:%s:%s", wiki, title)
	editWarExists, err := s.redis.Exists(ctx, editWarKey).Result()
	if err != nil {
		log.Printf("Failed to check edit war status for %s: %v", pageKey, err)
	} else {
		context.IsEditWar = editWarExists == 1
	}
	
	// Cache the context
	s.contextCacheMu.Lock()
	s.contextCache[pageKey] = context
	s.contextCacheMu.Unlock()
	
	return context, nil
}

// isInWatchlist checks if a page is in the watchlist
func (s *IndexingStrategy) isInWatchlist(pageKey string) bool {
	s.watchlistMu.RLock()
	defer s.watchlistMu.RUnlock()
	return s.watchlist[pageKey]
}

// loadWatchlist loads the watchlist from Redis
func (s *IndexingStrategy) loadWatchlist(ctx context.Context) {
	watchlistKey := "indexing:watchlist"
	
	pages, err := s.redis.SMembers(ctx, watchlistKey).Result()
	if err != nil {
		log.Printf("Failed to load watchlist from Redis: %v", err)
		return
	}
	
	s.watchlistMu.Lock()
	s.watchlist = make(map[string]bool)
	for _, page := range pages {
		s.watchlist[page] = true
	}
	s.watchlistMu.Unlock()
	
	log.Printf("Loaded %d pages from indexing watchlist", len(pages))
}

// parseInt64 safely parses a string to int64
func parseInt64(s string) int64 {
	var result int64
	fmt.Sscanf(s, "%d", &result)
	return result
}

// IndexingStats contains statistics about indexing decisions
type IndexingStats struct {
	TotalEdits     int64   `json:"total_edits"`
	IndexedEdits   int64   `json:"indexed_edits"`
	SkippedEdits   int64   `json:"skipped_edits"`
	IndexingRate   float64 `json:"indexing_rate"`
	WatchlistIndex int64   `json:"watchlist_index"`
	TrendingIndex  int64   `json:"trending_index"`
	SpikingIndex   int64   `json:"spiking_index"`
	EditWarIndex   int64   `json:"editwar_index"`
	HotPageIndex   int64   `json:"hotpage_index"`
}