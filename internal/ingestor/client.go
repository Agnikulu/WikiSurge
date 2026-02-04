package ingestor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/metrics"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/r3labs/sse/v2"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

const (
	WikipediaSSEURL = "https://stream.wikimedia.org/v2/stream/recentchange"
	UserAgent       = "WikipediaRTI/1.0"
	ConnectionTimeout = 30 * time.Second
)

// WikiStreamClient handles connection to Wikipedia's SSE stream
type WikiStreamClient struct {
	sseClient       *sse.Client
	config          *config.Config
	logger          zerolog.Logger
	rateLimiter     *rate.Limiter
	stopChan        chan struct{}
	reconnectDelay  time.Duration
	wg              sync.WaitGroup
	mu              sync.RWMutex
	isRunning       bool
	rateLimitHitCount int64
}

// NewWikiStreamClient creates a new Wikipedia SSE client
func NewWikiStreamClient(cfg *config.Config, logger zerolog.Logger) *WikiStreamClient {
	// Create rate limiter with configured limits
	rateLimiter := rate.NewLimiter(rate.Limit(cfg.Ingestor.RateLimit), cfg.Ingestor.BurstLimit)
	
	// Create SSE client
	client := sse.NewClient(WikipediaSSEURL)
	client.Connection.Transport = &http.Transport{
		ResponseHeaderTimeout: ConnectionTimeout,
	}
	client.Headers = map[string]string{
		"Accept":     "text/event-stream",
		"User-Agent": UserAgent,
	}
	
	return &WikiStreamClient{
		sseClient:      client,
		config:         cfg,
		logger:         logger.With().Str("component", "sse-client").Logger(),
		rateLimiter:    rateLimiter,
		stopChan:       make(chan struct{}),
		reconnectDelay: cfg.Ingestor.ReconnectDelay,
	}
}

// Connect establishes the SSE connection to Wikipedia EventStreams
func (w *WikiStreamClient) Connect() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	if w.isRunning {
		return fmt.Errorf("client is already running")
	}
	
	w.logger.Info().Msg("Establishing SSE connection to Wikipedia EventStreams")
	
	// Test connection with a simple HTTP request first
	ctx, cancel := context.WithTimeout(context.Background(), ConnectionTimeout)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "GET", WikipediaSSEURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create test request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", UserAgent)
	
	client := &http.Client{Timeout: ConnectionTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Wikipedia SSE: %w", err)
	}
	resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	
	w.logger.Info().Msg("SSE connection established successfully")
	return nil
}

// Start begins the main event loop for processing SSE messages
func (w *WikiStreamClient) Start() error {
	w.mu.Lock()
	if w.isRunning {
		w.mu.Unlock()
		return fmt.Errorf("client is already running")
	}
	w.isRunning = true
	w.mu.Unlock()
	
	w.logger.Info().Msg("Starting Wikipedia SSE client")
	
	w.wg.Add(1)
	go w.eventLoop()
	
	return nil
}

// eventLoop is the main processing loop that handles SSE events and reconnections
func (w *WikiStreamClient) eventLoop() {
	defer w.wg.Done()
	defer func() {
		w.mu.Lock()
		w.isRunning = false
		w.mu.Unlock()
	}()
	
	for {
		select {
		case <-w.stopChan:
			w.logger.Info().Msg("Stop signal received, shutting down event loop")
			return
		default:
			if err := w.processStream(); err != nil {
				w.logger.Error().Err(err).Msg("Stream processing failed, will reconnect")
				metrics.SSEReconnectionsTotal.WithLabelValues().Inc()
				
				// Wait before reconnecting with exponential backoff
				select {
				case <-w.stopChan:
					w.logger.Info().Msg("Stop signal received during reconnection wait")
					return
				case <-time.After(w.reconnectDelay):
					// Increase reconnect delay with exponential backoff
					w.reconnectDelay *= 2
					if w.reconnectDelay > w.config.Ingestor.MaxReconnectDelay {
						w.reconnectDelay = w.config.Ingestor.MaxReconnectDelay
					}
					w.logger.Info().
						Dur("delay", w.reconnectDelay).
						Msg("Attempting to reconnect")
				}
			} else {
				// Reset reconnect delay on successful connection
				w.reconnectDelay = w.config.Ingestor.ReconnectDelay
			}
		}
	}
}

// processStream handles the actual SSE stream processing
func (w *WikiStreamClient) processStream() error {
	eventChan := make(chan *sse.Event)
	
	// Subscribe to the message event type
	go func() {
		err := w.sseClient.SubscribeChanWithContext(context.Background(), "message", eventChan)
		if err != nil {
			w.logger.Error().Err(err).Msg("Failed to subscribe to SSE stream")
		}
	}()
	
	w.logger.Info().Msg("Started processing SSE events")
	
	for {
		select {
		case <-w.stopChan:
			return nil
		case event, ok := <-eventChan:
			if !ok {
				return fmt.Errorf("SSE event channel closed")
			}
			
			if err := w.processEvent(event); err != nil {
				w.logger.Error().Err(err).Msg("Failed to process event")
				// Continue processing other events even if one fails
			}
		}
	}
}

// processEvent processes a single SSE event
func (w *WikiStreamClient) processEvent(event *sse.Event) error {
	if event == nil || event.Data == nil {
		return nil
	}
	
	// Apply rate limiting
	if err := w.rateLimiter.Wait(context.Background()); err != nil {
		return fmt.Errorf("rate limiter error: %w", err)
	}
	
	// Track rate limiting hits
	w.rateLimitHitCount++
	if w.rateLimitHitCount%100 == 0 {
		metrics.RateLimitHitsTotal.WithLabelValues().Inc()
		w.logger.Warn().
			Int64("hits", w.rateLimitHitCount).
			Msg("Rate limiter hit 100 times")
	}
	
	// Parse JSON data into WikipediaEdit
	var edit models.WikipediaEdit
	if err := json.Unmarshal(event.Data, &edit); err != nil {
		w.logger.Debug().
			Err(err).
			Str("data", string(event.Data)).
			Msg("Failed to parse edit JSON")
		return nil // Skip invalid JSON rather than failing
	}
	
	// Validate edit structure
	if err := edit.Validate(); err != nil {
		w.logger.Debug().
			Err(err).
			Int64("edit_id", edit.ID).
			Msg("Edit validation failed")
		return nil // Skip invalid edits
	}
	
	// Apply filters
	if !w.shouldProcess(&edit) {
		w.logger.Debug().
			Int64("edit_id", edit.ID).
			Str("title", edit.Title).
			Str("user", edit.User).
			Bool("bot", edit.Bot).
			Str("type", edit.Type).
			Str("language", edit.Language()).
			Msg("Edit filtered out")
		return nil
	}
	
	// Increment metrics
	metrics.EditsIngestedTotal.WithLabelValues().Inc()
	
	// Log the edit (placeholder for Kafka sending)
	w.logger.Info().
		Int64("edit_id", edit.ID).
		Str("title", edit.Title).
		Str("user", edit.User).
		Bool("bot", edit.Bot).
		Str("type", edit.Type).
		Str("language", edit.Language()).
		Int("byte_change", edit.ByteChange()).
		Bool("significant", edit.IsSignificant()).
		Msg("Edit processed")
	
	// TODO: Send to Kafka (placeholder for now)
	// This is where we would send edit.ToJSON() to Kafka
	
	return nil
}

// shouldProcess applies filters to determine if an edit should be processed
func (w *WikiStreamClient) shouldProcess(edit *models.WikipediaEdit) bool {
	// Filter by bot status
	if w.config.Ingestor.ExcludeBots && edit.Bot {
		metrics.EditsFilteredTotal.WithLabelValues("bot").Inc()
		return false
	}
	
	// Filter by language
	if len(w.config.Ingestor.AllowedLanguages) > 0 {
		editLang := edit.Language()
		allowed := false
		for _, lang := range w.config.Ingestor.AllowedLanguages {
			if editLang == lang {
				allowed = true
				break
			}
		}
		if !allowed {
			metrics.EditsFilteredTotal.WithLabelValues("language").Inc()
			return false
		}
	}
	
	// Filter by edit type
	if edit.Type != "edit" && edit.Type != "new" {
		metrics.EditsFilteredTotal.WithLabelValues("type").Inc()
		return false
	}
	
	return true
}

// Stop gracefully shuts down the SSE client
func (w *WikiStreamClient) Stop() {
	w.logger.Info().Msg("Stopping Wikipedia SSE client")
	
	// Signal stop to event loop
	close(w.stopChan)
	
	// Close SSE client
	if w.sseClient != nil {
		w.sseClient.Connection.Transport.(*http.Transport).CloseIdleConnections()
	}
	
	// Wait for goroutine to complete
	w.wg.Wait()
	
	w.logger.Info().Msg("Wikipedia SSE client stopped successfully")
}