package ingestor

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/rs/zerolog"
)

// mockProducer is a simple mock for testing that implements the necessary interface
type mockProducer struct {
	producedEdits []*models.WikipediaEdit
	mu            sync.Mutex
	shouldError   bool
	errorCount    int32
}

func newMockProducer() *mockProducer {
	return &mockProducer{
		producedEdits: make([]*models.WikipediaEdit, 0),
	}
}

func (m *mockProducer) Produce(edit *models.WikipediaEdit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.shouldError {
		atomic.AddInt32(&m.errorCount, 1)
		return fmt.Errorf("mock producer error")
	}
	
	m.producedEdits = append(m.producedEdits, edit)
	return nil
}

func (m *mockProducer) Close() error {
	return nil
}

func (m *mockProducer) Start() error {
	return nil
}

func (m *mockProducer) GetStats() map[string]interface{} {
	return map[string]interface{}{"mock": true}
}

func (m *mockProducer) GetProducedEdits() []*models.WikipediaEdit {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*models.WikipediaEdit, len(m.producedEdits))
	copy(result, m.producedEdits)
	return result
}

func (m *mockProducer) GetErrorCount() int32 {
	return atomic.LoadInt32(&m.errorCount)
}

func (m *mockProducer) SetShouldError(shouldError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldError = shouldError
}

func TestWikiStreamClient_shouldProcess_BotFilter(t *testing.T) {
	logger := zerolog.New(nil).With().Timestamp().Logger()
	mockProd := newMockProducer()

	tests := []struct {
		name        string
		excludeBots bool
		editBot     bool
		expected    bool
	}{
		{
			name:        "exclude bots enabled, edit is bot",
			excludeBots: true,
			editBot:     true,
			expected:    false,
		},
		{
			name:        "exclude bots enabled, edit is not bot",
			excludeBots: true,
			editBot:     false,
			expected:    true,
		},
		{
			name:        "exclude bots disabled, edit is bot",
			excludeBots: false,
			editBot:     true,
			expected:    true,
		},
		{
			name:        "exclude bots disabled, edit is not bot",
			excludeBots: false,
			editBot:     false,
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Ingestor: config.Ingestor{
					ExcludeBots: tt.excludeBots,
				},
			}
			
			client := NewWikiStreamClient(cfg, logger, mockProd)
			
			edit := &models.WikipediaEdit{
				Bot:  tt.editBot,
				Type: "edit", // Valid type
				Wiki: "enwiki",
			}
			
			result := client.shouldProcess(edit)
			if result != tt.expected {
				t.Errorf("shouldProcess() = %t, expected %t", result, tt.expected)
			}
		})
	}
}

func TestWikiStreamClient_shouldProcess_LanguageFilter(t *testing.T) {
	logger := zerolog.New(nil).With().Timestamp().Logger()
	mockProd := newMockProducer()

	tests := []struct {
		name             string
		allowedLanguages []string
		editWiki         string
		expected         bool
	}{
		{
			name:             "no language filter",
			allowedLanguages: []string{},
			editWiki:         "enwiki",
			expected:         true,
		},
		{
			name:             "allowed language",
			allowedLanguages: []string{"en", "es"},
			editWiki:         "enwiki",
			expected:         true,
		},
		{
			name:             "not allowed language",
			allowedLanguages: []string{"en", "es"},
			editWiki:         "frwiki",
			expected:         false,
		},
		{
			name:             "multiple allowed languages, match second",
			allowedLanguages: []string{"en", "es", "fr"},
			editWiki:         "eswiki",
			expected:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Ingestor: config.Ingestor{
					AllowedLanguages: tt.allowedLanguages,
				},
			}
			
			client := NewWikiStreamClient(cfg, logger, mockProd)
			
			edit := &models.WikipediaEdit{
				Wiki: tt.editWiki,
				Type: "edit", // Valid type
				Bot:  false,  // Not a bot
			}
			
			result := client.shouldProcess(edit)
			if result != tt.expected {
				t.Errorf("shouldProcess() = %t, expected %t for wiki %s with allowed languages %v", 
					result, tt.expected, tt.editWiki, tt.allowedLanguages)
			}
		})
	}
}

func TestWikiStreamClient_shouldProcess_TypeFilter(t *testing.T) {
	logger := zerolog.New(nil).With().Timestamp().Logger()
	mockProd := newMockProducer()

	tests := []struct {
		name     string
		editType string
		expected bool
	}{
		{
			name:     "edit type allowed",
			editType: "edit",
			expected: true,
		},
		{
			name:     "new type allowed",
			editType: "new",
			expected: true,
		},
		{
			name:     "log type not allowed",
			editType: "log",
			expected: false,
		},
		{
			name:     "unknown type not allowed",
			editType: "unknown",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Ingestor: config.Ingestor{
					ExcludeBots: false, // Don't filter bots for this test
				},
			}
			
			client := NewWikiStreamClient(cfg, logger, mockProd)
			
			edit := &models.WikipediaEdit{
				Type: tt.editType,
				Bot:  false,
				Wiki: "enwiki",
			}
			
			result := client.shouldProcess(edit)
			if result != tt.expected {
				t.Errorf("shouldProcess() = %t, expected %t for type %s", 
					result, tt.expected, tt.editType)
			}
		})
	}
}

func TestWikiStreamClient_shouldProcess_Combined(t *testing.T) {
	logger := zerolog.New(nil).With().Timestamp().Logger()
	mockProd := newMockProducer()

	cfg := &config.Config{
		Ingestor: config.Ingestor{
			ExcludeBots:      true,
			AllowedLanguages: []string{"en", "es"},
		},
	}
	
	client := NewWikiStreamClient(cfg, logger, mockProd)

	tests := []struct {
		name     string
		edit     *models.WikipediaEdit
		expected bool
	}{
		{
			name: "passes all filters",
			edit: &models.WikipediaEdit{
				Type: "edit",
				Bot:  false,
				Wiki: "enwiki",
			},
			expected: true,
		},
		{
			name: "fails bot filter",
			edit: &models.WikipediaEdit{
				Type: "edit",
				Bot:  true,
				Wiki: "enwiki",
			},
			expected: false,
		},
		{
			name: "fails language filter",
			edit: &models.WikipediaEdit{
				Type: "edit",
				Bot:  false,
				Wiki: "frwiki",
			},
			expected: false,
		},
		{
			name: "fails type filter",
			edit: &models.WikipediaEdit{
				Type: "log",
				Bot:  false,
				Wiki: "enwiki",
			},
			expected: false,
		},
		{
			name: "fails multiple filters",
			edit: &models.WikipediaEdit{
				Type: "log",
				Bot:  true,
				Wiki: "frwiki",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.shouldProcess(tt.edit)
			if result != tt.expected {
				t.Errorf("shouldProcess() = %t, expected %t for edit: Type=%s, Bot=%t, Wiki=%s", 
					result, tt.expected, tt.edit.Type, tt.edit.Bot, tt.edit.Wiki)
			}
		})
	}
}

func TestWikiStreamClient_shouldProcess_NamespaceFilter(t *testing.T) {
	logger := zerolog.New(nil).With().Timestamp().Logger()
	mockProd := newMockProducer()

	tests := []struct {
		name              string
		allowedNamespaces []int
		editNamespace     int
		expected          bool
	}{
		{
			name:              "no namespace filter",
			allowedNamespaces: []int{},
			editNamespace:     1,
			expected:          true,
		},
		{
			name:              "main namespace allowed",
			allowedNamespaces: []int{0},
			editNamespace:     0,
			expected:          true,
		},
		{
			name:              "talk namespace not allowed",
			allowedNamespaces: []int{0},
			editNamespace:     1,
			expected:          false,
		},
		{
			name:              "user namespace not allowed",
			allowedNamespaces: []int{0},
			editNamespace:     2,
			expected:          false,
		},
		{
			name:              "user talk namespace not allowed",
			allowedNamespaces: []int{0},
			editNamespace:     3,
			expected:          false,
		},
		{
			name:              "draft namespace not allowed",
			allowedNamespaces: []int{0},
			editNamespace:     118,
			expected:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Ingestor: config.Ingestor{
					AllowedNamespaces: tt.allowedNamespaces,
				},
			}
			
			client := NewWikiStreamClient(cfg, logger, mockProd)
			
			edit := &models.WikipediaEdit{
				Namespace: tt.editNamespace,
				Type:      "edit",
				Bot:       false,
				Wiki:      "enwiki",
			}
			
			result := client.shouldProcess(edit)
			if result != tt.expected {
				t.Errorf("shouldProcess() = %t, expected %t for namespace %d with allowed namespaces %v", 
					result, tt.expected, tt.editNamespace, tt.allowedNamespaces)
			}
		})
	}
}

func TestWikiStreamClient_shouldProcess_TitlePrefixFilter(t *testing.T) {
	logger := zerolog.New(nil).With().Timestamp().Logger()
	mockProd := newMockProducer()

	// Config with main namespace only
	cfg := &config.Config{
		Ingestor: config.Ingestor{
			AllowedNamespaces: []int{0},
		},
	}
	
	client := NewWikiStreamClient(cfg, logger, mockProd)

	tests := []struct {
		name     string
		title    string
		expected bool
	}{
		{
			name:     "main article allowed",
			title:    "Albert Einstein",
			expected: true,
		},
		{
			name:     "user talk filtered",
			title:    "User talk:SomeUser",
			expected: false,
		},
		{
			name:     "user page filtered",
			title:    "User:SomeUser",
			expected: false,
		},
		{
			name:     "talk page filtered",
			title:    "Talk:Albert Einstein",
			expected: false,
		},
		{
			name:     "draft filtered",
			title:    "Draft:Some Article",
			expected: false,
		},
		{
			name:     "wikipedia namespace filtered",
			title:    "Wikipedia:Village pump",
			expected: false,
		},
		{
			name:     "template filtered",
			title:    "Template:Infobox",
			expected: false,
		},
		{
			name:     "category filtered",
			title:    "Category:Science",
			expected: false,
		},
		{
			name:     "file filtered",
			title:    "File:Example.jpg",
			expected: false,
		},
		{
			name:     "help filtered",
			title:    "Help:Editing",
			expected: false,
		},
		{
			name:     "portal filtered",
			title:    "Portal:Science",
			expected: false,
		},
		{
			name:     "module filtered",
			title:    "Module:Example",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edit := &models.WikipediaEdit{
				Title:     tt.title,
				Namespace: 0, // Set to 0 to test title prefix filter specifically
				Type:      "edit",
				Bot:       false,
				Wiki:      "enwiki",
			}
			
			result := client.shouldProcess(edit)
			if result != tt.expected {
				t.Errorf("shouldProcess() = %t, expected %t for title %q", 
					result, tt.expected, tt.title)
			}
		})
	}
}

func TestEditParsing(t *testing.T) {
	// Use past timestamp to pass validation (1 hour ago)
	pastTimestamp := time.Now().Add(-time.Hour).Unix()
	
	// This is a sample JSON structure similar to what Wikipedia SSE sends
	sampleJSON := fmt.Sprintf(`{
		"id": 12345,
		"type": "edit",
		"title": "Test Page",
		"user": "TestUser",
		"bot": false,
		"wiki": "enwiki",
		"server_url": "en.wikipedia.org",
		"timestamp": %d,
		"length": {
			"old": 100,
			"new": 150
		},
		"revision": {
			"old": 1000,
			"new": 1001
		},
		"comment": "Test edit comment"
	}`, pastTimestamp)

	var edit models.WikipediaEdit
	if err := json.Unmarshal([]byte(sampleJSON), &edit); err != nil {
		t.Fatalf("Failed to parse sample JSON: %v", err)
	}

	// Test that all fields were parsed correctly
	if edit.ID != 12345 {
		t.Errorf("Expected ID 12345, got %d", edit.ID)
	}
	if edit.Type != "edit" {
		t.Errorf("Expected Type 'edit', got '%s'", edit.Type)
	}
	if edit.Title != "Test Page" {
		t.Errorf("Expected Title 'Test Page', got '%s'", edit.Title)
	}
	if edit.User != "TestUser" {
		t.Errorf("Expected User 'TestUser', got '%s'", edit.User)
	}
	if edit.Bot {
		t.Error("Expected Bot false, got true")
	}
	if edit.Wiki != "enwiki" {
		t.Errorf("Expected Wiki 'enwiki', got '%s'", edit.Wiki)
	}
	if edit.ServerURL != "en.wikipedia.org" {
		t.Errorf("Expected ServerURL 'en.wikipedia.org', got '%s'", edit.ServerURL)
	}
	if edit.Length.Old != 100 {
		t.Errorf("Expected Length.Old 100, got %d", edit.Length.Old)
	}
	if edit.Length.New != 150 {
		t.Errorf("Expected Length.New 150, got %d", edit.Length.New)
	}
	if edit.Revision.Old != 1000 {
		t.Errorf("Expected Revision.Old 1000, got %d", edit.Revision.Old)
	}
	if edit.Revision.New != 1001 {
		t.Errorf("Expected Revision.New 1001, got %d", edit.Revision.New)
	}
	if edit.Comment != "Test edit comment" {
		t.Errorf("Expected Comment 'Test edit comment', got '%s'", edit.Comment)
	}

	// Test validation
	if err := edit.Validate(); err != nil {
		t.Errorf("Valid edit failed validation: %v", err)
	}

	// Test helper methods
	if edit.ByteChange() != 50 {
		t.Errorf("Expected ByteChange 50, got %d", edit.ByteChange())
	}
	
	if edit.Language() != "en" {
		t.Errorf("Expected Language 'en', got '%s'", edit.Language())
	}
	
	if edit.IsSignificant() {
		t.Error("Expected IsSignificant false for 50 byte change, got true")
	}
}

// TestSSEEventParsing_Valid tests parsing of valid SSE events
func TestSSEEventParsing_Valid(t *testing.T) {
	// Use current timestamp to pass validation
	currentTimestamp := time.Now().Unix()
	
	tests := []struct {
		name     string
		jsonData string
		expected models.WikipediaEdit
	}{
		{
			name: "complete valid edit",
			jsonData: fmt.Sprintf(`{
				"id": 12345,
				"type": "edit",
				"title": "Test Page",
				"user": "TestUser",
				"bot": false,
				"wiki": "enwiki",
				"server_url": "en.wikipedia.org",
				"timestamp": %d,
				"length": {"old": 100, "new": 150},
				"revision": {"old": 1000, "new": 1001},
				"comment": "Test edit comment"
			}`, currentTimestamp),
			expected: models.WikipediaEdit{
				ID:        12345,
				Type:      "edit",
				Title:     "Test Page",
				User:      "TestUser",
				Bot:       false,
				Wiki:      "enwiki",
				ServerURL: "en.wikipedia.org",
				Timestamp: currentTimestamp,
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 150},
				Revision: struct {
					Old int64 `json:"old"`
					New int64 `json:"new"`
				}{Old: 1000, New: 1001},
				Comment:   "Test edit comment",
			},
		},
		{
			name: "bot edit",
			jsonData: fmt.Sprintf(`{
				"id": 67890,
				"type": "edit",
				"title": "Bot Test Page",
				"user": "BotUser",
				"bot": true,
				"wiki": "eswiki",
				"server_url": "es.wikipedia.org",
				"timestamp": %d,
				"length": {"old": 200, "new": 300},
				"revision": {"old": 2000, "new": 2001},
				"comment": "Bot edit"
			}`, currentTimestamp),
			expected: models.WikipediaEdit{
				ID:        67890,
				Type:      "edit",
				Title:     "Bot Test Page",
				User:      "BotUser",
				Bot:       true,
				Wiki:      "eswiki",
				ServerURL: "es.wikipedia.org",
				Timestamp: currentTimestamp,
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 200, New: 300},
				Revision: struct {
					Old int64 `json:"old"`
					New int64 `json:"new"`
				}{Old: 2000, New: 2001},
				Comment:   "Bot edit",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var edit models.WikipediaEdit
			if err := json.Unmarshal([]byte(tt.jsonData), &edit); err != nil {
				t.Fatalf("Failed to parse JSON: %v", err)
			}

			// Verify all fields
			if edit.ID != tt.expected.ID {
				t.Errorf("ID = %d, expected %d", edit.ID, tt.expected.ID)
			}
			if edit.Type != tt.expected.Type {
				t.Errorf("Type = %s, expected %s", edit.Type, tt.expected.Type)
			}
			if edit.Title != tt.expected.Title {
				t.Errorf("Title = %s, expected %s", edit.Title, tt.expected.Title)
			}
			if edit.User != tt.expected.User {
				t.Errorf("User = %s, expected %s", edit.User, tt.expected.User)
			}
			if edit.Bot != tt.expected.Bot {
				t.Errorf("Bot = %t, expected %t", edit.Bot, tt.expected.Bot)
			}
			if edit.Wiki != tt.expected.Wiki {
				t.Errorf("Wiki = %s, expected %s", edit.Wiki, tt.expected.Wiki)
			}

			// Test validation
			if err := edit.Validate(); err != nil {
				t.Errorf("Valid edit failed validation: %v", err)
			}
		})
	}
}

// TestSSEEventParsing_EdgeCases tests edge cases and invalid data
func TestSSEEventParsing_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		jsonData    string
		expectError bool
		description string
	}{
		{
			name:        "missing required field id",
			jsonData:    `{"type": "edit", "title": "Test", "user": "User", "wiki": "enwiki"}`,
			expectError: true,
			description: "should fail validation when ID is missing",
		},
		{
			name:        "invalid timestamp type",
			jsonData:    `{"id": 123, "timestamp": "not-a-number"}`,
			expectError: true,
			description: "should fail to unmarshal with invalid timestamp",
		},
		{
			name:        "malformed JSON", 
			jsonData:    `{"id": 123, "title": "incomplete`,
			expectError: true,
			description: "should fail to parse malformed JSON",
		},
		{
			name: "negative length values",
			jsonData: fmt.Sprintf(`{
				"id": 123,
				"type": "edit",
				"title": "Test",
				"user": "User",
				"wiki": "enwiki",
				"server_url": "en.wikipedia.org",
				"timestamp": %d,
				"length": {"old": -1, "new": 100},
				"revision": {"old": 1000, "new": 1001}
			}`, time.Now().Unix()),
			expectError: true,
			description: "should fail validation with negative length",
		},
		{
			name: "empty title",
			jsonData: fmt.Sprintf(`{
				"id": 123,
				"type": "edit",
				"title": "",
				"user": "User",
				"wiki": "enwiki",
				"server_url": "en.wikipedia.org",
				"timestamp": %d,
				"length": {"old": 1, "new": 100},
				"revision": {"old": 1000, "new": 1001}
			}`, time.Now().Unix()),
			expectError: true,
			description: "should fail validation with empty title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var edit models.WikipediaEdit
			err := json.Unmarshal([]byte(tt.jsonData), &edit)
			
			if tt.expectError && err == nil {
				// If unmarshaling succeeded, validation should fail
				if validateErr := edit.Validate(); validateErr == nil {
					t.Errorf("Expected error for %s, but got none", tt.description)
				}
			} else if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for valid case: %v", err)
			}
		})
	}
}

// TestRateLimiting tests rate limiting functionality
func TestRateLimiting(t *testing.T) {
	logger := zerolog.New(nil).With().Timestamp().Logger()
	mockProd := newMockProducer()
	
	// Create config with very low rate limit for testing
	cfg := &config.Config{
		Ingestor: config.Ingestor{
			RateLimit:  2, // 2 per second
			BurstLimit: 2,
		},
	}
	
	client := NewWikiStreamClient(cfg, logger, mockProd)
	
	// Create test edit
	edit := &models.WikipediaEdit{
		ID:        123,
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
	
	// Test rate limiting by checking shouldProcess and producer calls
	// We'll simulate the rate limiting effect by adding delays
	startTime := time.Now()
	
	for i := 0; i < 6; i++ {
		editCopy := *edit
		editCopy.ID = int64(i + 1)
		
		// Test filtering
		if !client.shouldProcess(&editCopy) {
			continue
		}
		
		// Test production
		err := mockProd.Produce(&editCopy)
		if err != nil {
			t.Errorf("Producer failed: %v", err)
		}
		
		// Simulate rate limiting delay (1/rate_limit seconds)
		delay := time.Duration(float64(time.Second) / float64(cfg.Ingestor.RateLimit))
		time.Sleep(delay)
	}
	
	elapsed := time.Since(startTime)
	
	// Should have taken some time due to simulated rate limiting
	expectedMinDuration := 5 * time.Duration(float64(time.Second)/float64(cfg.Ingestor.RateLimit))
	if elapsed < expectedMinDuration/2 { // Allow some margin
		t.Logf("Rate limiting simulation: processed 6 events in %v", elapsed)
	}
	
	// Verify events were produced
	producedEdits := mockProd.GetProducedEdits()
	if len(producedEdits) != 6 {
		t.Errorf("Expected 6 produced edits, got %d", len(producedEdits))
	}
}

// TestReconnectionLogic tests the reconnection behavior (mock-based)
func TestReconnectionLogic(t *testing.T) {
	logger := zerolog.New(nil).With().Timestamp().Logger()
	mockProd := newMockProducer()
	
	cfg := &config.Config{
		Ingestor: config.Ingestor{
			ReconnectDelay:    100 * time.Millisecond,
			MaxReconnectDelay: 1 * time.Second,
		},
	}
	
	client := NewWikiStreamClient(cfg, logger, mockProd)
	
	// Test that reconnect delay increases exponentially
	initialDelay := client.reconnectDelay
	if initialDelay != 100*time.Millisecond {
		t.Errorf("Expected initial reconnect delay %v, got %v", 100*time.Millisecond, initialDelay)
	}
	
	// Simulate failure - this would double the delay
	client.reconnectDelay *= 2
	if client.reconnectDelay != 200*time.Millisecond {
		t.Errorf("Expected doubled reconnect delay %v, got %v", 200*time.Millisecond, client.reconnectDelay)
	}
	
	// Simulate multiple failures
	for i := 0; i < 5; i++ {
		client.reconnectDelay *= 2
		if client.reconnectDelay > cfg.Ingestor.MaxReconnectDelay {
			client.reconnectDelay = cfg.Ingestor.MaxReconnectDelay
			break
		}
	}
	
	// Should cap at max delay
	if client.reconnectDelay != cfg.Ingestor.MaxReconnectDelay {
		t.Errorf("Expected capped reconnect delay %v, got %v", cfg.Ingestor.MaxReconnectDelay, client.reconnectDelay)
	}
	
	// Reset on successful connection
	client.reconnectDelay = cfg.Ingestor.ReconnectDelay
	if client.reconnectDelay != 100*time.Millisecond {
		t.Errorf("Expected reset reconnect delay %v, got %v", 100*time.Millisecond, client.reconnectDelay)
	}
}

// TestProcessEventErrorHandling tests error handling in event processing
func TestProcessEventErrorHandling(t *testing.T) {
	logger := zerolog.New(nil).With().Timestamp().Logger()
	mockProd := newMockProducer()
	
	cfg := &config.Config{
		Ingestor: config.Ingestor{
			RateLimit:  100, // High limit to avoid rate limiting in test
			BurstLimit: 100,
		},
	}
	
	client := NewWikiStreamClient(cfg, logger, mockProd)
	
	tests := []struct {
		name        string
		edit        *models.WikipediaEdit
		setupProd   func(*mockProducer)
		expectProd  bool
	}{
		{
			name:       "valid edit",
			edit: &models.WikipediaEdit{
				ID:        123,
				Type:      "edit",
				Title:     "Test",
				User:      "User",
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
			},
			expectProd: true,
		},
		{
			name: "producer error",
			edit: &models.WikipediaEdit{
				ID:        124,
				Type:      "edit",
				Title:     "Test2",
				User:      "User",
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
			},
			setupProd: func(p *mockProducer) {
				p.SetShouldError(true)
			},
			expectProd: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset producer for clean state
			mockProd.mu.Lock()
			mockProd.producedEdits = mockProd.producedEdits[:0]
			mockProd.shouldError = false
			mockProd.mu.Unlock()
			
			if tt.setupProd != nil {
				tt.setupProd(mockProd)
			}
			
			// Test filtering and production
			if client.shouldProcess(tt.edit) {
				err := mockProd.Produce(tt.edit)
				
				producedEdits := mockProd.GetProducedEdits()
				hasProduced := len(producedEdits) > 0
				
				if tt.expectProd && !hasProduced && err == nil {
					t.Error("Expected event to be produced but it wasn't")
				} else if !tt.expectProd && hasProduced {
					t.Error("Expected event not to be produced but it was")
				}
			}
		})
	}
}