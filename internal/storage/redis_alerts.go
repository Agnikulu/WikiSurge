package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	streams := make([]string, 0, len(alertTypes)*2)
	
	// Build stream names and starting positions
	for _, alertType := range alertTypes {
		streamName := fmt.Sprintf("alerts:%s", alertType)
		streams = append(streams, streamName, "$") // "$" means read only new messages
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
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

					// Update stream position for next read
					for i := 0; i < len(streams); i += 2 {
						if streams[i] == stream.Stream {
							streams[i+1] = message.ID
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

// parseAlertMessage parses a Redis stream message into an Alert
func (r *RedisAlerts) parseAlertMessage(message redis.XMessage) (Alert, error) {
	var alert Alert

	alertDataStr, exists := message.Values["alert_data"].(string)
	if !exists {
		return alert, fmt.Errorf("alert_data field missing from message")
	}

	err := json.Unmarshal([]byte(alertDataStr), &alert)
	if err != nil {
		return alert, fmt.Errorf("failed to unmarshal alert data: %w", err)
	}

	// Set message ID as fallback if alert ID is missing
	if alert.ID == "" {
		alert.ID = message.ID
	}

	return alert, nil
}

// AlertStats contains statistics about an alert stream
type AlertStats struct {
	StreamName     string `json:"stream_name"`
	Length         int64  `json:"length"`
	FirstEntryID   string `json:"first_entry_id"`
	LastEntryID    string `json:"last_entry_id"`
	ConsumerGroups int64  `json:"consumer_groups"`
}