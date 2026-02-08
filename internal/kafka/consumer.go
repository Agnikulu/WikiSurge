package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/segmentio/kafka-go"
)

// MessageHandler defines the interface for handling consumed messages
type MessageHandler interface {
	ProcessEdit(ctx context.Context, edit *models.WikipediaEdit) error
}

// Consumer handles Kafka message consumption
type Consumer struct {
	reader   *kafka.Reader
	handler  MessageHandler
	config   *config.Config
	metrics  *ConsumerMetrics
	logger   zerolog.Logger
	stopChan chan struct{}
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

// ConsumerMetrics contains Prometheus metrics for the consumer
type ConsumerMetrics struct {
	MessagesProcessed *prometheus.CounterVec
	ProcessingErrors  prometheus.Counter
	ConsumerLag       prometheus.Gauge
	ProcessingTime    prometheus.Histogram
}

// ConsumerConfig holds consumer-specific configuration
type ConsumerConfig struct {
	Brokers       []string
	Topic         string
	GroupID       string
	MinBytes      int
	MaxBytes      int
	CommitInterval time.Duration
	StartOffset   int64
	MaxWait       time.Duration
}

// NewConsumer creates a new Kafka consumer instance
func NewConsumer(cfg *config.Config, consumerCfg ConsumerConfig, handler MessageHandler, logger zerolog.Logger) (*Consumer, error) {
	// Set defaults if not provided
	if consumerCfg.MinBytes == 0 {
		consumerCfg.MinBytes = 1024 // 1KB
	}
	if consumerCfg.MaxBytes == 0 {
		consumerCfg.MaxBytes = 10 * 1024 * 1024 // 10MB
	}
	if consumerCfg.CommitInterval == 0 {
		consumerCfg.CommitInterval = time.Second
	}
	if consumerCfg.MaxWait == 0 {
		consumerCfg.MaxWait = 500 * time.Millisecond
	}
	if consumerCfg.Topic == "" {
		consumerCfg.Topic = "wikipedia.edits"
	}

	// Create Kafka reader with configuration
	readerConfig := kafka.ReaderConfig{
		Brokers:        consumerCfg.Brokers,
		Topic:          consumerCfg.Topic,
		GroupID:        consumerCfg.GroupID,
		MinBytes:       consumerCfg.MinBytes,
		MaxBytes:       consumerCfg.MaxBytes,
		CommitInterval: consumerCfg.CommitInterval,
		StartOffset:    consumerCfg.StartOffset, // kafka.FirstOffset for earliest, kafka.LastOffset for latest
		MaxWait:        consumerCfg.MaxWait,
		Logger:         kafka.LoggerFunc(func(msg string, args ...interface{}) {
			logger.Debug().Msgf(msg, args...)
		}),
		ErrorLogger: kafka.LoggerFunc(func(msg string, args ...interface{}) {
			logger.Error().Msgf(msg, args...)
		}),
	}

	reader := kafka.NewReader(readerConfig)

	// Initialize metrics
	metrics := &ConsumerMetrics{
		MessagesProcessed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kafka_messages_processed_total",
				Help: "Total number of Kafka messages processed",
			},
			[]string{"consumer_group", "topic", "status"},
		),
		ProcessingErrors: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "kafka_processing_errors_total",
				Help: "Total number of message processing errors",
			},
		),
		ConsumerLag: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "kafka_consumer_lag",
				Help: "Current consumer lag in messages",
			},
		),
		ProcessingTime: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name: "kafka_message_processing_seconds",
				Help: "Time spent processing individual messages",
				Buckets: prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
			},
		),
	}

	// Register metrics
	prometheus.MustRegister(
		metrics.MessagesProcessed,
		metrics.ProcessingErrors,
		metrics.ConsumerLag,
		metrics.ProcessingTime,
	)

	ctx, cancel := context.WithCancel(context.Background())

	return &Consumer{
		reader:   reader,
		handler:  handler,
		config:   cfg,
		metrics:  metrics,
		logger:   logger.With().Str("component", "kafka_consumer").Str("group", consumerCfg.GroupID).Logger(),
		stopChan: make(chan struct{}),
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// Start begins consuming messages from Kafka
func (c *Consumer) Start() error {
	c.logger.Info().Msg("Starting Kafka consumer")
	
	c.wg.Add(1)
	go c.consumeLoop()
	
	return nil
}

// Stop gracefully shuts down the consumer
func (c *Consumer) Stop() error {
	c.logger.Info().Msg("Stopping Kafka consumer")
	
	// Signal stop
	close(c.stopChan)
	c.cancel()
	
	// Wait for consumer goroutine to finish
	c.wg.Wait()
	
	// Close reader
	if err := c.reader.Close(); err != nil {
		c.logger.Error().Err(err).Msg("Error closing Kafka reader")
		return err
	}
	
	c.logger.Info().Msg("Kafka consumer stopped")
	return nil
}

// consumeLoop is the main consumption loop
func (c *Consumer) consumeLoop() {
	defer c.wg.Done()
	
	for {
		select {
		case <-c.stopChan:
			c.logger.Info().Msg("Consumer loop stopping")
			return
		case <-c.ctx.Done():
			c.logger.Info().Msg("Consumer context cancelled")
			return
		default:
			// Fetch message with timeout
			message, err := c.reader.FetchMessage(c.ctx)
			if err != nil {
				if err == context.Canceled || err == context.DeadlineExceeded {
					// Context cancelled or timeout - check if we should stop
					continue
				}
				c.logger.Error().Err(err).Msg("Error fetching message")
				c.metrics.ProcessingErrors.Inc()
				time.Sleep(time.Second) // Avoid tight loop on persistent errors
				continue
			}

			// Process the message
			if err := c.processMessage(message); err != nil {
				c.logger.Error().Err(err).Bytes("message_key", message.Key).Msg("Error processing message")
				c.metrics.ProcessingErrors.Inc()
				c.metrics.MessagesProcessed.WithLabelValues(
					c.reader.Config().GroupID,
					c.reader.Config().Topic,
					"error",
				).Inc()
			} else {
				c.metrics.MessagesProcessed.WithLabelValues(
					c.reader.Config().GroupID,
					c.reader.Config().Topic,
					"success",
				).Inc()
			}

			// Commit the message (whether processing succeeded or failed)
			if err := c.reader.CommitMessages(c.ctx, message); err != nil {
				c.logger.Error().Err(err).Msg("Error committing message")
				// Continue processing even if commit fails
			}

			// Update lag metric periodically
			c.updateLagMetric()
		}
	}
}

// processMessage handles a single Kafka message
func (c *Consumer) processMessage(message kafka.Message) error {
	start := time.Now()
	defer func() {
		c.metrics.ProcessingTime.Observe(time.Since(start).Seconds())
	}()

	// Unmarshal message to WikipediaEdit
	var edit models.WikipediaEdit
	if err := json.Unmarshal(message.Value, &edit); err != nil {
		return fmt.Errorf("failed to unmarshal WikipediaEdit: %w", err)
	}

	// Call handler to process the edit
	if err := c.handler.ProcessEdit(c.ctx, &edit); err != nil {
		return fmt.Errorf("handler failed to process edit: %w", err)
	}

	return nil
}

// updateLagMetric updates the consumer lag metric
func (c *Consumer) updateLagMetric() {
	// Get consumer stats
	stats := c.reader.Stats()
	
	// The stats already contain the current lag
	c.metrics.ConsumerLag.Set(float64(stats.Lag))
}

// GetStats returns consumer statistics
func (c *Consumer) GetStats() kafka.ReaderStats {
	return c.reader.Stats()
}