package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/rs/zerolog"
	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock handler
// ---------------------------------------------------------------------------

type mockHandler struct {
	edits []*models.WikipediaEdit
	err   error
}

func (m *mockHandler) ProcessEdit(_ context.Context, edit *models.WikipediaEdit) error {
	m.edits = append(m.edits, edit)
	return m.err
}

// ---------------------------------------------------------------------------
// ConsumerConfig defaults
// ---------------------------------------------------------------------------

func TestConsumerConfig_Defaults(t *testing.T) {
	cfg := ConsumerConfig{
		Brokers: []string{"localhost:9092"},
		GroupID: "test-group",
	}

	// Verify zero-value defaults that NewConsumer would override
	assert.Equal(t, 0, cfg.MinBytes)
	assert.Equal(t, 0, cfg.MaxBytes)
	assert.Equal(t, time.Duration(0), cfg.CommitInterval)
	assert.Equal(t, time.Duration(0), cfg.MaxWait)
	assert.Equal(t, "", cfg.Topic)
}

// ---------------------------------------------------------------------------
// processMessage tests — unit test the JSON→handler pipeline
// ---------------------------------------------------------------------------

func newTestConsumer(handler MessageHandler) *Consumer {
	ctx, cancel := context.WithCancel(context.Background())
	return &Consumer{
		handler:  handler,
		metrics:  getSharedConsumerMetrics(),
		logger:   zerolog.Nop(),
		stopChan: make(chan struct{}),
		ctx:      ctx,
		cancel:   cancel,
	}
}

func TestProcessMessage_ValidEdit(t *testing.T) {
	h := &mockHandler{}
	c := newTestConsumer(h)
	defer c.cancel()

	edit := models.WikipediaEdit{
		ID:    12345,
		Title: "TestArticle",
		Wiki:  "enwiki",
		User:  "TestUser",
	}
	data, _ := json.Marshal(edit)

	err := c.processMessage(kafka.Message{Value: data})
	require.NoError(t, err)
	require.Len(t, h.edits, 1)
	assert.Equal(t, "TestArticle", h.edits[0].Title)
	assert.Equal(t, int64(12345), h.edits[0].ID)
}

func TestProcessMessage_InvalidJSON(t *testing.T) {
	h := &mockHandler{}
	c := newTestConsumer(h)
	defer c.cancel()

	err := c.processMessage(kafka.Message{Value: []byte("not-json")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
	assert.Empty(t, h.edits)
}

func TestProcessMessage_HandlerError(t *testing.T) {
	h := &mockHandler{err: errors.New("processing failure")}
	c := newTestConsumer(h)
	defer c.cancel()

	edit := models.WikipediaEdit{Title: "Page"}
	data, _ := json.Marshal(edit)

	err := c.processMessage(kafka.Message{Value: data})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "handler failed")
}

func TestProcessMessage_EmptyValue(t *testing.T) {
	h := &mockHandler{}
	c := newTestConsumer(h)
	defer c.cancel()

	err := c.processMessage(kafka.Message{Value: []byte{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

// ---------------------------------------------------------------------------
// DLQMessage serialisation
// ---------------------------------------------------------------------------

func TestDLQMessage_MarshalUnmarshal(t *testing.T) {
	orig := DLQMessage{
		OriginalTopic:     "wikipedia.edits",
		OriginalPartition: 3,
		OriginalOffset:    42,
		OriginalKey:       "key-1",
		OriginalValue:     `{"title":"FooBar"}`,
		Error:             "unmarshal error",
		Timestamp:         time.Now().UTC().Format(time.RFC3339),
		ConsumerGroup:     "cg-main",
	}

	data, err := json.Marshal(orig)
	require.NoError(t, err)

	var decoded DLQMessage
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, orig, decoded)
}

func TestDLQMessage_Fields(t *testing.T) {
	msg := DLQMessage{
		OriginalTopic:     "t",
		OriginalPartition: 1,
		OriginalOffset:    99,
		OriginalKey:       "k",
		OriginalValue:     "v",
		Error:             "e",
		ConsumerGroup:     "g",
	}

	data, _ := json.Marshal(msg)
	var m map[string]interface{}
	json.Unmarshal(data, &m)

	expectedFields := []string{
		"original_topic", "original_partition", "original_offset",
		"original_key", "original_value", "error", "timestamp",
		"consumer_group",
	}
	for _, f := range expectedFields {
		_, ok := m[f]
		assert.True(t, ok, "missing field: %s", f)
	}
}

// ---------------------------------------------------------------------------
// DefaultDLQTopic constant
// ---------------------------------------------------------------------------

func TestDefaultDLQTopic(t *testing.T) {
	assert.Equal(t, "wikipedia.edits.dlq", DefaultDLQTopic)
}

// ---------------------------------------------------------------------------
// DeadLetterProducer.Count
// ---------------------------------------------------------------------------

func TestDeadLetterProducer_Count(t *testing.T) {
	dlq := &DeadLetterProducer{
		logger: zerolog.Nop(),
	}
	assert.Equal(t, int64(0), dlq.Count())
	dlq.count.Add(5)
	assert.Equal(t, int64(5), dlq.Count())
}

// ---------------------------------------------------------------------------
// NewDeadLetterProducer — validation
// ---------------------------------------------------------------------------

func TestNewDeadLetterProducer_NoBrokers(t *testing.T) {
	_, err := NewDeadLetterProducer(nil, zerolog.Nop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Kafka brokers")
}

// ---------------------------------------------------------------------------
// NewConsumer defaults
// ---------------------------------------------------------------------------

func TestNewConsumer_DefaultTopic(t *testing.T) {
	cfg := &config.Config{
		Kafka: config.Kafka{
			Brokers: []string{"localhost:9092"},
		},
	}
	h := &mockHandler{}
	c, err := NewConsumer(cfg, ConsumerConfig{
		Brokers: []string{"localhost:9092"},
		GroupID: "test",
	}, h, zerolog.Nop())
	require.NoError(t, err)
	defer c.reader.Close()

	readerCfg := c.reader.Config()
	assert.Equal(t, "wikipedia.edits", readerCfg.Topic)
}

func TestNewConsumer_CustomTopic(t *testing.T) {
	cfg := &config.Config{
		Kafka: config.Kafka{
			Brokers: []string{"localhost:9092"},
		},
	}
	h := &mockHandler{}
	c, err := NewConsumer(cfg, ConsumerConfig{
		Brokers: []string{"localhost:9092"},
		GroupID: "test",
		Topic:   "custom.topic",
	}, h, zerolog.Nop())
	require.NoError(t, err)
	defer c.reader.Close()

	assert.Equal(t, "custom.topic", c.reader.Config().Topic)
}

func TestNewConsumer_DefaultsApplied(t *testing.T) {
	cfg := &config.Config{}
	h := &mockHandler{}
	c, err := NewConsumer(cfg, ConsumerConfig{
		Brokers: []string{"localhost:9092"},
		GroupID: "test",
	}, h, zerolog.Nop())
	require.NoError(t, err)
	defer c.reader.Close()

	rcfg := c.reader.Config()
	assert.Equal(t, 1024, rcfg.MinBytes)
	assert.Equal(t, 10*1024*1024, rcfg.MaxBytes)
}

// ---------------------------------------------------------------------------
// PoisonMessageHandler.checkDLQRate (via counter)
// ---------------------------------------------------------------------------

func TestDeadLetterProducer_CountAtomic(t *testing.T) {
	dlq := &DeadLetterProducer{
		logger: zerolog.Nop(),
	}
	for i := 0; i < 100; i++ {
		dlq.count.Add(1)
	}
	assert.Equal(t, int64(100), dlq.Count())
}

// ---------------------------------------------------------------------------
// MessageHandler interface satisfaction
// ---------------------------------------------------------------------------

func TestMockHandlerSatisfiesInterface(t *testing.T) {
	var h MessageHandler = &mockHandler{}
	assert.NotNil(t, h)
}
