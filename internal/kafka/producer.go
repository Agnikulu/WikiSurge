package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/metrics"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/rs/zerolog"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/compress"
)

const (
	DefaultTopic         = "wikipedia.edits"
	DefaultBufferSize    = 1000
	DefaultBatchSize     = 100
	DefaultFlushInterval = 100 * time.Millisecond
	DefaultWriteTimeout  = 10 * time.Second
	DefaultReadTimeout   = 10 * time.Second
)

// Producer handles asynchronous message production to Kafka
type Producer struct {
	writer        *kafka.Writer
	config        *config.Config
	logger        zerolog.Logger
	buffer        chan *models.WikipediaEdit
	batchSize     int
	flushInterval time.Duration
	stopChan      chan struct{}
	wg            sync.WaitGroup
	mu            sync.RWMutex
	isRunning     bool
	droppedCount  int64
}

// NewProducer creates a new Kafka producer instance
func NewProducer(brokers []string, topic string, cfg *config.Config, logger zerolog.Logger) (*Producer, error) {
	if len(brokers) == 0 {
		return nil, fmt.Errorf("no Kafka brokers provided")
	}
	
	if topic == "" {
		topic = DefaultTopic
	}
	
	// Create Kafka writer with optimized configuration
	writer := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.Hash{},                    // Hash balancer on key (page title)
		Compression:  compress.Snappy,                  // Snappy compression for efficiency
		BatchSize:    DefaultBatchSize,                 // 100 messages
		BatchTimeout: DefaultFlushInterval,             // 100ms
		WriteTimeout: DefaultWriteTimeout,              // 10s
		ReadTimeout:  DefaultReadTimeout,               // 10s
		RequiredAcks: kafka.RequireOne,                 // RequireOne for performance
		Async:        false,                            // Synchronous for error handling
		Logger:       kafka.LoggerFunc(logger.Debug().Msgf),
		ErrorLogger:  kafka.LoggerFunc(logger.Error().Msgf),
	}
	
	producer := &Producer{
		writer:        writer,
		config:        cfg,
		logger:        logger.With().Str("component", "kafka-producer").Logger(),
		buffer:        make(chan *models.WikipediaEdit, DefaultBufferSize),
		batchSize:     DefaultBatchSize,
		flushInterval: DefaultFlushInterval,
		stopChan:      make(chan struct{}),
	}
	
	producer.logger.Info().
		Strs("brokers", brokers).
		Str("topic", topic).
		Int("buffer_size", DefaultBufferSize).
		Int("batch_size", DefaultBatchSize).
		Dur("flush_interval", DefaultFlushInterval).
		Msg("Kafka producer created")
	
	return producer, nil
}

// Start begins the producer background goroutine for batching messages
func (p *Producer) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.isRunning {
		return fmt.Errorf("producer is already running")
	}
	
	p.isRunning = true
	p.wg.Add(1)
	
	go p.batchingLoop()
	
	p.logger.Info().Msg("Kafka producer started")
	return nil
}

// batchingLoop is the background goroutine that batches messages and flushes them
func (p *Producer) batchingLoop() {
	defer p.wg.Done()
	defer func() {
		p.mu.Lock()
		p.isRunning = false
		p.mu.Unlock()
	}()
	
	ticker := time.NewTicker(p.flushInterval)
	defer ticker.Stop()
	
	// Pre-allocate batch slice for efficiency
	batch := make([]kafka.Message, 0, p.batchSize)
	
	p.logger.Debug().Msg("Started batching loop")
	
	for {
		select {
		case <-p.stopChan:
			// Flush remaining messages before shutdown
			if len(batch) > 0 {
				if err := p.writeBatch(batch); err != nil {
					p.logger.Error().Err(err).Int("batch_size", len(batch)).Msg("Failed to flush remaining batch during shutdown")
				}
			}
			p.logger.Info().Msg("Batching loop stopped")
			return
			
		case edit := <-p.buffer:
			// Convert edit to Kafka message
			message, err := p.editToKafkaMessage(edit)
			if err != nil {
				p.logger.Error().Err(err).Int64("edit_id", edit.ID).Msg("Failed to convert edit to Kafka message")
				metrics.ProduceErrorsTotal.WithLabelValues("serialization").Inc()
				continue
			}
			
			batch = append(batch, message)
			
			// Flush if batch size reached
			if len(batch) >= p.batchSize {
				if err := p.writeBatch(batch); err != nil {
					p.logger.Error().Err(err).Int("batch_size", len(batch)).Msg("Failed to write batch")
				}
				batch = batch[:0] // Reset slice while keeping capacity
			}
			
		case <-ticker.C:
			// Flush on timer if batch not empty
			if len(batch) > 0 {
				if err := p.writeBatch(batch); err != nil {
					p.logger.Error().Err(err).Int("batch_size", len(batch)).Msg("Failed to write timed batch")
				}
				batch = batch[:0] // Reset slice while keeping capacity
			}
		}
	}
}

// Produce sends an edit to Kafka (non-blocking)
func (p *Producer) Produce(edit *models.WikipediaEdit) error {
	if edit == nil {
		return fmt.Errorf("edit cannot be nil")
	}
	
	// Increment attempts metric
	metrics.ProduceAttemptsTotal.WithLabelValues().Inc()
	
	// Non-blocking send to buffer channel
	select {
	case p.buffer <- edit:
		return nil
	default:
		// Buffer is full, drop message and increment metric
		p.mu.Lock()
		p.droppedCount++
		dropCount := p.droppedCount
		p.mu.Unlock()
		
		metrics.MessagesDroppedTotal.WithLabelValues("buffer_full").Inc()
		
		// Log warning every 100 drops
		if dropCount%100 == 0 {
			p.logger.Warn().
				Int64("total_dropped", dropCount).
				Msg("Buffer full: dropping messages (backpressure signal)")
		}
		
		return fmt.Errorf("producer buffer full, message dropped")
	}
}

// editToKafkaMessage converts a WikipediaEdit to a Kafka message
func (p *Producer) editToKafkaMessage(edit *models.WikipediaEdit) (kafka.Message, error) {
	// Marshal edit to JSON
	value, err := json.Marshal(edit)
	if err != nil {
		return kafka.Message{}, fmt.Errorf("failed to marshal edit to JSON: %w", err)
	}
	
	// Create message with key as page title for partitioning
	message := kafka.Message{
		Key:   []byte(edit.Title),
		Value: value,
		Headers: []kafka.Header{
			{Key: "wiki", Value: []byte(edit.Wiki)},
			{Key: "language", Value: []byte(edit.Language())},
			{Key: "timestamp", Value: []byte(fmt.Sprintf("%d", edit.Timestamp))},
		},
	}
	
	// Add bot header
	if edit.Bot {
		message.Headers = append(message.Headers, kafka.Header{Key: "bot", Value: []byte("true")})
	} else {
		message.Headers = append(message.Headers, kafka.Header{Key: "bot", Value: []byte("false")})
	}
	
	return message, nil
}

// writeBatch writes a batch of messages to Kafka
func (p *Producer) writeBatch(batch []kafka.Message) error {
	if len(batch) == 0 {
		return nil
	}
	
	start := time.Now()
	
	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), DefaultWriteTimeout)
	defer cancel()
	
	// Write messages to Kafka
	err := p.writer.WriteMessages(ctx, batch...)
	
	// Observe latency histogram
	latency := time.Since(start).Seconds()
	metrics.KafkaProduceLatency.WithLabelValues().Observe(latency)
	
	if err != nil {
		// Increment error metric
		metrics.ProduceErrorsTotal.WithLabelValues("write").Inc()
		p.logger.Error().
			Err(err).
			Int("batch_size", len(batch)).
			Dur("latency", time.Since(start)).
			Msg("Failed to write batch to Kafka")
		return fmt.Errorf("failed to write batch to Kafka: %w", err)
	}
	
	// Success - increment produced metric
	metrics.MessagesProducedTotal.WithLabelValues().Add(float64(len(batch)))
	
	p.logger.Debug().
		Int("batch_size", len(batch)).
		Dur("latency", time.Since(start)).
		Msg("Batch written to Kafka successfully")
	
	return nil
}

// Close gracefully shuts down the producer
func (p *Producer) Close() error {
	p.logger.Info().Msg("Shutting down Kafka producer")
	
	// Signal stop to batching loop
	close(p.stopChan)
	
	// Wait for batching goroutine to finish
	p.wg.Wait()
	
	// Close the buffer channel
	close(p.buffer)
	
	// Close the Kafka writer
	if err := p.writer.Close(); err != nil {
		p.logger.Error().Err(err).Msg("Error closing Kafka writer")
		return fmt.Errorf("failed to close Kafka writer: %w", err)
	}
	
	p.logger.Info().
		Int64("total_dropped", p.droppedCount).
		Msg("Kafka producer shutdown complete")
	
	return nil
}

// GetStats returns producer statistics
func (p *Producer) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	return map[string]interface{}{
		"is_running":     p.isRunning,
		"buffer_size":    len(p.buffer),
		"buffer_cap":     cap(p.buffer),
		"dropped_count":  p.droppedCount,
		"batch_size":     p.batchSize,
		"flush_interval": p.flushInterval.String(),
	}
}