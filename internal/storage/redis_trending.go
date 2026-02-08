package storage

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
)

// RedisTrending manages trending page scoring and tracking in Redis
type RedisTrending struct {
	client   *redis.Client
	config   *config.TrendingConfig
	halfLife float64 // Half-life in seconds
}

// NewRedisTrending creates a new Redis trending pages tracker
func NewRedisTrending(client *redis.Client, cfg *config.TrendingConfig) *RedisTrending {
	halfLife := cfg.HalfLifeMinutes * 60 // Convert minutes to seconds
	
	return &RedisTrending{
		client:   client,
		config:   cfg,
		halfLife: halfLife,
	}
}

// UpdateScore updates the trending score for a page based on an edit
func (r *RedisTrending) UpdateScore(ctx context.Context, edit *models.WikipediaEdit) error {
	if !r.config.Enabled {
		return nil
	}

	pageKey := fmt.Sprintf("%s:%s", edit.Wiki, edit.Title)
	trendingKey := "trending:global"
	
	now := float64(time.Now().Unix())
	
	// Calculate score increment based on edit significance
	scoreIncrement := r.calculateScoreIncrement(edit)
	
	// Get current score and last update time
	currentScore, lastUpdate, err := r.getCurrentScore(ctx, pageKey)
	if err != nil {
		return fmt.Errorf("failed to get current score: %w", err)
	}
	
	// Apply decay based on time elapsed
	if lastUpdate > 0 {
		timeDelta := now - lastUpdate
		decayFactor := math.Exp(-timeDelta * math.Ln2 / r.halfLife)
		currentScore *= decayFactor
	}
	
	// Add new score increment
	newScore := currentScore + scoreIncrement
	
	// Update the trending zset
	err = r.client.ZAdd(ctx, trendingKey, redis.Z{
		Score:  newScore,
		Member: pageKey,
	}).Err()
	
	if err != nil {
		return fmt.Errorf("failed to update trending score: %w", err)
	}
	
	// Store metadata about the page's score
	metadataKey := fmt.Sprintf("trending:meta:%s", pageKey)
	metadata := map[string]interface{}{
		"last_update": now,
		"edit_count":  1, // Will be incremented if exists
	}
	
	// Update edit count if metadata exists
	if r.client.Exists(ctx, metadataKey).Val() == 1 {
		r.client.HIncrBy(ctx, metadataKey, "edit_count", 1)
		r.client.HSet(ctx, metadataKey, "last_update", now)
	} else {
		r.client.HMSet(ctx, metadataKey, metadata)
	}
	
	// Set TTL for metadata (longer than trending window)
	r.client.Expire(ctx, metadataKey, time.Duration(r.halfLife*5)*time.Second)
	
	log.Printf("Updated trending score for %s: %.2f (increment: %.2f)", 
		pageKey, newScore, scoreIncrement)
	
	return nil
}

// GetTrendingPages returns the top trending pages
func (r *RedisTrending) GetTrendingPages(ctx context.Context, limit int) ([]TrendingPage, error) {
	trendingKey := "trending:global"
	
	// Get top trending pages with scores
	results, err := r.client.ZRevRangeWithScores(ctx, trendingKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get trending pages: %w", err)
	}
	
	trendingPages := make([]TrendingPage, 0, len(results))
	
	for i, result := range results {
		pageKey := result.Member.(string)
		score := result.Score
		
		// Parse wiki and title from page key
		parts := strings.SplitN(pageKey, ":", 2)
		if len(parts) != 2 {
			continue
		}
		
		wiki := parts[0]
		title := parts[1]
		
		// Get additional metadata
		metadataKey := fmt.Sprintf("trending:meta:%s", pageKey)
		metadata, err := r.client.HGetAll(ctx, metadataKey).Result()
		if err != nil {
			log.Printf("Failed to get metadata for %s: %v", pageKey, err)
			metadata = make(map[string]string)
		}
		
		editCount := 0
		if countStr, exists := metadata["edit_count"]; exists {
			editCount, _ = strconv.Atoi(countStr)
		}
		
		var lastUpdate time.Time
		if updateStr, exists := metadata["last_update"]; exists {
			if timestamp, err := strconv.ParseFloat(updateStr, 64); err == nil {
				lastUpdate = time.Unix(int64(timestamp), 0)
			}
		}
		
		trendingPages = append(trendingPages, TrendingPage{
			Rank:       i + 1,
			Wiki:       wiki,
			Title:      title,
			Score:      score,
			EditCount:  editCount,
			LastUpdate: lastUpdate,
		})
	}
	
	return trendingPages, nil
}

// GetPageRank returns the current trending rank for a specific page (1-based)
func (r *RedisTrending) GetPageRank(ctx context.Context, wiki, title string) (int, error) {
	trendingKey := "trending:global"
	pageKey := fmt.Sprintf("%s:%s", wiki, title)
	
	rank, err := r.client.ZRevRank(ctx, trendingKey, pageKey).Result()
	if err == redis.Nil {
		return 0, nil // Page not in trending list
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get page rank: %w", err)
	}
	
	return int(rank) + 1, nil // Convert to 1-based rank
}

// IsTopTrending checks if a page is in the top N trending pages
func (r *RedisTrending) IsTopTrending(ctx context.Context, wiki, title string, topN int) (bool, error) {
	rank, err := r.GetPageRank(ctx, wiki, title)
	if err != nil {
		return false, err
	}
	
	return rank > 0 && rank <= topN, nil
}

// PruneTrending removes old entries and applies decay to all scores
func (r *RedisTrending) PruneTrending(ctx context.Context) error {
	if !r.config.Enabled {
		return nil
	}
	
	trendingKey := "trending:global"
	now := float64(time.Now().Unix())
	
	// Get all pages with scores
	results, err := r.client.ZRangeWithScores(ctx, trendingKey, 0, -1).Result()
	if err != nil {
		return fmt.Errorf("failed to get all trending pages: %w", err)
	}
	
	// Process each page: apply decay and remove if score too low
	for _, result := range results {
		pageKey := result.Member.(string)
		currentScore := result.Score
		
		// Get last update time
		metadataKey := fmt.Sprintf("trending:meta:%s", pageKey)
		lastUpdateStr, err := r.client.HGet(ctx, metadataKey, "last_update").Result()
		if err != nil {
			// If no metadata, remove the page
			r.client.ZRem(ctx, trendingKey, pageKey)
			continue
		}
		
		lastUpdate, err := strconv.ParseFloat(lastUpdateStr, 64)
		if err != nil {
			// Invalid timestamp, remove the page
			r.client.ZRem(ctx, trendingKey, pageKey)
			r.client.Del(ctx, metadataKey)
			continue
		}
		
		// Apply decay
		timeDelta := now - lastUpdate
		decayFactor := math.Exp(-timeDelta * math.Ln2 / r.halfLife)
		newScore := currentScore * decayFactor
		
		// Remove if score is too low (less than 0.1)
		if newScore < 0.1 {
			r.client.ZRem(ctx, trendingKey, pageKey)
			r.client.Del(ctx, metadataKey)
			continue
		}
		
		// Update with decayed score
		r.client.ZAdd(ctx, trendingKey, redis.Z{
			Score:  newScore,
			Member: pageKey,
		})
	}
	
	// Limit to max pages to prevent unbounded growth
	count, err := r.client.ZCard(ctx, trendingKey).Result()
	if err != nil {
		return fmt.Errorf("failed to get trending count: %w", err)
	}
	
	if count > int64(r.config.MaxPages) {
		// Remove lowest scoring pages
		toRemove := count - int64(r.config.MaxPages)
		r.client.ZRemRangeByRank(ctx, trendingKey, 0, toRemove-1)
	}
	
	log.Printf("Pruned trending pages: %d entries processed", len(results))
	return nil
}

// StartPruningScheduler starts a background goroutine for periodic pruning
func (r *RedisTrending) StartPruningScheduler(ctx context.Context) {
	if !r.config.Enabled {
		return
	}
	
	ticker := time.NewTicker(r.config.PruneInterval)
	
	go func() {
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				if err := r.PruneTrending(ctx); err != nil {
					log.Printf("Error during trending pruning: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// getCurrentScore retrieves current score and last update time for a page
func (r *RedisTrending) getCurrentScore(ctx context.Context, pageKey string) (float64, float64, error) {
	trendingKey := "trending:global"
	
	// Get current score
	score, err := r.client.ZScore(ctx, trendingKey, pageKey).Result()
	if err == redis.Nil {
		return 0, 0, nil // Page not in trending list
	}
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get current score: %w", err)
	}
	
	// Get last update time
	metadataKey := fmt.Sprintf("trending:meta:%s", pageKey)
	lastUpdateStr, err := r.client.HGet(ctx, metadataKey, "last_update").Result()
	if err == redis.Nil {
		return score, 0, nil // No metadata yet
	}
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get last update: %w", err)
	}
	
	lastUpdate, err := strconv.ParseFloat(lastUpdateStr, 64)
	if err != nil {
		return score, 0, nil // Invalid timestamp
	}
	
	return score, lastUpdate, nil
}

// calculateScoreIncrement determines score increment based on edit characteristics
func (r *RedisTrending) calculateScoreIncrement(edit *models.WikipediaEdit) float64 {
	baseScore := 1.0
	
	// Larger edits get higher scores
	byteChange := edit.ByteChange()
	if byteChange < 0 {
		byteChange = -byteChange // Use absolute value
	}
	
	// Scale based on byte change (logarithmic scaling)
	if byteChange > 100 {
		sizeMultiplier := 1.0 + math.Log10(float64(byteChange)/100.0)
		baseScore *= sizeMultiplier
	}
	
	// Bot edits get reduced score
	if edit.Bot {
		baseScore *= 0.5
	}
	
	// New pages get bonus score (detected by looking for "new" in type)
	if edit.Type == "new" {
		baseScore *= 1.5
	}
	
	return baseScore
}

// TrendingPage represents a trending page with metadata
type TrendingPage struct {
	Rank       int       `json:"rank"`
	Wiki       string    `json:"wiki"`
	Title      string    `json:"title"`
	Score      float64   `json:"score"`
	EditCount  int       `json:"edit_count"`
	LastUpdate time.Time `json:"last_update"`
}