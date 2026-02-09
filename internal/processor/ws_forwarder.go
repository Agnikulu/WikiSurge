package processor

import (
	"context"
	"encoding/json"

	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const editsPubSubChannel = "wikisurge:edits:live"

// EditBroadcaster defines the interface for broadcasting edits (e.g. to WebSocket clients).
type EditBroadcaster interface {
	BroadcastEditFiltered(edit *models.WikipediaEdit)
}

// WebSocketForwarder is a Kafka MessageHandler that forwards edits to a WebSocket hub
// and publishes them to Redis pub/sub for cross-process relay.
type WebSocketForwarder struct {
	broadcaster EditBroadcaster
	redis       *redis.Client
	logger      zerolog.Logger
}

// NewWebSocketForwarder creates a forwarder that sends every consumed edit to the hub
// and publishes to Redis pub/sub.
func NewWebSocketForwarder(broadcaster EditBroadcaster, redisClient *redis.Client, logger zerolog.Logger) *WebSocketForwarder {
	return &WebSocketForwarder{
		broadcaster: broadcaster,
		redis:       redisClient,
		logger:      logger.With().Str("component", "websocket-forwarder").Logger(),
	}
}

// ProcessEdit implements kafka.MessageHandler. It broadcasts the edit to all
// matching WebSocket clients and publishes to Redis pub/sub for the API process.
func (f *WebSocketForwarder) ProcessEdit(_ context.Context, edit *models.WikipediaEdit) error {
	// Broadcast to local hub (processor-side).
	f.broadcaster.BroadcastEditFiltered(edit)

	// Publish to Redis pub/sub for the API server to relay to its own WS clients.
	if f.redis != nil {
		data, err := json.Marshal(edit)
		if err == nil {
			f.redis.Publish(context.Background(), editsPubSubChannel, data)
		}
	}

	return nil
}
