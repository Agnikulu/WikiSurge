package storage

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
)

// RedisHotPages manages tracking of hot pages in Redis
type RedisHotPages struct {
	client *redis.Client
	config *config.HotPages
}

// NewRedisHotPages creates a new Redis hot pages tracker
func NewRedisHotPages(client *redis.Client, cfg *config.HotPages) *RedisHotPages {
	return &RedisHotPages{
		client: client,
		config: cfg,
	}
}

// TrackEdit processes an edit and updates hot page tracking
func (r *RedisHotPages) TrackEdit(ctx context.Context, edit *models.WikipediaEdit) error {
	pageKey := fmt.Sprintf("page:%s:%s", edit.Wiki, edit.Title)
	hotPagesKey := "hot:pages"
	
	// Increment page edit count
	count, err := r.client.Incr(ctx, pageKey).Result()
	if err != nil {
		return fmt.Errorf("failed to increment page count: %w", err)
	}

	// Set TTL for page tracking based on window duration
	if count == 1 {
		r.client.Expire(ctx, pageKey, r.config.WindowDuration)
	}

	// Check if page should be promoted to hot pages
	if count >= int64(r.config.PromotionThreshold) {
		score := float64(time.Now().Unix())
		
		// Add to hot pages sorted set
		err = r.client.ZAdd(ctx, hotPagesKey, redis.Z{
			Score:  score,
			Member: fmt.Sprintf("%s:%s", edit.Wiki, edit.Title),
		}).Err()
		
		if err != nil {
			return fmt.Errorf("failed to add to hot pages: %w", err)
		}

		// Track recent editors for this hot page
		editorsKey := fmt.Sprintf("editors:%s:%s", edit.Wiki, edit.Title)
		r.client.ZAdd(ctx, editorsKey, redis.Z{
			Score:  score,
			Member: edit.User,
		})
		
		// Limit number of editors tracked per page
		r.client.ZRemRangeByRank(ctx, editorsKey, 0, 
			int64(-r.config.MaxMembersPerPage-1))
		
		// Set TTL for editors tracking
		r.client.Expire(ctx, editorsKey, r.config.WindowDuration)

		log.Printf("Page promoted to hot: %s:%s (count: %d)", 
			edit.Wiki, edit.Title, count)
	}

	// Maintain hot pages list size
	return r.pruneHotPages(ctx)
}

// GetHotPages returns the current hot pages
func (r *RedisHotPages) GetHotPages(ctx context.Context, limit int) ([]HotPage, error) {
	hotPagesKey := "hot:pages"
	
	// Get hot pages with scores (most recent first)
	results, err := r.client.ZRevRangeWithScores(ctx, hotPagesKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get hot pages: %w", err)
	}

	hotPages := make([]HotPage, 0, len(results))
	for _, result := range results {
		pageName := result.Member.(string)
		lastActivity := time.Unix(int64(result.Score), 0)
		
		// Get current edit count
		pageKey := fmt.Sprintf("page:%s", pageName)
		countStr, err := r.client.Get(ctx, pageKey).Result()
		if err != nil && err != redis.Nil {
			log.Printf("Failed to get count for page %s: %v", pageName, err)
			continue
		}
		
		count := 0
		if countStr != "" {
			count, _ = strconv.Atoi(countStr)
		}

		// Get recent editors count
		editorsKey := fmt.Sprintf("editors:%s", pageName)
		editorsCount, err := r.client.ZCard(ctx, editorsKey).Result()
		if err != nil {
			editorsCount = 0
		}

		hotPages = append(hotPages, HotPage{
			PageName:     pageName,
			EditCount:    count,
			EditorsCount: int(editorsCount),
			LastActivity: lastActivity,
		})
	}

	return hotPages, nil
}

// IsHotPage checks if a page is currently tracked as hot
func (r *RedisHotPages) IsHotPage(ctx context.Context, wiki, title string) (bool, error) {
	hotPagesKey := "hot:pages"
	pageName := fmt.Sprintf("%s:%s", wiki, title)
	
	_, err := r.client.ZScore(ctx, hotPagesKey, pageName).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check hot page status: %w", err)
	}
	
	return true, nil
}

// GetPageEditCount returns the current edit count for a page
func (r *RedisHotPages) GetPageEditCount(ctx context.Context, wiki, title string) (int, error) {
	pageKey := fmt.Sprintf("page:%s:%s", wiki, title)
	
	countStr, err := r.client.Get(ctx, pageKey).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get page edit count: %w", err)
	}
	
	count, err := strconv.Atoi(countStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse edit count: %w", err)
	}
	
	return count, nil
}

// GetPageEditors returns the recent editors for a hot page
func (r *RedisHotPages) GetPageEditors(ctx context.Context, wiki, title string) ([]string, error) {
	editorsKey := fmt.Sprintf("editors:%s:%s", wiki, title)
	
	editors, err := r.client.ZRevRange(ctx, editorsKey, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get page editors: %w", err)
	}
	
	return editors, nil
}

// pruneHotPages maintains the hot pages list size
func (r *RedisHotPages) pruneHotPages(ctx context.Context) error {
	hotPagesKey := "hot:pages"
	
	// Remove oldest entries if we exceed max tracked pages
	count, err := r.client.ZCard(ctx, hotPagesKey).Result()
	if err != nil {
		return fmt.Errorf("failed to get hot pages count: %w", err)
	}
	
	if count > int64(r.config.MaxTracked) {
		// Remove oldest entries
		toRemove := count - int64(r.config.MaxTracked)
		err = r.client.ZRemRangeByRank(ctx, hotPagesKey, 0, toRemove-1).Err()
		if err != nil {
			return fmt.Errorf("failed to prune hot pages: %w", err)
		}
	}
	
	return nil
}

// HotPage represents a hot page with metadata
type HotPage struct {
	PageName     string    `json:"page_name"`
	EditCount    int       `json:"edit_count"`
	EditorsCount int       `json:"editors_count"`
	LastActivity time.Time `json:"last_activity"`
}