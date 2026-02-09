package storage

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// StatsTracker tracks real-time edit statistics in Redis for dashboard display.
type StatsTracker struct {
	redis *redis.Client
}

// LanguageCount represents edits per language.
type LanguageCount struct {
	Language string
	Count    int64
}

// TimelinePoint represents edit count in a time bucket.
type TimelinePoint struct {
	Timestamp int64 `json:"timestamp"`
	Count     int64 `json:"count"`
}

// NewStatsTracker creates a new stats tracker.
func NewStatsTracker(client *redis.Client) *StatsTracker {
	return &StatsTracker{redis: client}
}

// RecordEdit records an edit's language and timestamp for aggregate stats.
// Called by the processor for every edit that passes through.
func (st *StatsTracker) RecordEdit(ctx context.Context, language string, isBot bool) error {
	pipe := st.redis.Pipeline()

	// Increment per-language counter (expires daily)
	langKey := "stats:languages"
	pipe.HIncrBy(ctx, langKey, language, 1)
	pipe.Expire(ctx, langKey, 25*time.Hour)

	// Increment total counter
	pipe.HIncrBy(ctx, langKey, "__total__", 1)

	// Increment human/bot counter
	botKey := "stats:edit_types"
	if isBot {
		pipe.HIncrBy(ctx, botKey, "bot", 1)
	} else {
		pipe.HIncrBy(ctx, botKey, "human", 1)
	}
	pipe.Expire(ctx, botKey, 25*time.Hour)

	// Increment minute-bucket counter for timeline
	minuteBucket := time.Now().Truncate(time.Minute).Unix()
	timelineKey := fmt.Sprintf("stats:timeline:%d", minuteBucket)
	pipe.Incr(ctx, timelineKey)
	pipe.Expire(ctx, timelineKey, 25*time.Hour)

	// Track which minute buckets exist (for retrieval)
	pipe.ZAdd(ctx, "stats:timeline:index", redis.Z{
		Score:  float64(minuteBucket),
		Member: strconv.FormatInt(minuteBucket, 10),
	})
	pipe.Expire(ctx, "stats:timeline:index", 25*time.Hour)

	_, err := pipe.Exec(ctx)
	return err
}

// GetLanguageCounts returns edit counts per language, sorted by count descending.
func (st *StatsTracker) GetLanguageCounts(ctx context.Context) ([]LanguageCount, int64, error) {
	data, err := st.redis.HGetAll(ctx, "stats:languages").Result()
	if err != nil {
		return nil, 0, err
	}

	var total int64
	var counts []LanguageCount
	for lang, countStr := range data {
		count, _ := strconv.ParseInt(countStr, 10, 64)
		if lang == "__total__" {
			total = count
			continue
		}
		counts = append(counts, LanguageCount{Language: lang, Count: count})
	}

	sort.Slice(counts, func(i, j int) bool {
		return counts[i].Count > counts[j].Count
	})

	return counts, total, nil
}

// GetEditTypes returns human vs bot edit counts.
func (st *StatsTracker) GetEditTypes(ctx context.Context) (human, bot int64, err error) {
	data, err := st.redis.HGetAll(ctx, "stats:edit_types").Result()
	if err != nil {
		return 0, 0, err
	}
	human, _ = strconv.ParseInt(data["human"], 10, 64)
	bot, _ = strconv.ParseInt(data["bot"], 10, 64)
	return human, bot, nil
}

// GetDailyEditCount returns the total edit count for today.
func (st *StatsTracker) GetDailyEditCount(ctx context.Context) (int64, error) {
	totalStr, err := st.redis.HGet(ctx, "stats:languages", "__total__").Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	total, _ := strconv.ParseInt(totalStr, 10, 64)
	return total, nil
}

// GetTimeline returns edit counts per minute for the given duration.
func (st *StatsTracker) GetTimeline(ctx context.Context, duration time.Duration) ([]TimelinePoint, error) {
	now := time.Now().Truncate(time.Minute).Unix()
	from := time.Now().Add(-duration).Truncate(time.Minute).Unix()

	// Get minute buckets in range
	members, err := st.redis.ZRangeByScore(ctx, "stats:timeline:index", &redis.ZRangeBy{
		Min: strconv.FormatInt(from, 10),
		Max: strconv.FormatInt(now, 10),
	}).Result()
	if err != nil {
		return nil, err
	}

	if len(members) == 0 {
		return []TimelinePoint{}, nil
	}

	// Build pipeline to get all counts
	pipe := st.redis.Pipeline()
	cmds := make([]*redis.StringCmd, len(members))
	for i, m := range members {
		key := fmt.Sprintf("stats:timeline:%s", m)
		cmds[i] = pipe.Get(ctx, key)
	}
	_, _ = pipe.Exec(ctx) // some keys may have expired

	points := make([]TimelinePoint, 0, len(members))
	for i, m := range members {
		ts, _ := strconv.ParseInt(m, 10, 64)
		count, err := cmds[i].Int64()
		if err != nil {
			count = 0
		}
		points = append(points, TimelinePoint{Timestamp: ts, Count: count})
	}

	return points, nil
}
