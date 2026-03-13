package api

import (
	"context"
	"sync"
	"time"

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
	cancelMu    sync.Mutex
	stopOnce    sync.Once
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
	h.runWithRestart(0)
}

// maxPanicRestarts caps how many times Run() will auto-restart after a panic
// to prevent unbounded goroutine spawning on persistent failures.
const maxPanicRestarts = 5

func (h *AlertHub) runWithRestart(restartCount int) {
	defer func() {
		if r := recover(); r != nil {
			if restartCount >= maxPanicRestarts {
				h.logger.Error().Interface("panic", r).Int("restarts", restartCount).Msg("Alert hub panic limit reached — not restarting")
				return
			}
			h.logger.Error().Interface("panic", r).Int("restart", restartCount+1).Msg("Alert hub recovered from panic — restarting")
			time.Sleep(time.Duration(restartCount+1) * time.Second)
			go h.runWithRestart(restartCount + 1)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	h.cancelMu.Lock()
	h.cancel = cancel
	h.cancelMu.Unlock()

	if h.alerts == nil {
		h.logger.Warn().Msg("Alerts storage not configured; alert hub will idle")
		<-ctx.Done()
		return
	}

	h.logger.Info().Msg("Alert hub started — single shared subscription")

	backoff := time.Second
	for {
		err := h.alerts.SubscribeToAlerts(ctx, []string{"spikes", "editwars"}, func(alert storage.Alert) error {
			h.broadcast(alert)
			return nil
		})

		if ctx.Err() != nil {
			return // shutdown
		}

		h.logger.Warn().Err(err).Dur("backoff", backoff).Msg("Alert subscription ended, reconnecting after backoff...")

		// Exponential backoff with cap at 30s.
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// Stop terminates the subscription loop.
func (h *AlertHub) Stop() {
	h.stopOnce.Do(func() {
		h.cancelMu.Lock()
		cancel := h.cancel
		h.cancelMu.Unlock()
		if cancel != nil {
			cancel()
		}
	})
}

// Subscribe returns a channel that receives alerts.  The caller MUST call
// Unsubscribe when done to avoid leaks.
func (h *AlertHub) Subscribe() chan storage.Alert {
	ch := make(chan storage.Alert, 128) // Increased buffer from 64 to 128
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	total := len(h.subscribers)
	h.mu.Unlock()
	h.logger.Debug().Int("total", total).Msg("Alert subscriber added")
	return ch
}

// Unsubscribe removes a subscriber channel.  It is safe to call from any
// goroutine.  After this call the channel must not be read from.
func (h *AlertHub) Unsubscribe(ch chan storage.Alert) {
	h.mu.Lock()
	delete(h.subscribers, ch)
	close(ch)
	total := len(h.subscribers)
	h.mu.Unlock()
	h.logger.Debug().Int("total", total).Msg("Alert subscriber removed")
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
