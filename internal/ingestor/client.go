package ingestor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/kafka"
	"github.com/Agnikulu/WikiSurge/internal/metrics"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/r3labs/sse/v2"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
	"gopkg.in/cenkalti/backoff.v1"
)

const (
	WikipediaSSEURL = "https://stream.wikimedia.org/v2/stream/recentchange"
	UserAgent       = "WikipediaRTI/1.0"
	ConnectionTimeout = 30 * time.Second
)

// WikiStreamClient handles connection to Wikipedia's SSE stream
type WikiStreamClient struct {
	sseClient         *sse.Client
	config            *config.Config
	logger            zerolog.Logger
	rateLimiter       *rate.Limiter
	producer          kafka.ProducerInterface
	stopChan          chan struct{}
	reconnectDelay    time.Duration
	wg                sync.WaitGroup
	mu                sync.RWMutex
	isRunning         bool
	rateLimitHitCount int64
	lastEventID       []byte // tracks the last SSE event ID for gap-free reconnects
}

// NewWikiStreamClient creates a new Wikipedia SSE client
func NewWikiStreamClient(cfg *config.Config, logger zerolog.Logger, producer kafka.ProducerInterface) *WikiStreamClient {
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
		producer:       producer,
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

				errMsg := err.Error()

				// Determine if the disconnect is benign (not a real failure):
				//  - idle timeout: stream was delivering data then stalled
				//  - CANCEL: Wikipedia's normal HTTP/2 stream reset (~every 4-5 min)
				//  - 503: Wikimedia temporary maintenance / overload
				// In these cases, reset backoff — don't penalize normal behavior.
				isBenign := strings.Contains(errMsg, "idle for") ||
					strings.Contains(errMsg, "CANCEL") ||
					strings.Contains(errMsg, "503")
				if isBenign {
					w.reconnectDelay = w.config.Ingestor.ReconnectDelay
				}

				// Wait before reconnecting
				select {
				case <-w.stopChan:
					w.logger.Info().Msg("Stop signal received during reconnection wait")
					return
				case <-time.After(w.reconnectDelay):
					w.logger.Info().
						Dur("delay", w.reconnectDelay).
						Bool("benign", isBenign).
						Msg("Attempting to reconnect")
				}

				// Increase reconnect delay with exponential backoff for
				// genuine failures only (not idle timeouts or CANCEL resets).
				if !isBenign {
					w.reconnectDelay *= 2
					if w.reconnectDelay > w.config.Ingestor.MaxReconnectDelay {
						w.reconnectDelay = w.config.Ingestor.MaxReconnectDelay
					}
				}
			} else {
				// Reset reconnect delay on successful connection
				w.reconnectDelay = w.config.Ingestor.ReconnectDelay
			}
		}
	}
}

// checkConnectivity does a lightweight HTTP HEAD to the SSE endpoint to verify
// network reachability.  This catches DNS failures, firewalls, and HTTP errors
// that the r3labs/sse library would otherwise silently swallow.
func (w *WikiStreamClient) checkConnectivity() error {
	client := &http.Client{Timeout: ConnectionTimeout}
	req, err := http.NewRequest("GET", WikipediaSSEURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		w.logger.Error().Err(err).Str("url", WikipediaSSEURL).Msg("Cannot reach Wikipedia SSE endpoint")
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		w.logger.Error().Int("status", resp.StatusCode).Str("url", WikipediaSSEURL).Msg("Wikipedia SSE endpoint returned error")
		return fmt.Errorf("HTTP %d from SSE endpoint", resp.StatusCode)
	}

	w.logger.Info().Int("status", resp.StatusCode).Msg("Preflight connectivity check passed")
	return nil
}

// processStream handles the actual SSE stream processing
func (w *WikiStreamClient) processStream() error {
	// Preflight connectivity check — the r3labs/sse library swallows HTTP
	// errors, so we probe first to get a clear diagnostic.
	if err := w.checkConnectivity(); err != nil {
		return fmt.Errorf("preflight check failed: %w", err)
	}

	// Grab the last event ID we tracked from the previous stream session.
	// Wikipedia EventStreams uses JSON-array IDs (not timestamps), so
	// we pass them via the Last-Event-ID header (handled by r3labs/sse)
	// rather than the ?since= query parameter.
	w.mu.RLock()
	lastID := w.lastEventID
	w.mu.RUnlock()

	// Create a fresh SSE client for each stream attempt to avoid stale
	// internal state from the r3labs/sse library's own retry logic.
	sseClient := sse.NewClient(WikipediaSSEURL)
	sseClient.Connection.Transport = &http.Transport{
		ResponseHeaderTimeout: ConnectionTimeout,
	}
	sseClient.Headers = map[string]string{
		"Accept":     "text/event-stream",
		"User-Agent": UserAgent,
	}

	// Seed the library's LastEventID so the Last-Event-ID header is sent
	// on the initial request, enabling Wikipedia to replay missed events.
	if len(lastID) > 0 {
		sseClient.LastEventID.Store(lastID)
		w.logger.Info().
			Str("last_event_id", string(lastID)).
			Msg("Resuming stream from last known event ID")
	}

	// Disable the library's internal auto-reconnect so our outer loop
	// controls reconnection with proper backoff and idle detection.
	sseClient.ReconnectStrategy = &backoff.StopBackOff{}

	// Use a cancellable context so we can tear down the SSE subscription
	// when we detect an idle timeout or a stop signal.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// eventChan bridges the callback-based SubscribeWithContext into our
	// select loop.  We use SubscribeWithContext instead of
	// SubscribeChanWithContext because the latter returns immediately
	// with nil error when StopBackOff is set, never delivering events.
	eventChan := make(chan *sse.Event, 64)

	// subDone signals when SubscribeWithContext returns, meaning the
	// underlying HTTP connection was lost or the stream ended.
	subDone := make(chan error, 1)

	go func() {
		err := sseClient.SubscribeWithContext(ctx, "message", func(msg *sse.Event) {
			select {
			case eventChan <- msg:
			case <-ctx.Done():
			}
		})
		subDone <- err
	}()

	// Idle timeout: if no events arrive within this duration, assume the
	// stream is stalled and force a reconnect.
	const idleTimeout = 2 * time.Minute
	idleTimer := time.NewTimer(idleTimeout)
	defer idleTimer.Stop()

	w.logger.Info().Msg("Started processing SSE events")

	for {
		select {
		case <-w.stopChan:
			return nil
		case subErr := <-subDone:
			// The SSE subscription goroutine exited — the stream is dead.
			if subErr != nil {
				return fmt.Errorf("SSE subscription ended: %w", subErr)
			}
			return fmt.Errorf("SSE subscription ended unexpectedly")
		case <-idleTimer.C:
			return fmt.Errorf("SSE stream idle for %v, forcing reconnect", idleTimeout)
		case event := <-eventChan:
			// Reset idle timer on every event
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(idleTimeout)

			// Track the last event ID for gap-free reconnects.
			// Wikipedia EventStreams sends a JSON-array-style ID
			// (e.g. [{"topic":"...","partition":0,"offset":123}]).
			if len(event.ID) > 0 {
				w.mu.Lock()
				w.lastEventID = make([]byte, len(event.ID))
				copy(w.lastEventID, event.ID)
				w.mu.Unlock()
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
	
	// Send edit to Kafka
	if err := w.producer.Produce(&edit); err != nil {
		w.logger.Error().
			Err(err).
			Int64("edit_id", edit.ID).
			Str("title", edit.Title).
			Msg("Failed to send edit to Kafka")
		metrics.ProduceErrorsTotal.WithLabelValues("produce").Inc()
		// Continue processing other events even if one fails
	} else {
		w.logger.Debug().
			Int64("edit_id", edit.ID).
			Str("title", edit.Title).
			Str("user", edit.User).
			Bool("bot", edit.Bot).
			Str("type", edit.Type).
			Str("language", edit.Language()).
			Int("byte_change", edit.ByteChange()).
			Bool("significant", edit.IsSignificant()).
			Msg("Edit sent to Kafka successfully")
	}
	
	return nil
}

// shouldProcess applies filters to determine if an edit should be processed
func (w *WikiStreamClient) shouldProcess(edit *models.WikipediaEdit) bool {
	return w.ShouldProcess(edit)
}

// nonMainNamespacePrefixes are title prefixes that indicate non-main namespace articles.
// These are filtered out as a safeguard even if namespace field is incorrect.
var nonMainNamespacePrefixes = []string{
	"User:", "User talk:",
	"Talk:",
	"Wikipedia:", "Wikipedia talk:",
	"File:", "File talk:",
	"MediaWiki:", "MediaWiki talk:",
	"Template:", "Template talk:",
	"Help:", "Help talk:",
	"Category:", "Category talk:",
	"Portal:", "Portal talk:",
	"Draft:", "Draft talk:",
	"TimedText:", "TimedText talk:",
	"Module:", "Module talk:",
	"Gadget:", "Gadget talk:",
	"Gadget definition:", "Gadget definition talk:",
	"Special:",
	"Media:",
}

// hasNonMainNamespacePrefix checks if the title starts with a non-main namespace prefix.
func hasNonMainNamespacePrefix(title string) bool {
	for _, prefix := range nonMainNamespacePrefixes {
		if len(title) > len(prefix) && title[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// ShouldProcess is a public version for testing
func (w *WikiStreamClient) ShouldProcess(edit *models.WikipediaEdit) bool {
	// Filter by bot status
	if w.config.Ingestor.ExcludeBots && edit.Bot {
		metrics.EditsFilteredTotal.WithLabelValues("bot").Inc()
		return false
	}

	// Only accept edits from actual Wikipedia projects (*.wikipedia.org).
	// This excludes Wikidata, Wiktionary, Commons, Meta, etc.
	if !strings.Contains(edit.ServerURL, "wikipedia.org") {
		metrics.EditsFilteredTotal.WithLabelValues("non_wikipedia").Inc()
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
	
	// Filter by namespace (0=Main articles, 1=Talk, 2=User, etc.)
	if len(w.config.Ingestor.AllowedNamespaces) > 0 {
		allowed := false
		for _, ns := range w.config.Ingestor.AllowedNamespaces {
			if edit.Namespace == ns {
				allowed = true
				break
			}
		}
		if !allowed {
			metrics.EditsFilteredTotal.WithLabelValues("namespace").Inc()
			return false
		}
	}
	
	// Additional safeguard: filter by title prefix even if namespace is 0
	// This catches any edge cases where namespace field might be incorrect
	if hasNonMainNamespacePrefix(edit.Title) {
		metrics.EditsFilteredTotal.WithLabelValues("title_prefix").Inc()
		return false
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
	
	// Close Kafka producer first to flush any remaining messages
	if w.producer != nil {
		if err := w.producer.Close(); err != nil {
			w.logger.Error().Err(err).Msg("Failed to close Kafka producer")
		}
	}
	
	// Close SSE client
	if w.sseClient != nil {
		if transport, ok := w.sseClient.Connection.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}
	
	// Wait for goroutine to complete
	w.wg.Wait()
	
	w.logger.Info().Msg("Wikipedia SSE client stopped successfully")
}