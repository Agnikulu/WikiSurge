package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/Agnikulu/WikiSurge/internal/models"
)

// RedisAlerts manages real-time alert streaming using Redis Streams
type RedisAlerts struct {
	client *redis.Client
}

// NewRedisAlerts creates a new Redis alerts manager
func NewRedisAlerts(client *redis.Client) *RedisAlerts {
	return &RedisAlerts{
		client: client,
	}
}

// Alert represents different types of alerts that can be streamed
type Alert struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// AlertType constants for different alert types
const (
	AlertTypeSpike     = "spike"
	AlertTypeEditWar   = "edit_war"
	AlertTypeTrending  = "trending"
	AlertTypeVandalism = "vandalism"
)

// PublishSpikeAlert publishes an alert when a page experiences a spike in activity
func (r *RedisAlerts) PublishSpikeAlert(ctx context.Context, wiki, title string, spikeRatio float64, editCount int) error {
	alert := Alert{
		ID:        fmt.Sprintf("spike-%d", time.Now().UnixNano()),
		Type:      AlertTypeSpike,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"wiki":        wiki,
			"title":       title,
			"spike_ratio": spikeRatio,
			"edit_count":  editCount,
		},
	}

	return r.publishAlert(ctx, "alerts:spikes", alert)
}

// PublishEditWarAlert publishes an alert when edit war activity is detected
func (r *RedisAlerts) PublishEditWarAlert(ctx context.Context, wiki, title string, participants []string, changeVolume int) error {
	alert := Alert{
		ID:        fmt.Sprintf("editwar-%d", time.Now().UnixNano()),
		Type:      AlertTypeEditWar,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"wiki":           wiki,
			"title":          title,
			"participants":   participants,
			"change_volume":  changeVolume,
			"num_editors":    len(participants),
		},
	}

	return r.publishAlert(ctx, "alerts:editwars", alert)
}

// PublishTrendingAlert publishes an alert when a page enters top trending
func (r *RedisAlerts) PublishTrendingAlert(ctx context.Context, wiki, title string, rank int, score float64) error {
	alert := Alert{
		ID:        fmt.Sprintf("trending-%d", time.Now().UnixNano()),
		Type:      AlertTypeTrending,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"wiki":  wiki,
			"title": title,
			"rank":  rank,
			"score": score,
		},
	}

	return r.publishAlert(ctx, "alerts:trending", alert)
}

// PublishVandalismAlert publishes an alert for potential vandalism
func (r *RedisAlerts) PublishVandalismAlert(ctx context.Context, edit *models.WikipediaEdit, confidence float64, reasons []string) error {
	alert := Alert{
		ID:        fmt.Sprintf("vandalism-%d", time.Now().UnixNano()),
		Type:      AlertTypeVandalism,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"wiki":       edit.Wiki,
			"title":      edit.Title,
			"user":       edit.User,
			"edit_id":    edit.ID,
			"confidence": confidence,
			"reasons":    reasons,
			"comment":    edit.Comment,
			"byte_change": edit.ByteChange(),
		},
	}

	return r.publishAlert(ctx, "alerts:vandalism", alert)
}

// SubscribeToAlerts subscribes to alert streams and calls the provided handler for each alert
func (r *RedisAlerts) SubscribeToAlerts(ctx context.Context, alertTypes []string, handler func(Alert) error) error {
	// go-redis XRead expects Streams as [key1, key2, ..., id1, id2, ...]
	// i.e. all stream names first, then all IDs in the same order.
	names := make([]string, len(alertTypes))
	ids := make([]string, len(alertTypes))
	for i, alertType := range alertTypes {
		names[i] = fmt.Sprintf("alerts:%s", alertType)
		ids[i] = "$" // "$" means read only new messages
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			streams := append(append([]string{}, names...), ids...)

			// Read from streams with blocking
			result, err := r.client.XRead(ctx, &redis.XReadArgs{
				Streams: streams,
				Block:   1 * time.Second,
				Count:   10,
			}).Result()

			if err != nil {
				if err == redis.Nil {
					continue // No new messages, keep waiting
				}
				log.Printf("Error reading from alert streams: %v", err)
				time.Sleep(time.Second)
				continue
			}

			// Process messages
			for _, stream := range result {
				for _, message := range stream.Messages {
					alert, err := r.parseAlertMessage(message)
					if err != nil {
						log.Printf("Failed to parse alert message: %v", err)
						continue
					}

					if err := handler(alert); err != nil {
						log.Printf("Alert handler error: %v", err)
					}

					// Update the ID for this stream so the next XRead picks up from here
					for i, name := range names {
						if name == stream.Stream {
							ids[i] = message.ID
							break
						}
					}
				}
			}
		}
	}
}

// GetRecentAlerts retrieves recent alerts from a specific stream
func (r *RedisAlerts) GetRecentAlerts(ctx context.Context, alertType string, count int64) ([]Alert, error) {
	streamName := fmt.Sprintf("alerts:%s", alertType)
	
	result, err := r.client.XRevRangeN(ctx, streamName, "+", "-", count).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get recent alerts: %w", err)
	}

	alerts := make([]Alert, 0, len(result))
	for _, message := range result {
		alert, err := r.parseAlertMessage(message)
		if err != nil {
			log.Printf("Failed to parse alert message: %v", err)
			continue
		}
		alerts = append(alerts, alert)
	}

	return alerts, nil
}

// CleanupOldAlerts removes old alerts beyond retention period
func (r *RedisAlerts) CleanupOldAlerts(ctx context.Context, alertTypes []string, retentionHours int) error {
	cutoffTime := time.Now().Add(-time.Duration(retentionHours) * time.Hour)
	cutoffMs := cutoffTime.UnixMilli()
	
	for _, alertType := range alertTypes {
		streamName := fmt.Sprintf("alerts:%s", alertType)
		
		// Trim stream to remove old entries
		// Redis streams use millisecond timestamps in IDs
		err := r.client.XTrimMaxLenApprox(ctx, streamName, 10000, 100).Err()
		if err != nil {
			log.Printf("Failed to trim stream %s: %v", streamName, err)
		}

		// Alternative: trim by time (requires Redis 6.2+)
		cutoffID := fmt.Sprintf("%d-0", cutoffMs)
		deleted, err := r.client.XTrimMinID(ctx, streamName, cutoffID).Result()
		if err != nil {
			log.Printf("Failed to trim stream %s by time: %v", streamName, err)
		} else {
			log.Printf("Cleaned up %d old alerts from %s", deleted, streamName)
		}
	}

	return nil
}

// GetAlertStats returns statistics about alert streams
func (r *RedisAlerts) GetAlertStats(ctx context.Context, alertTypes []string) (map[string]AlertStats, error) {
	stats := make(map[string]AlertStats)

	for _, alertType := range alertTypes {
		streamName := fmt.Sprintf("alerts:%s", alertType)
		
		// Get stream info
		info, err := r.client.XInfoStream(ctx, streamName).Result()
		if err != nil {
			if err == redis.Nil {
				// Stream doesn't exist
				stats[alertType] = AlertStats{
					StreamName: streamName,
					Length:     0,
				}
				continue
			}
			return nil, fmt.Errorf("failed to get stream info for %s: %w", streamName, err)
		}

		stats[alertType] = AlertStats{
			StreamName:    streamName,
			Length:        info.Length,
			FirstEntryID:  info.FirstEntry.ID,
			LastEntryID:   info.LastEntry.ID,
			ConsumerGroups: info.Groups,
		}
	}

	return stats, nil
}

// publishAlert publishes an alert to a Redis stream
func (r *RedisAlerts) publishAlert(ctx context.Context, streamName string, alert Alert) error {
	alertJSON, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("failed to marshal alert: %w", err)
	}

	// Add to stream
	_, err = r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamName,
		Values: map[string]interface{}{
			"alert_data": alertJSON,
			"type":       alert.Type,
			"timestamp":  alert.Timestamp.Unix(),
		},
	}).Result()

	if err != nil {
		return fmt.Errorf("failed to add alert to stream: %w", err)
	}

	// Limit stream length to prevent unbounded growth
	r.client.XTrimMaxLenApprox(ctx, streamName, 10000, 100)

	log.Printf("Published %s alert: %s", alert.Type, alert.ID)
	return nil
}

// parseAlertMessage parses a Redis stream message into an Alert.
// The processor stores alerts under the key "data", while publishAlert uses
// "alert_data". We check both for backwards compatibility.
func (r *RedisAlerts) parseAlertMessage(message redis.XMessage) (Alert, error) {
	var alert Alert

	alertDataStr, exists := message.Values["data"].(string)
	if !exists {
		alertDataStr, exists = message.Values["alert_data"].(string)
	}
	if !exists {
		return alert, fmt.Errorf("alert data field missing from message (tried 'data' and 'alert_data')")
	}

	// First unmarshal into a generic map to handle both formats
	var dataMap map[string]interface{}
	err := json.Unmarshal([]byte(alertDataStr), &dataMap)
	if err != nil {
		return alert, fmt.Errorf("failed to unmarshal alert data: %w", err)
	}

	// Extract timestamp
	if tsStr, ok := dataMap["timestamp"].(string); ok {
		ts, _ := time.Parse(time.RFC3339Nano, tsStr)
		alert.Timestamp = ts
	}

	// Determine alert type from severity/page_title pattern (spike detector format)
	// or from explicit type field (alert hub format)
	if alertType, ok := dataMap["type"].(string); ok {
		alert.Type = alertType
	} else if _, hasSpike := dataMap["spike_ratio"]; hasSpike {
		alert.Type = "spike"
	} else if _, hasEditWar := dataMap["revert_count"]; hasEditWar {
		alert.Type = "edit_war"
	}

	// Store all data fields
	alert.Data = dataMap
	alert.ID = message.ID

	return alert, nil
}

// GetAlertsSince retrieves alerts from a stream that are newer than the given timestamp,
// optionally filtered by severity.
func (r *RedisAlerts) GetAlertsSince(ctx context.Context, alertType string, since time.Time, severity string, count int64) ([]Alert, error) {
	streamName := fmt.Sprintf("alerts:%s", alertType)

	// Build the start ID from the since timestamp (milliseconds)
	startID := fmt.Sprintf("%d-0", since.UnixMilli())

	result, err := r.client.XRevRangeN(ctx, streamName, "+", startID, count).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get alerts since %v: %w", since, err)
	}

	alerts := make([]Alert, 0, len(result))
	for _, message := range result {
		alert, err := r.parseAlertMessage(message)
		if err != nil {
			log.Printf("Failed to parse alert message: %v", err)
			continue
		}

		// Filter by severity if specified
		if severity != "" {
			alertSeverity := DeriveSeverity(alert)
			if alertSeverity != severity {
				continue
			}
		}

		alerts = append(alerts, alert)
	}

	return alerts, nil
}

// GetEditWarAlertsSince reads edit war alerts from the alerts:editwars stream.
// Edit war alerts are stored in a different format (field "data" instead of "alert_data").
func (r *RedisAlerts) GetEditWarAlertsSince(ctx context.Context, since time.Time, count int64) ([]map[string]interface{}, error) {
	streamName := "alerts:editwars"
	startID := fmt.Sprintf("%d-0", since.UnixMilli())

	result, err := r.client.XRevRangeN(ctx, streamName, "+", startID, count).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get edit war alerts: %w", err)
	}

	wars := make([]map[string]interface{}, 0, len(result))
	for _, message := range result {
		entry := make(map[string]interface{})

		// Try "data" field first (edit war detector format)
		if dataStr, ok := message.Values["data"].(string); ok {
			var warData map[string]interface{}
			if err := json.Unmarshal([]byte(dataStr), &warData); err == nil {
				entry = warData
				entry["active"] = false
				wars = append(wars, entry)
				continue
			}
		}

		// Try "alert_data" field (general alert format)
		if alertDataStr, ok := message.Values["alert_data"].(string); ok {
			var alert Alert
			if err := json.Unmarshal([]byte(alertDataStr), &alert); err == nil {
				entry["page_title"] = alert.Data["title"]
				entry["severity"] = message.Values["severity"]
				entry["active"] = false
				entry["timestamp"] = alert.Timestamp.Format(time.RFC3339)
				wars = append(wars, entry)
			}
		}
	}

	return wars, nil
}

// GetActiveEditWars scans Redis for marker keys that flag active edit wars,
// then enriches each entry with editor / change data when still available.
// Marker keys ("editwar:<page>") have a 1-hour TTL set by the processor,
// while the editor hashes ("editwar:editors:<page>") have a shorter 10-min TTL
// and may already have expired.
func (r *RedisAlerts) GetActiveEditWars(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	var cursor uint64
	var activeWars []map[string]interface{}
	seen := make(map[string]bool)

	for {
		keys, nextCursor, err := r.client.Scan(ctx, cursor, "editwar:*", 200).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan for active edit wars: %w", err)
		}

		for _, key := range keys {
			if len(activeWars) >= limit {
				break
			}

			// Only consider plain marker keys ("editwar:<page>").
			// Skip sub-keys like editwar:editors:*, editwar:changes:*, editwar:<wiki>:*.
			raw := key[len("editwar:"):]
			if strings.Contains(raw, ":") {
				continue // sub-key (editors:, changes:, enwiki:, etc.)
			}

			pageTitle := raw
			if seen[pageTitle] {
				continue
			}
			seen[pageTitle] = true

			// Verify the key is actually a string marker (value "1")
			keyType, err := r.client.Type(ctx, key).Result()
			if err != nil || keyType != "string" {
				continue
			}

			// Derive approximate start time from the marker key's TTL.
			now := time.Now()
			startTime := now
			markerKey := key
			ttl, ttlErr := r.client.TTL(ctx, markerKey).Result()
			if ttlErr == nil && ttl > 0 {
				elapsed := 3600*time.Second - ttl
				if elapsed > 0 {
					startTime = now.Add(-elapsed)
				}
			}

			// Try to enrich with editor data (may have expired).
			editorsKey := fmt.Sprintf("editwar:editors:%s", pageTitle)
			editorMap, _ := r.client.HGetAll(ctx, editorsKey).Result()

			editors := make([]string, 0, len(editorMap))
			totalEdits := 0
			for editor, countStr := range editorMap {
				editors = append(editors, editor)
				count := 0
				fmt.Sscanf(countStr, "%d", &count)
				totalEdits += count
			}

			// Count reverts from the changes list (may have expired).
			changesKey := fmt.Sprintf("editwar:changes:%s", pageTitle)
			revertCount := 0
			changes, err := r.client.LRange(ctx, changesKey, 0, -1).Result()
			if err == nil {
				revertCount = countRevertPatterns(changes)
			}

			// If editor data expired, skip this edit war (it's no longer active).
			// Editor data has 10-min TTL, so if it's gone, there's been no activity.
			if len(editors) == 0 {
				continue // Skip inactive wars, they'll appear in history
			}

			severity := classifyEditWarSeverity(len(editors), totalEdits, revertCount)

			war := map[string]interface{}{
				"page_title":   pageTitle,
				"editor_count": max(len(editors), 2), // at least 2 editors triggered the war
				"edit_count":   totalEdits,
				"revert_count": revertCount,
				"severity":     severity,
				"editors":      editors,
				"active":       true,
				"start_time":   startTime.UTC().Format(time.RFC3339),
				"last_edit":    now.UTC().Format(time.RFC3339),
			}
			activeWars = append(activeWars, war)
		}

		cursor = nextCursor
		if cursor == 0 || len(activeWars) >= limit {
			break
		}
	}

	if activeWars == nil {
		activeWars = make([]map[string]interface{}, 0)
	}
	return activeWars, nil
}

// DeriveSeverity extracts or computes severity from an Alert.
func DeriveSeverity(alert Alert) string {
	switch alert.Type {
	case AlertTypeSpike:
		ratio, _ := alert.Data["spike_ratio"].(float64)
		switch {
		case ratio >= 10:
			return "critical"
		case ratio >= 5:
			return "high"
		case ratio >= 2:
			return "medium"
		default:
			return "low"
		}
	case AlertTypeEditWar:
		numEditors, _ := alert.Data["num_editors"].(float64)
		switch {
		case numEditors >= 6:
			return "critical"
		case numEditors >= 4:
			return "high"
		default:
			return "medium"
		}
	case AlertTypeVandalism:
		confidence, _ := alert.Data["confidence"].(float64)
		switch {
		case confidence >= 0.9:
			return "critical"
		case confidence >= 0.7:
			return "high"
		case confidence >= 0.5:
			return "medium"
		default:
			return "low"
		}
	default:
		return "low"
	}
}

// countRevertPatterns counts sign-reversal patterns in a list of byte change strings.
func countRevertPatterns(changes []string) int {
	if len(changes) < 2 {
		return 0
	}
	reverts := 0
	for i := 1; i < len(changes); i++ {
		var prev, curr int
		fmt.Sscanf(changes[i-1], "%d", &prev)
		fmt.Sscanf(changes[i], "%d", &curr)
		// A revert is when changes go in opposite directions
		if (prev > 0 && curr < 0) || (prev < 0 && curr > 0) {
			reverts++
		}
	}
	return reverts
}

// classifyEditWarSeverity returns a severity level based on edit war metrics.
func classifyEditWarSeverity(editorCount, editCount, revertCount int) string {
	switch {
	case editorCount >= 5 || revertCount >= 8:
		return "critical"
	case editorCount >= 3 || revertCount >= 4:
		return "high"
	case editorCount >= 2 || revertCount >= 2:
		return "medium"
	default:
		return "low"
	}
}

// AlertStats contains statistics about an alert stream
type AlertStats struct {
	StreamName     string `json:"stream_name"`
	Length         int64  `json:"length"`
	FirstEntryID   string `json:"first_entry_id"`
	LastEntryID    string `json:"last_entry_id"`
	ConsumerGroups int64  `json:"consumer_groups"`
}