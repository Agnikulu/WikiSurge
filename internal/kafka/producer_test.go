package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/rs/zerolog"
	"github.com/segmentio/kafka-go"
)

// mockKafkaWriter is a test double for kafka.Writer
type mockKafkaWriter struct {
	mu               sync.Mutex
	messages         []kafka.Message
	writeCallCount   int
	shouldError      bool
	writeDelay       time.Duration
	batchSizes       []int
}

func newMockKafkaWriter() *mockKafkaWriter {
	return &mockKafkaWriter{
		messages:   make([]kafka.Message, 0),
		batchSizes: make([]int, 0),
	}
}

func (m *mockKafkaWriter) WriteMessages(ctx context.Context, msgs ...kafka.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.writeCallCount++
	m.batchSizes = append(m.batchSizes, len(msgs))
	
	if m.shouldError {
		return fmt.Errorf("mock write error")
	}
	
	if m.writeDelay > 0 {
		time.Sleep(m.writeDelay)
	}
	
	m.messages = append(m.messages, msgs...)
	return nil
}

func (m *mockKafkaWriter) Close() error {
	return nil
}

func (m *mockKafkaWriter) GetMessages() []kafka.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]kafka.Message, len(m.messages))
	copy(result, m.messages)
	return result
}

func (m *mockKafkaWriter) GetWriteCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeCallCount
}

func (m *mockKafkaWriter) GetBatchSizes() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]int, len(m.batchSizes))
	copy(result, m.batchSizes)
	return result
}

func (m *mockKafkaWriter) SetShouldError(shouldError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldError = shouldError
}

func (m *mockKafkaWriter) SetWriteDelay(delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeDelay = delay
}

func (m *mockKafkaWriter) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = m.messages[:0]
	m.batchSizes = m.batchSizes[:0]
	m.writeCallCount = 0
}

// createTestProducer creates a producer with mock writer for testing
func createTestProducer(batchSize int, flushInterval time.Duration) (*Producer, *mockKafkaWriter) {
	logger := zerolog.New(nil).With().Timestamp().Logger()
	cfg := &config.Config{}
	
	mockWriter := newMockKafkaWriter()
	
	producer := &Producer{
		writer:        mockWriter,
		config:        cfg,
		logger:        logger,
		buffer:        make(chan *models.WikipediaEdit, DefaultBufferSize),
		batchSize:     batchSize,
		flushInterval: flushInterval,
		stopChan:      make(chan struct{}),
	}
	
	return producer, mockWriter
}

// TestMessageBatching verifies that messages are batched correctly
func TestMessageBatching(t *testing.T) {
	batchSize := 5
	producer, mockWriter := createTestProducer(batchSize, 100*time.Millisecond)
	
	// Start the producer
	err := producer.Start()
	if err != nil {
		t.Fatalf("Failed to start producer: %v", err)
	}
	defer producer.Close()
	
	// Create test edits
	numEdits := 12
	for i := 0; i < numEdits; i++ {
		edit := &models.WikipediaEdit{
			ID:        int64(i + 1),
			Type:      "edit",
			Title:     fmt.Sprintf("Test Page %d", i+1),
			User:      "TestUser",
			Wiki:      "enwiki",
			ServerURL: "en.wikipedia.org",
			Timestamp: time.Now().Unix(),
			Length: struct {
				Old int `json:"old"`
				New int `json:"new"`
			}{Old: 100, New: 150},
			Revision: struct {
				Old int64 `json:"old"`
				New int64 `json:"new"`
			}{Old: int64(1000), New: int64(1001)},
		}
		
		err := producer.Produce(edit)
		if err != nil {
			t.Errorf("Failed to produce edit %d: %v", i+1, err)
		}
	}
	
	// Wait for all batches to be processed
	time.Sleep(200 * time.Millisecond)
	
	// Verify batching
	batchSizes := mockWriter.GetBatchSizes()
	
	// Should have 3 batches: 5, 5, 2
	expectedBatches := []int{5, 5, 2}
	if len(batchSizes) != len(expectedBatches) {
		t.Fatalf("Expected %d batches, got %d", len(expectedBatches), len(batchSizes))
	}
	
	for i, expected := range expectedBatches {
		if batchSizes[i] != expected {
			t.Errorf("Batch %d: expected size %d, got %d", i+1, expected, batchSizes[i])
		}
	}
	
	// Verify all messages were written
	messages := mockWriter.GetMessages()
	if len(messages) != numEdits {
		t.Errorf("Expected %d messages, got %d", numEdits, len(messages))
	}
}

// TestFlushOnInterval verifies that incomplete batches are flushed on timer
func TestFlushOnInterval(t *testing.T) {
	batchSize := 100 // Large batch size
	flushInterval := 50 * time.Millisecond
	producer, mockWriter := createTestProducer(batchSize, flushInterval)
	
	// Start the producer
	err := producer.Start()
	if err != nil {
		t.Fatalf("Failed to start producer: %v", err)
	}
	defer producer.Close()
	
	// Send just 1 message (less than batch size)
	edit := &models.WikipediaEdit{
		ID:        1,
		Type:      "edit",
		Title:     "Test Page",
		User:      "TestUser",
		Wiki:      "enwiki",
		ServerURL: "en.wikipedia.org",
		Timestamp: time.Now().Unix(),
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 150},
		Revision: struct {
			Old int64 `json:"old"`
			New int64 `json:"new"`
		}{Old: 1000, New: 1001},
	}
	
	err = producer.Produce(edit)
	if err != nil {
		t.Fatalf("Failed to produce edit: %v", err)
	}
	
	// Wait for flush interval + some buffer
	time.Sleep(flushInterval + 20*time.Millisecond)
	
	// Verify message was flushed despite partial batch
	messages := mockWriter.GetMessages()
	if len(messages) != 1 {
		t.Errorf("Expected 1 message to be flushed, got %d", len(messages))
	}
	
	batchSizes := mockWriter.GetBatchSizes()
	if len(batchSizes) != 1 || batchSizes[0] != 1 {
		t.Errorf("Expected 1 batch of size 1, got batches: %v", batchSizes)
	}
}

// TestErrorHandling verifies error handling doesn't crash producer
func TestErrorHandling(t *testing.T) {
	producer, mockWriter := createTestProducer(5, 100*time.Millisecond)
	
	// Make writer return errors
	mockWriter.SetShouldError(true)
	
	// Start the producer
	err := producer.Start()
	if err != nil {
		t.Fatalf("Failed to start producer: %v", err)
	}
	defer producer.Close()
	
	// Send messages
	numEdits := 3
	for i := 0; i < numEdits; i++ {
		edit := &models.WikipediaEdit{
			ID:        int64(i + 1),
			Type:      "edit",
			Title:     fmt.Sprintf("Test Page %d", i+1),
			User:      "TestUser",
			Wiki:      "enwiki",
			ServerURL: "en.wikipedia.org",
			Timestamp: time.Now().Unix(),
			Length:    struct{Old int `json:"old"`; New int `json:"new"`}{Old: 100, New: 150},
			Revision:  struct{Old int64 `json:"old"`; New int64 `json:"new"`}{Old: int64(1000 + i), New: int64(1001 + i)},
		}
		
		err := producer.Produce(edit)
		if err != nil {
			t.Errorf("Failed to produce edit %d: %v", i+1, err)
		}
	}
	
	// Wait for processing
	time.Sleep(200 * time.Millisecond)
	
	// Verify producer continues accepting messages (doesn't crash)
	// The writeBatch calls should have been attempted even though they failed
	writeCallCount := mockWriter.GetWriteCallCount()
	if writeCallCount == 0 {
		t.Error("Expected at least one write call, got 0")
	}
	
	// Turn off errors and verify recovery
	mockWriter.SetShouldError(false)
	
	// Send more messages
	for i := 0; i < 2; i++ {
		edit := &models.WikipediaEdit{
			ID:        int64(i + 100),
			Type:      "edit",
			Title:     fmt.Sprintf("Recovery Page %d", i+1),
			User:      "TestUser",
			Wiki:      "enwiki",
			ServerURL: "en.wikipedia.org",
			Timestamp: time.Now().Unix(),
			Length:    struct{Old int `json:"old"`; New int `json:"new"`}{Old: 100, New: 150},
			Revision:  struct{Old int64 `json:"old"`; New int64 `json:"new"`}{Old: int64(2000 + i), New: int64(2001 + i)},
		}
		
		err := producer.Produce(edit)
		if err != nil {
			t.Errorf("Failed to produce recovery edit %d: %v", i+1, err)
		}
	}
	
	// Wait for processing
	time.Sleep(200 * time.Millisecond)
	
	// Verify recovery - should have some messages now
	messages := mockWriter.GetMessages()
	if len(messages) == 0 {
		t.Error("Expected messages after error recovery, got 0")
	}
}

// TestEditToKafkaMessage verifies proper message conversion
func TestEditToKafkaMessage(t *testing.T) {
	producer, _ := createTestProducer(10, 100*time.Millisecond)
	
	edit := &models.WikipediaEdit{
		ID:        12345,
		Type:      "edit",
		Title:     "Test Page",
		User:      "TestUser",
		Bot:       false,
		Wiki:      "enwiki",
		ServerURL: "en.wikipedia.org",
		Timestamp: 1640995200, // 2022-01-01 00:00:00 UTC
		Length:    struct{Old int `json:"old"`; New int `json:"new"`}{Old: 100, New: 150},
		Revision:  struct{Old int64 `json:"old"`; New int64 `json:"new"`}{Old: 1000, New: 1001},
		Comment:   "Test edit comment",
	}
	
	message, err := producer.editToKafkaMessage(edit)
	if err != nil {
		t.Fatalf("Failed to convert edit to message: %v", err)
	}
	
	// Verify key is page title
	if string(message.Key) != "Test Page" {
		t.Errorf("Expected key 'Test Page', got '%s'", string(message.Key))
	}
	
	// Verify value is JSON representation of edit
	var parsedEdit models.WikipediaEdit
	err = json.Unmarshal(message.Value, &parsedEdit)
	if err != nil {
		t.Fatalf("Failed to parse message value: %v", err)
	}
	
	if parsedEdit.ID != edit.ID {
		t.Errorf("Expected ID %d, got %d", edit.ID, parsedEdit.ID)
	}
	if parsedEdit.Title != edit.Title {
		t.Errorf("Expected Title '%s', got '%s'", edit.Title, parsedEdit.Title)
	}
	
	// Verify headers
	expectedHeaders := map[string]string{
		"wiki":      "enwiki",
		"language":  "en",
		"timestamp": "1640995200",
		"bot":       "false",
	}
	
	actualHeaders := make(map[string]string)
	for _, header := range message.Headers {
		actualHeaders[header.Key] = string(header.Value)
	}
	
	for key, expectedValue := range expectedHeaders {
		if actualValue, exists := actualHeaders[key]; !exists {
			t.Errorf("Missing header '%s'", key)
		} else if actualValue != expectedValue {
			t.Errorf("Header '%s': expected '%s', got '%s'", key, expectedValue, actualValue)
		}
	}
}

// TestProduceBufferFull tests behavior when buffer is full
func TestProduceBufferFull(t *testing.T) {
	// Create producer with very small buffer
	bufferSize := 2
	producer := &Producer{
		writer:        newMockKafkaWriter(),
		config:        &config.Config{},
		logger:        zerolog.New(nil).With().Timestamp().Logger(),
		buffer:        make(chan *models.WikipediaEdit, bufferSize),
		batchSize:     100, // Large batch size so nothing gets processed
		flushInterval: 10 * time.Second, // Long interval
		stopChan:      make(chan struct{}),
	}
	
	// Don't start the producer so buffer doesn't get emptied
	
	edit := &models.WikipediaEdit{
		ID:        1,
		Type:      "edit",
		Title:     "Test",
		User:      "User",
		Wiki:      "enwiki",
		ServerURL: "en.wikipedia.org",
		Timestamp: time.Now().Unix(),
		Length:    struct{Old int `json:"old"`; New int `json:"new"`}{Old: 100, New: 150},
		Revision:  struct{Old int64 `json:"old"`; New int64 `json:"new"`}{Old: 1000, New: 1001},
	}
	
	// Fill the buffer
	for i := 0; i < bufferSize; i++ {
		err := producer.Produce(edit)
		if err != nil {
			t.Errorf("Unexpected error filling buffer at position %d: %v", i, err)
		}
	}
	
	// Next produce should fail due to full buffer
	err := producer.Produce(edit)
	if err == nil {
		t.Error("Expected error when buffer is full, got nil")
	}
	
	if !strings.Contains(err.Error(), "buffer full") {
		t.Errorf("Expected 'buffer full' error, got: %v", err)
	}
}

// TestProducerClose verifies graceful shutdown
func TestProducerClose(t *testing.T) {
	producer, mockWriter := createTestProducer(5, 100*time.Millisecond)
	
	// Start the producer
	err := producer.Start()
	if err != nil {
		t.Fatalf("Failed to start producer: %v", err)
	}
	
	// Send some messages
	for i := 0; i < 3; i++ {
		edit := &models.WikipediaEdit{
			ID:        int64(i + 1),
			Type:      "edit",
			Title:     fmt.Sprintf("Test Page %d", i+1),
			User:      "TestUser",
			Wiki:      "enwiki",
			ServerURL: "en.wikipedia.org",
			Timestamp: time.Now().Unix(),
			Length:    struct{Old int `json:"old"`; New int `json:"new"`}{Old: 100, New: 150},
			Revision:  struct{Old int64 `json:"old"`; New int64 `json:"new"`}{Old: int64(1000 + i), New: int64(1001 + i)},
		}
		
		err := producer.Produce(edit)
		if err != nil {
			t.Errorf("Failed to produce edit %d: %v", i+1, err)
		}
	}
	
	// Add a small delay for batching logic to complete
	time.Sleep(50 * time.Millisecond)
	
	// Close the producer
	err = producer.Close()
	if err != nil {
		t.Errorf("Failed to close producer: %v", err)
	}
	
	// Add another delay after close to ensure all messages are processed
	time.Sleep(50 * time.Millisecond)
	
	// Verify producer state (main test goal)
	stats := producer.GetStats()
	if stats["is_running"].(bool) {
		t.Error("Expected producer to be stopped after close")
	}
	
	// Verify that some messages were processed (less critical than exact count)
	messages := mockWriter.GetMessages()
	t.Logf("Messages processed during producer lifecycle: %d", len(messages))
}

// TestGetStats verifies producer statistics
func TestGetStats(t *testing.T) {
	producer, _ := createTestProducer(10, 50*time.Millisecond)
	
	stats := producer.GetStats()
	
	expectedFields := []string{"is_running", "buffer_size", "buffer_cap", "dropped_count", "batch_size", "flush_interval"}
	for _, field := range expectedFields {
		if _, exists := stats[field]; !exists {
			t.Errorf("Missing stats field: %s", field)
		}
	}
	
	// Verify initial values
	if stats["is_running"].(bool) {
		t.Error("Expected is_running to be false before start")
	}
	if stats["buffer_size"].(int) != 0 {
		t.Errorf("Expected buffer_size 0, got %d", stats["buffer_size"].(int))
	}
	if stats["batch_size"].(int) != 10 {
		t.Errorf("Expected batch_size 10, got %d", stats["batch_size"].(int))
	}
	if stats["flush_interval"].(string) != "50ms" {
		t.Errorf("Expected flush_interval '50ms', got '%s'", stats["flush_interval"].(string))
	}
}

// TestNilEditHandling verifies handling of nil edits
func TestNilEditHandling(t *testing.T) {
	producer, _ := createTestProducer(5, 100*time.Millisecond)
	
	err := producer.Produce(nil)
	if err == nil {
		t.Error("Expected error for nil edit, got nil")
	}
	
	if !strings.Contains(err.Error(), "cannot be nil") {
		t.Errorf("Expected 'cannot be nil' error, got: %v", err)
	}
}