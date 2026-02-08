package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// /ws/alerts — stream spike and edit-war alerts in real time
// ---------------------------------------------------------------------------

// WebSocketAlerts upgrades the connection and streams alerts from Redis.
//
// Route: WS /ws/alerts
//
// Unlike the feed endpoint this is simpler: no per-client filtering is needed.
// It subscribes to Redis streams (alerts:spikes, alerts:editwars) and
// forwards every alert as a JSON WebSocket message.
func (s *APIServer) WebSocketAlerts(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error().Err(err).Msg("WebSocket alert upgrade failed")
		return
	}

	s.logger.Info().Str("remote", r.RemoteAddr).Msg("Alerts WebSocket client connected")

	// Use a context tied to the connection lifetime.
	ctx, cancel := context.WithCancel(r.Context())

	// readPump — detect client disconnect.
	go func() {
		defer cancel()
		conn.SetReadLimit(maxMessageSize)
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error {
			_ = conn.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}()

	// Ping ticker to keep the connection alive.
	pingTicker := time.NewTicker(pingPeriod)

	// alertCh receives alerts from the Redis subscription goroutine.
	alertCh := make(chan storage.Alert, 64)
	subDone := make(chan struct{})

	// Subscribe to Redis alert streams in a background goroutine.
	go func() {
		defer close(subDone)
		if s.alerts == nil {
			s.logger.Warn().Msg("Alerts storage not configured; alerts WebSocket will idle")
			<-ctx.Done()
			return
		}
		// SubscribeToAlerts blocks until ctx is cancelled.
		_ = s.alerts.SubscribeToAlerts(ctx, []string{"spikes", "editwars"}, func(alert storage.Alert) error {
			select {
			case alertCh <- alert:
			default:
				s.logger.Warn().Msg("Alert channel full, dropping alert for WS client")
			}
			return nil
		})
	}()

	// Main write loop.
	go func() {
		defer func() {
			pingTicker.Stop()
			cancel()
			conn.Close()
		}()

		for {
			select {
			case alert := <-alertCh:
				msg := WSMessage{
					Type: alert.Type,
					Data: alert,
				}
				payload, err := json.Marshal(msg)
				if err != nil {
					s.logger.Error().Err(err).Msg("Failed to marshal alert")
					continue
				}
				_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
					s.logger.Debug().Err(err).Msg("Alert WS write error")
					return
				}

			case <-pingTicker.C:
				_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}

			case <-ctx.Done():
				return
			}
		}
	}()
}
