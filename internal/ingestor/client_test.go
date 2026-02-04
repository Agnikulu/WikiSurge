package ingestor

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/rs/zerolog"
)

func TestWikiStreamClient_shouldProcess_BotFilter(t *testing.T) {
	logger := zerolog.New(nil).With().Timestamp().Logger()

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
			
			client := NewWikiStreamClient(cfg, logger)
			
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
			
			client := NewWikiStreamClient(cfg, logger)
			
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
			
			client := NewWikiStreamClient(cfg, logger)
			
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

	cfg := &config.Config{
		Ingestor: config.Ingestor{
			ExcludeBots:      true,
			AllowedLanguages: []string{"en", "es"},
		},
	}
	
	client := NewWikiStreamClient(cfg, logger)

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

func TestEditParsing(t *testing.T) {
	// Use current timestamp to pass validation
	currentTimestamp := time.Now().Unix() * 1000
	
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
	}`, currentTimestamp)

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