package api

import (
	"context"
	"sync"

	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/rs/zerolog"
)

// AlertHub manages a single Redis alert subscription and fans out alerts
// to all connected WebSocket clients.  This avoids the N-blocking-XRead
// problem where each WS client holds its own Redis connection.
type AlertHub struct {
	mu          sync.RWMutex
	subscribers map[chan storage.Alert]struct{}
	logger      zerolog.Logger
	alerts      *storage.RedisAlerts
	cancel      context.CancelFunc
}

// NewAlertHub creates an AlertHub.  Call Run() to start the shared
// subscription goroutine.
func NewAlertHub(alerts *storage.RedisAlerts, logger zerolog.Logger) *AlertHub {
	return &AlertHub{
		subscribers: make(map[chan storage.Alert]struct{}),
		logger:      logger.With().Str("component", "alert-hub").Logger(),
		alerts:      alerts,
	}
}

// Run starts the single Redis subscription loop.  It should be launched
// as a goroutine: go hub.Run()
func (h *AlertHub) Run() {
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel

	if h.alerts == nil {
		h.logger.Warn().Msg("Alerts storage not configured; alert hub will idle")
		<-ctx.Done()
		return
	}

	h.logger.Info().Msg("Alert hub started — single shared subscription")

	for {
		err := h.alerts.SubscribeToAlerts(ctx, []string{"spikes", "editwars"}, func(alert storage.Alert) error {
			h.broadcast(alert)
			return nil
		})

		if ctx.Err() != nil {
			return // shutdown
		}

		h.logger.Warn().Err(err).Msg("Alert subscription ended, reconnecting...")
	}
}

// Stop terminates the subscription loop.
func (h *AlertHub) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
}

// Subscribe returns a channel that receives alerts.  The caller MUST call
// Unsubscribe when done to avoid leaks.
func (h *AlertHub) Subscribe() chan storage.Alert {
	ch := make(chan storage.Alert, 64)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
	h.logger.Debug().Int("total", len(h.subscribers)).Msg("Alert subscriber added")
	return ch
}

// Unsubscribe removes a subscriber channel.  It is safe to call from any
// goroutine.  After this call the channel must not be read from.
func (h *AlertHub) Unsubscribe(ch chan storage.Alert) {
	h.mu.Lock()
	delete(h.subscribers, ch)
	close(ch)
	h.mu.Unlock()
	h.logger.Debug().Int("total", len(h.subscribers)).Msg("Alert subscriber removed")
}

// broadcast sends an alert to all subscribers (non-blocking per subscriber).
func (h *AlertHub) broadcast(alert storage.Alert) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers {
		select {
		case ch <- alert:
		default:
			// Subscriber channel full — drop to avoid blocking the
			// shared subscription goroutine.
		}
	}
}
