package integration

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/ingestor"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/rs/zerolog"
	kafkago "github.com/segmentio/kafka-go"
)

// testKafkaBroker represents an embedded test Kafka broker
type testKafkaBroker struct {
	mu       sync.Mutex
	topics   map[string][]kafkago.Message
	running  bool
	stopChan chan struct{}
}

func newTestKafkaBroker() *testKafkaBroker {
	return &testKafkaBroker{
		topics:   make(map[string][]kafkago.Message),
		stopChan: make(chan struct{}),
	}
}

func (b *testKafkaBroker) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	if b.running {
		return fmt.Errorf("broker already running")
	}
	
	b.running = true
	return nil
}

func (b *testKafkaBroker) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	if !b.running {
		return nil
	}
	
	b.running = false
	close(b.stopChan)
	return nil
}

func (b *testKafkaBroker) WriteMessage(topic string, message kafkago.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	if !b.running {
		return fmt.Errorf("broker not running")
	}
	
	if b.topics[topic] == nil {
		b.topics[topic] = make([]kafkago.Message, 0)
	}
	
	b.topics[topic] = append(b.topics[topic], message)
	return nil
}

func (b *testKafkaBroker) GetMessages(topic string) []kafkago.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	messages := b.topics[topic]
	if messages == nil {
		return []kafkago.Message{}
	}
	
	result := make([]kafkago.Message, len(messages))
	copy(result, messages)
	return result
}

func (b *testKafkaBroker) GetMessageCount(topic string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	if b.topics[topic] == nil {
		return 0
	}
	
	return len(b.topics[topic])
}

func (b *testKafkaBroker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	for topic := range b.topics {
		b.topics[topic] = b.topics[topic][:0]
	}
}

// testSSEEventSource simulates Wikipedia SSE events
type testSSEEventSource struct {
	mu       sync.Mutex
	events   []*models.WikipediaEdit
	index    int
	running  bool
	stopChan chan struct{}
	delay    time.Duration
}

func newTestSSEEventSource() *testSSEEventSource {
	return &testSSEEventSource{
		events:   make([]*models.WikipediaEdit, 0),
		stopChan: make(chan struct{}),
		delay:    10 * time.Millisecond, // 10ms between events
	}
}

func (s *testSSEEventSource) AddEvent(edit *models.WikipediaEdit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.events = append(s.events, edit)
}

func (s *testSSEEventSource) Start() <-chan *models.WikipediaEdit {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	eventChan := make(chan *models.WikipediaEdit, 10)
	
	if s.running {
		close(eventChan)
		return eventChan
	}
	
	s.running = true
	
	go func() {
		defer close(eventChan)
		
		for {
			select {
			case <-s.stopChan:
				return
			case <-time.After(s.delay):
				s.mu.Lock()
				if s.index < len(s.events) {
					event := s.events[s.index]
					s.index++
					s.mu.Unlock()
					
					select {
					case eventChan <- event:
					case <-s.stopChan:
						return
					}
				} else {
					s.mu.Unlock()
					// No more events, stop
					return
				}
			}
		}
	}()
	
	return eventChan
}

func (s *testSSEEventSource) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if !s.running {
		return
	}
	
	s.running = false
	close(s.stopChan)
}

func (s *testSSEEventSource) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.events = s.events[:0]
	s.index = 0
}

// testProducer integrates with test Kafka broker
type testProducer struct {
	broker *testKafkaBroker
	topic  string
	logger zerolog.Logger
	mu     sync.Mutex
	closed bool
}

func newTestProducer(broker *testKafkaBroker, topic string) *testProducer {
	return &testProducer{
		broker: broker,
		topic:  topic,
		logger: zerolog.New(nil).With().Timestamp().Logger(),
	}
}

func (p *testProducer) Produce(edit *models.WikipediaEdit) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.closed {
		return fmt.Errorf("producer closed")
	}
	
	// Convert edit to Kafka message
	value, err := json.Marshal(edit)
	if err != nil {
		return fmt.Errorf("failed to marshal edit: %w", err)
	}
	
	message := kafkago.Message{
		Key:   []byte(edit.Title),
		Value: value,
		Headers: []kafkago.Header{
			{Key: "wiki", Value: []byte(edit.Wiki)},
			{Key: "language", Value: []byte(edit.Language())},
			{Key: "timestamp", Value: []byte(fmt.Sprintf("%d", edit.Timestamp))},
		},
	}
	
	if edit.Bot {
		message.Headers = append(message.Headers, kafkago.Header{Key: "bot", Value: []byte("true")})
	} else {
		message.Headers = append(message.Headers, kafkago.Header{Key: "bot", Value: []byte("false")})
	}
	
	return p.broker.WriteMessage(p.topic, message)
}

func (p *testProducer) Start() error {
	return nil // No-op for test producer
}

func (p *testProducer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.closed = true
	return nil
}

func (p *testProducer) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"type": "test-producer",
		"closed": p.closed,
	}
}

// createTestEdit creates a test Wikipedia edit
func createTestEdit(id int64, title, user, wiki string, bot bool) *models.WikipediaEdit {
	return &models.WikipediaEdit{
		ID:        id,
		Type:      "edit",
		Title:     title,
		User:      user,
		Bot:       bot,
		Wiki:      wiki,
		ServerURL: fmt.Sprintf("%s.wikipedia.org", wiki[:2]),
		Timestamp: time.Now().Unix(),
		Length:    struct{Old int `json:"old"`; New int `json:"new"`}{Old: 100, New: 150},
		Revision:  struct{Old int64 `json:"old"`; New int64 `json:"new"`}{Old: 1000 + id, New: 1001 + id},
		Comment:   fmt.Sprintf("Test edit %d", id),
	}
}

// TestEndToEndFlow tests the complete ingestion pipeline
func TestEndToEndFlow(t *testing.T) {
	// Setup test infrastructure
	broker := newTestKafkaBroker()
	err := broker.Start()
	if err != nil {
		t.Fatalf("Failed to start test broker: %v", err)
	}
	defer broker.Stop()
	
	eventSource := newTestSSEEventSource()
	defer eventSource.Stop()
	
	topic := "test.wikipedia.edits"
	testProd := newTestProducer(broker, topic)
	
	// Create test configuration
	cfg := &config.Config{
		Ingestor: config.Ingestor{
			ExcludeBots:      false, // Allow all edits for this test
			AllowedLanguages: []string{}, // Allow all languages
			RateLimit:        1000, // High rate limit
			BurstLimit:       1000,
		},
	}
	
	logger := zerolog.New(nil).With().Timestamp().Logger()
	
	// For this test, we'll simulate the client behavior without actual SSE connection
	client := ingestor.NewWikiStreamClient(cfg, logger, testProd)
	
	// Generate 100 test events
	numEvents := 100
	expectedEdits := make([]*models.WikipediaEdit, numEvents)
	
	for i := 0; i < numEvents; i++ {
		edit := createTestEdit(
			int64(i+1),
			fmt.Sprintf("Test Page %d", i+1),
			fmt.Sprintf("User%d", i+1),
			"enwiki",
			i%10 == 0, // Every 10th edit is a bot edit
		)
		expectedEdits[i] = edit
		eventSource.AddEvent(edit)
	}
	
	// Start event processing simulation
	eventChan := eventSource.Start()
	
	// Process events through the client
	processedCount := 0
	for edit := range eventChan {
		// Simulate the client's processEvent logic
		if !client.ShouldProcess(edit) {
			continue
		}
		
		err := testProd.Produce(edit)
		if err != nil {
			t.Errorf("Failed to produce edit %d: %v", edit.ID, err)
		}
		processedCount++
	}
	
	// Wait a moment for all processing to complete
	time.Sleep(100 * time.Millisecond)
	
	// Verify all events were received and processed
	messages := broker.GetMessages(topic)
	
	if len(messages) != numEvents {
		t.Errorf("Expected %d messages in Kafka, got %d", numEvents, len(messages))
	}
	
	// Verify message content matches expectations
	for i, message := range messages {
		var receivedEdit models.WikipediaEdit
		err := json.Unmarshal(message.Value, &receivedEdit)
		if err != nil {
			t.Errorf("Failed to unmarshal message %d: %v", i, err)
			continue
		}
		
		expectedEdit := expectedEdits[i]
		
		if receivedEdit.ID != expectedEdit.ID {
			t.Errorf("Message %d: expected ID %d, got %d", i, expectedEdit.ID, receivedEdit.ID)
		}
		if receivedEdit.Title != expectedEdit.Title {
			t.Errorf("Message %d: expected Title %s, got %s", i, expectedEdit.Title, receivedEdit.Title)
		}
		
		// Verify headers
		headerMap := make(map[string]string)
		for _, header := range message.Headers {
			headerMap[header.Key] = string(header.Value)
		}
		
		if headerMap["wiki"] != expectedEdit.Wiki {
			t.Errorf("Message %d: expected wiki header %s, got %s", i, expectedEdit.Wiki, headerMap["wiki"])
		}
		if headerMap["language"] != expectedEdit.Language() {
			t.Errorf("Message %d: expected language header %s, got %s", i, expectedEdit.Language(), headerMap["language"])
		}
	}
	
	// Clean up
	testProd.Close()
}

// TestBackpressureHandling tests system behavior under backpressure
func TestBackpressureHandling(t *testing.T) {
	// Setup test infrastructure with slow processing
	broker := newTestKafkaBroker()
	err := broker.Start()
	if err != nil {
		t.Fatalf("Failed to start test broker: %v", err)
	}
	defer broker.Stop()
	
	topic := "test.wikipedia.edits"
	
	// Create a slow test producer that simulates slow Kafka
	slowProducer := &slowTestProducer{
		broker:    broker,
		topic:     topic,
		delay:     50 * time.Millisecond, // 50ms per write (slow)
		bufferCap: 10,                   // Small buffer
		buffer:    make(chan *models.WikipediaEdit, 10),
		stopChan:  make(chan struct{}),
	}
	
	err = slowProducer.Start()
	if err != nil {
		t.Fatalf("Failed to start slow producer: %v", err)
	}
	defer slowProducer.Close()
	
	cfg := &config.Config{
		Ingestor: config.Ingestor{
			RateLimit:  1000, // High rate limit to test backpressure, not rate limiting
			BurstLimit: 1000,
		},
	}
	
	logger := zerolog.New(nil).With().Timestamp().Logger()
	_ = ingestor.NewWikiStreamClient(cfg, logger, slowProducer) // Create but don't need to use it directly
	
	// Send 100 messages rapidly
	numMessages := 100
	var producedCount int32
	var droppedCount int32
	
	for i := 0; i < numMessages; i++ {
		edit := createTestEdit(
			int64(i+1),
			fmt.Sprintf("Backpressure Test %d", i+1),
			"TestUser",
			"enwiki",
			false,
		)
		
		err := slowProducer.Produce(edit)
		if err != nil {
			droppedCount++
			t.Logf("Message %d dropped due to backpressure: %v", i+1, err)
		} else {
			producedCount++
		}
	}
	
	// Wait for processing to complete
	time.Sleep(2 * time.Second)
	
	// Verify backpressure behavior
	processedCount := slowProducer.GetProcessedCount()
	
	t.Logf("Produced: %d, Dropped: %d, Processed: %d", producedCount, droppedCount, processedCount)
	
	// Should have some dropped messages due to backpressure
	if droppedCount == 0 {
		t.Error("Expected some messages to be dropped due to backpressure, but none were")
	}
	
	// Processed count should be less than or equal to produced count
	if processedCount > int32(producedCount) {
		t.Errorf("Processed count (%d) should not exceed produced count (%d)", processedCount, producedCount)
	}
	
	// Verify system recovers - send more messages slowly
	time.Sleep(100 * time.Millisecond) // Let buffer drain
	
	recoveryMessages := 5
	for i := 0; i < recoveryMessages; i++ {
		edit := createTestEdit(
			int64(1000+i),
			fmt.Sprintf("Recovery Test %d", i+1),
			"RecoveryUser",
			"enwiki",
			false,
		)
		
		err := slowProducer.Produce(edit)
		if err != nil {
			t.Errorf("Recovery message %d failed: %v", i+1, err)
		}
		
		time.Sleep(100 * time.Millisecond) // Send slowly to avoid backpressure
	}
	
	// Wait for recovery processing
	time.Sleep(1 * time.Second)
	
	finalProcessedCount := slowProducer.GetProcessedCount()
	
	// Should have processed additional recovery messages
	if finalProcessedCount <= processedCount {
		t.Errorf("Expected recovery processing, but count didn't increase: %d -> %d", processedCount, finalProcessedCount)
	}
}

// slowTestProducer simulates a slow Kafka producer for backpressure testing
type slowTestProducer struct {
	broker       *testKafkaBroker
	topic        string
	delay        time.Duration
	buffer       chan *models.WikipediaEdit
	bufferCap    int
	stopChan     chan struct{}
	wg           sync.WaitGroup
	mu           sync.Mutex
	processedCount int32
	running      bool
}

func (p *slowTestProducer) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.running {
		return fmt.Errorf("producer already running")
	}
	
	p.running = true
	p.wg.Add(1)
	
	go p.processingLoop()
	return nil
}

func (p *slowTestProducer) processingLoop() {
	defer p.wg.Done()
	
	for {
		select {
		case <-p.stopChan:
			return
		case edit := <-p.buffer:
			// Simulate slow processing
			time.Sleep(p.delay)
			
			// Convert and write to broker
			value, _ := json.Marshal(edit)
			message := kafkago.Message{
				Key:   []byte(edit.Title),
				Value: value,
			}
			
			p.broker.WriteMessage(p.topic, message)
			
			p.mu.Lock()
			p.processedCount++
			p.mu.Unlock()
		}
	}
}

func (p *slowTestProducer) Produce(edit *models.WikipediaEdit) error {
	select {
	case p.buffer <- edit:
		return nil
	default:
		return fmt.Errorf("producer buffer full, message dropped")
	}
}

func (p *slowTestProducer) Close() error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return nil
	}
	p.running = false
	p.mu.Unlock()
	
	close(p.stopChan)
	p.wg.Wait()
	close(p.buffer)
	return nil
}

func (p *slowTestProducer) GetProcessedCount() int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.processedCount
}

func (p *slowTestProducer) GetStats() map[string]interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return map[string]interface{}{
		"type": "slow-test-producer",
		"processed_count": p.processedCount,
		"running": p.running,
	}
}

// TestClientFiltering tests filtering behavior in integration context
func TestClientFiltering(t *testing.T) {
	broker := newTestKafkaBroker()
	err := broker.Start()
	if err != nil {
		t.Fatalf("Failed to start test broker: %v", err)
	}
	defer broker.Stop()
	
	topic := "test.wikipedia.edits"
	testProd := newTestProducer(broker, topic)
	defer testProd.Close()
	
	// Create config that excludes bots and only allows English
	cfg := &config.Config{
		Ingestor: config.Ingestor{
			ExcludeBots:      true,
			AllowedLanguages: []string{"en"},
			RateLimit:        1000,
			BurstLimit:       1000,
		},
	}
	
	logger := zerolog.New(nil).With().Timestamp().Logger()
	client := ingestor.NewWikiStreamClient(cfg, logger, testProd)
	
	// Create test edits with different characteristics
	testEdits := []*models.WikipediaEdit{
		createTestEdit(1, "Valid Edit", "User1", "enwiki", false),       // Should pass
		createTestEdit(2, "Bot Edit", "BotUser", "enwiki", true),        // Should be filtered (bot)
		createTestEdit(3, "French Edit", "User2", "frwiki", false),      // Should be filtered (language)
		createTestEdit(4, "Bot French", "BotUser", "frwiki", true),      // Should be filtered (both)
		createTestEdit(5, "Another Valid", "User3", "enwiki", false),    // Should pass
	}
	
	// Process each edit
	for _, edit := range testEdits {
		if client.ShouldProcess(edit) {
			err := testProd.Produce(edit)
			if err != nil {
				t.Errorf("Failed to produce edit %d: %v", edit.ID, err)
			}
		}
	}
	
	// Wait for processing
	time.Sleep(100 * time.Millisecond)
	
	// Verify only 2 messages passed filtering
	messages := broker.GetMessages(topic)
	expectedCount := 2
	
	if len(messages) != expectedCount {
		t.Errorf("Expected %d messages after filtering, got %d", expectedCount, len(messages))
	}
	
	// Verify the correct messages passed
	if len(messages) >= 1 {
		var firstEdit models.WikipediaEdit
		json.Unmarshal(messages[0].Value, &firstEdit)
		if firstEdit.ID != 1 {
			t.Errorf("Expected first message to be edit 1, got edit %d", firstEdit.ID)
		}
	}
	
	if len(messages) >= 2 {
		var secondEdit models.WikipediaEdit
		json.Unmarshal(messages[1].Value, &secondEdit)
		if secondEdit.ID != 5 {
			t.Errorf("Expected second message to be edit 5, got edit %d", secondEdit.ID)
		}
	}
}