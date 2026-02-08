package processor

import (
	"context"

	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/rs/zerolog"
)

// EditBroadcaster defines the interface for broadcasting edits (e.g. to WebSocket clients).
type EditBroadcaster interface {
	BroadcastEditFiltered(edit *models.WikipediaEdit)
}

// WebSocketForwarder is a Kafka MessageHandler that forwards edits to a WebSocket hub.
// It implements the kafka.MessageHandler interface so it can be wired as a consumer.
type WebSocketForwarder struct {
	broadcaster EditBroadcaster
	logger      zerolog.Logger
}

// NewWebSocketForwarder creates a forwarder that sends every consumed edit to the hub.
func NewWebSocketForwarder(broadcaster EditBroadcaster, logger zerolog.Logger) *WebSocketForwarder {
	return &WebSocketForwarder{
		broadcaster: broadcaster,
		logger:      logger.With().Str("component", "websocket-forwarder").Logger(),
	}
}

// ProcessEdit implements kafka.MessageHandler. It broadcasts the edit to all
// matching WebSocket clients. This is a non-blocking operation.
func (f *WebSocketForwarder) ProcessEdit(_ context.Context, edit *models.WikipediaEdit) error {
	f.broadcaster.BroadcastEditFiltered(edit)
	return nil
}
