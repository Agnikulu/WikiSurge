package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWikipediaEdit_ByteChange(t *testing.T) {
	tests := []struct {
		name     string
		edit     WikipediaEdit
		expected int
	}{
		{
			name: "positive change",
			edit: WikipediaEdit{
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 150},
			},
			expected: 50,
		},
		{
			name: "negative change",
			edit: WikipediaEdit{
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 200, New: 150},
			},
			expected: -50,
		},
		{
			name: "no change",
			edit: WikipediaEdit{
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 100},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.edit.ByteChange()
			if result != tt.expected {
				t.Errorf("ByteChange() = %d, expected %d", result, tt.expected)
			}
		})
	}
}

func TestWikipediaEdit_Language(t *testing.T) {
	tests := []struct {
		name     string
		wiki     string
		expected string
	}{
		{
			name:     "english wikipedia",
			wiki:     "enwiki",
			expected: "en",
		},
		{
			name:     "spanish wikipedia",
			wiki:     "eswiki",
			expected: "es",
		},
		{
			name:     "german wikipedia",
			wiki:     "dewiki",
			expected: "de",
		},
		{
			name:     "empty wiki",
			wiki:     "",
			expected: "",
		},
		{
			name:     "single character wiki",
			wiki:     "e",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edit := WikipediaEdit{Wiki: tt.wiki}
			result := edit.Language()
			if result != tt.expected {
				t.Errorf("Language() = %s, expected %s", result, tt.expected)
			}
		})
	}
}

func TestWikipediaEdit_IsSignificant(t *testing.T) {
	tests := []struct {
		name     string
		edit     WikipediaEdit
		expected bool
	}{
		{
			name: "significant positive change",
			edit: WikipediaEdit{
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 250},
			},
			expected: true,
		},
		{
			name: "significant negative change",
			edit: WikipediaEdit{
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 250, New: 100},
			},
			expected: true,
		},
		{
			name: "insignificant positive change",
			edit: WikipediaEdit{
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 150},
			},
			expected: false,
		},
		{
			name: "insignificant negative change",
			edit: WikipediaEdit{
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 150, New: 100},
			},
			expected: false,
		},
		{
			name: "boundary case - exactly 100",
			edit: WikipediaEdit{
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 200},
			},
			expected: false,
		},
		{
			name: "boundary case - 101",
			edit: WikipediaEdit{
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 201},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.edit.IsSignificant()
			if result != tt.expected {
				t.Errorf("IsSignificant() = %t, expected %t", result, tt.expected)
			}
		})
	}
}

func TestWikipediaEdit_ToJSON(t *testing.T) {
	edit := WikipediaEdit{
		ID:        12345,
		Type:      "edit",
		Title:     "Test Page",
		User:      "TestUser",
		Bot:       false,
		Wiki:      "enwiki",
		ServerURL: "en.wikipedia.org",
		Timestamp: time.Now().Unix() * 1000,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 150},
		Revision: struct {
			Old int64 `json:"old"`
			New int64 `json:"new"`
		}{Old: 1000, New: 1001},
		Comment: "Test comment",
	}

	result := edit.ToJSON()
	if len(result) == 0 {
		t.Error("ToJSON() returned empty result")
	}

	// Test that result is valid JSON
	var parsed WikipediaEdit
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Errorf("ToJSON() result is not valid JSON: %v", err)
	}

	// Test that parsed data matches original
	if parsed.ID != edit.ID {
		t.Errorf("ToJSON() ID mismatch: got %d, expected %d", parsed.ID, edit.ID)
	}
	if parsed.Title != edit.Title {
		t.Errorf("ToJSON() Title mismatch: got %s, expected %s", parsed.Title, edit.Title)
	}
}

func TestWikipediaEdit_Validate(t *testing.T) {
	validEdit := WikipediaEdit{
		ID:        12345,
		Type:      "edit",
		Title:     "Test Page",
		User:      "TestUser",
		Bot:       false,
		Wiki:      "enwiki",
		ServerURL: "en.wikipedia.org",
		Timestamp: time.Now().Unix() * 1000,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 150},
		Revision: struct {
			Old int64 `json:"old"`
			New int64 `json:"new"`
		}{Old: 1000, New: 1001},
		Comment: "Test comment",
	}

	// Test valid edit
	if err := validEdit.Validate(); err != nil {
		t.Errorf("Validate() failed for valid edit: %v", err)
	}

	// Test invalid cases
	tests := []struct {
		name        string
		edit        WikipediaEdit
		expectError bool
	}{
		{
			name:        "zero ID",
			edit:        func() WikipediaEdit { e := validEdit; e.ID = 0; return e }(),
			expectError: true,
		},
		{
			name:        "empty type",
			edit:        func() WikipediaEdit { e := validEdit; e.Type = ""; return e }(),
			expectError: true,
		},
		{
			name:        "empty title",
			edit:        func() WikipediaEdit { e := validEdit; e.Title = ""; return e }(),
			expectError: true,
		},
		{
			name:        "title too long",
			edit:        func() WikipediaEdit { e := validEdit; e.Title = string(make([]byte, 257)); return e }(),
			expectError: true,
		},
		{
			name:        "empty user",
			edit:        func() WikipediaEdit { e := validEdit; e.User = ""; return e }(),
			expectError: true,
		},
		{
			name:        "empty wiki",
			edit:        func() WikipediaEdit { e := validEdit; e.Wiki = ""; return e }(),
			expectError: true,
		},
		{
			name:        "empty server URL",
			edit:        func() WikipediaEdit { e := validEdit; e.ServerURL = ""; return e }(),
			expectError: true,
		},
		{
			name:        "zero timestamp",
			edit:        func() WikipediaEdit { e := validEdit; e.Timestamp = 0; return e }(),
			expectError: true,
		},
		{
			name:        "future timestamp",
			edit:        func() WikipediaEdit { e := validEdit; e.Timestamp = (time.Now().Unix() + 3600) * 1000; return e }(),
			expectError: true,
		},
		{
			name:        "negative length old",
			edit:        func() WikipediaEdit { e := validEdit; e.Length.Old = -1; return e }(),
			expectError: true,
		},
		{
			name:        "negative length new",
			edit:        func() WikipediaEdit { e := validEdit; e.Length.New = -1; return e }(),
			expectError: true,
		},
		{
			name:        "negative revision old",
			edit:        func() WikipediaEdit { e := validEdit; e.Revision.Old = -1; return e }(),
			expectError: true,
		},
		{
			name:        "negative revision new",
			edit:        func() WikipediaEdit { e := validEdit; e.Revision.New = -1; return e }(),
			expectError: true,
		},
		{
			name:        "comment too long",
			edit:        func() WikipediaEdit { e := validEdit; e.Comment = string(make([]byte, 501)); return e }(),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.edit.Validate()
			if tt.expectError && err == nil {
				t.Error("Validate() expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Validate() unexpected error: %v", err)
			}
		})
	}
}