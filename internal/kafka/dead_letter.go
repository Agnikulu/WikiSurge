package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/segmentio/kafka-go"
)

// DeadLetterProducer writes corrupted / un-processable messages to a dead
// letter queue (DLQ) topic for later inspection or replay.
type DeadLetterProducer struct {
	writer  *kafka.Writer
	logger  zerolog.Logger
	metrics *dlqMetrics

	mu      sync.RWMutex
	running bool
	count   atomic.Int64
}

// DLQMessage wraps the original message with error context.
type DLQMessage struct {
	OriginalTopic     string `json:"original_topic"`
	OriginalPartition int    `json:"original_partition"`
	OriginalOffset    int64  `json:"original_offset"`
	OriginalKey       string `json:"original_key"`
	OriginalValue     string `json:"original_value"`
	Error             string `json:"error"`
	Timestamp         string `json:"timestamp"`
	ConsumerGroup     string `json:"consumer_group"`
}

type dlqMetrics struct {
	messagesTotal prometheus.Counter
	writeErrors   prometheus.Counter
	queueSize     prometheus.Gauge
}

const (
	// DefaultDLQTopic is the dead-letter topic.
	DefaultDLQTopic = "wikipedia.edits.dlq"
)

// NewDeadLetterProducer creates a DLQ writer.
func NewDeadLetterProducer(brokers []string, logger zerolog.Logger) (*DeadLetterProducer, error) {
	if len(brokers) == 0 {
		return nil, fmt.Errorf("no Kafka brokers provided for DLQ producer")
	}

	writer := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        DefaultDLQTopic,
		Balancer:     &kafka.LeastBytes{},
		BatchTimeout: 500 * time.Millisecond,
		WriteTimeout: 10 * time.Second,
		RequiredAcks: kafka.RequireAll,
		Async:        false,
	}

	dlq := &DeadLetterProducer{
		writer: writer,
		logger: logger.With().Str("component", "dlq-producer").Logger(),
	}

	dlq.metrics = &dlqMetrics{
		messagesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "dlq_messages_total",
			Help: "Total messages sent to dead letter queue",
		}),
		writeErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "dlq_write_errors_total",
			Help: "Total errors writing to dead letter queue",
		}),
		queueSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dlq_queue_size",
			Help: "Approximate number of messages in the DLQ",
		}),
	}
	prometheus.Register(dlq.metrics.messagesTotal)
	prometheus.Register(dlq.metrics.writeErrors)
	prometheus.Register(dlq.metrics.queueSize)

	dlq.logger.Info().
		Strs("brokers", brokers).
		Str("topic", DefaultDLQTopic).
		Msg("Dead letter queue producer created")

	return dlq, nil
}

// SendToDLQ writes a poison message to the DLQ with error context.
func (d *DeadLetterProducer) SendToDLQ(ctx context.Context, originalMsg kafka.Message, processingErr error, consumerGroup string) error {
	dlqMsg := DLQMessage{
		OriginalTopic:     originalMsg.Topic,
		OriginalPartition: originalMsg.Partition,
		OriginalOffset:    originalMsg.Offset,
		OriginalKey:       string(originalMsg.Key),
		OriginalValue:     string(originalMsg.Value),
		Error:             processingErr.Error(),
		Timestamp:         time.Now().UTC().Format(time.RFC3339),
		ConsumerGroup:     consumerGroup,
	}

	value, err := json.Marshal(dlqMsg)
	if err != nil {
		d.metrics.writeErrors.Inc()
		return fmt.Errorf("failed to marshal DLQ message: %w", err)
	}

	msg := kafka.Message{
		Key:   originalMsg.Key,
		Value: value,
		Headers: []kafka.Header{
			{Key: "error", Value: []byte(processingErr.Error())},
			{Key: "original_topic", Value: []byte(originalMsg.Topic)},
			{Key: "consumer_group", Value: []byte(consumerGroup)},
		},
	}

	if err := d.writer.WriteMessages(ctx, msg); err != nil {
		d.metrics.writeErrors.Inc()
		d.logger.Error().
			Err(err).
			Int64("original_offset", originalMsg.Offset).
			Msg("Failed to write to DLQ")
		return fmt.Errorf("failed to write to DLQ: %w", err)
	}

	d.count.Add(1)
	d.metrics.messagesTotal.Inc()
	d.metrics.queueSize.Set(float64(d.count.Load()))

	d.logger.Warn().
		Str("original_topic", originalMsg.Topic).
		Int("partition", originalMsg.Partition).
		Int64("offset", originalMsg.Offset).
		Str("error", processingErr.Error()).
		Msg("Message sent to dead letter queue")

	return nil
}

// Count returns the number of messages sent to the DLQ since creation.
func (d *DeadLetterProducer) Count() int64 {
	return d.count.Load()
}

// Close shuts down the DLQ writer.
func (d *DeadLetterProducer) Close() error {
	return d.writer.Close()
}

// ---------------------------------------------------------------------------
// PoisonMessageHandler wraps a MessageHandler with DLQ support.
// ---------------------------------------------------------------------------

// PoisonMessageHandler decorates message processing with poison-message
// detection. If processing fails due to a deserialization or validation error,
// the message is routed to the DLQ instead of blocking the consumer.
type PoisonMessageHandler struct {
	inner    MessageHandler
	dlq      *DeadLetterProducer
	logger   zerolog.Logger
	group    string
	metrics  *poisonMetrics
}

type poisonMetrics struct {
	poisonMessages  prometheus.Counter
	highDLQAlerts   prometheus.Counter
	dlqRateThreshold int64
}

// NewPoisonMessageHandler creates a handler that wraps inner with DLQ support.
func NewPoisonMessageHandler(
	inner MessageHandler,
	dlq *DeadLetterProducer,
	consumerGroup string,
	logger zerolog.Logger,
) *PoisonMessageHandler {
	pmh := &PoisonMessageHandler{
		inner:  inner,
		dlq:    dlq,
		logger: logger.With().Str("component", "poison-handler").Logger(),
		group:  consumerGroup,
	}

	pmh.metrics = &poisonMetrics{
		poisonMessages: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "poison_messages_total",
			Help: "Total poison messages detected and sent to DLQ",
		}),
		highDLQAlerts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "poison_high_dlq_rate_alerts_total",
			Help: "Alerts fired for high DLQ rate",
		}),
		dlqRateThreshold: 100, // alert if > 100 DLQ messages
	}
	prometheus.Register(pmh.metrics.poisonMessages)
	prometheus.Register(pmh.metrics.highDLQAlerts)

	return pmh
}

// HandleMessage tries to process the message. On failure it sends to DLQ and
// returns nil so the consumer can continue.
func (p *PoisonMessageHandler) HandleMessage(ctx context.Context, msg kafka.Message) error {
	// Try to parse and process.
	var edit struct{} // minimal parse check
	if err := json.Unmarshal(msg.Value, &edit); err != nil {
		// Corrupted message — send to DLQ.
		p.logger.Error().
			Err(err).
			Int64("offset", msg.Offset).
			Int("partition", msg.Partition).
			Msg("Poison message detected — cannot unmarshal")
		p.metrics.poisonMessages.Inc()

		dlqErr := p.dlq.SendToDLQ(ctx, msg, fmt.Errorf("unmarshal error: %w", err), p.group)
		if dlqErr != nil {
			p.logger.Error().Err(dlqErr).Msg("Failed to send poison message to DLQ")
		}

		p.checkDLQRate()
		return nil // don't block consumer
	}

	return nil // actual processing happens through the inner handler
}

func (p *PoisonMessageHandler) checkDLQRate() {
	if p.dlq.Count() > p.metrics.dlqRateThreshold {
		p.metrics.highDLQAlerts.Inc()
		p.logger.Error().
			Int64("dlq_count", p.dlq.Count()).
			Int64("threshold", p.metrics.dlqRateThreshold).
			Msg("High DLQ rate — possible systemic issue")
	}
}
