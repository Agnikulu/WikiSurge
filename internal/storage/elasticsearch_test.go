package storage

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
)

func TestElasticsearchClient_IndexNameGeneration(t *testing.T) {
	cfg := &config.Elasticsearch{
		URL:           "http://localhost:9200",
		RetentionDays: 7,
	}

	client, err := NewElasticsearchClient(cfg)
	if err != nil {
		t.Skip("Elasticsearch not available for testing")
	}

	// Test date-based index name generation
	testTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	expectedIndex := "wikipedia-edits-2024-01-15"
	
	actualIndex := client.getIndexName(testTime)
	
	if actualIndex != expectedIndex {
		t.Errorf("Expected index name %s, got %s", expectedIndex, actualIndex)
	}
}

func TestEditDocumentTransformation(t *testing.T) {
	edit := &models.WikipediaEdit{
		ID:        12345,
		Type:      "edit",
		Title:     "Test Article",
		User:      "TestUser",
		Bot:       false,
		Wiki:      "enwiki",
		Timestamp: 1642248000000, // Jan 15, 2022
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{
			Old: 1000,
			New: 1200,
		},
		Revision: struct {
			Old int64 `json:"old"`
			New int64 `json:"new"`
		}{
			Old: 98765,
			New: 98766,
		},
		Comment: "Updated information",
	}

	doc := models.FromWikipediaEdit(edit, "trending")

	// Verify transformation
	if doc.Title != edit.Title {
		t.Errorf("Expected title %s, got %s", edit.Title, doc.Title)
	}
	
	if doc.User != edit.User {
		t.Errorf("Expected user %s, got %s", edit.User, doc.User)
	}
	
	if doc.Bot != edit.Bot {
		t.Errorf("Expected bot %t, got %t", edit.Bot, doc.Bot)
	}
	
	if doc.IndexedReason != "trending" {
		t.Errorf("Expected indexed_reason 'trending', got %s", doc.IndexedReason)
	}
	
	if doc.ByteChange != 200 {
		t.Errorf("Expected byte_change 200, got %d", doc.ByteChange)
	}
	
	if doc.Language != "en" {
		t.Errorf("Expected language 'en', got %s", doc.Language)
	}
	
	// Verify ID generation (should be consistent)
	doc2 := models.FromWikipediaEdit(edit, "trending")
	if doc.ID != doc2.ID {
		t.Errorf("Document IDs should be consistent for same edit")
	}
}

func TestBulkOperationSerialization(t *testing.T) {
	edit := &models.WikipediaEdit{
		ID:        12345,
		Title:     "Test Article",
		User:      "TestUser",
		Wiki:      "enwiki",
		Timestamp: 1642248000000,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 1000, New: 1200},
	}

	doc := models.FromWikipediaEdit(edit, "test")
	
	// Test bulk operation structure
	bulkOp := BulkOperation{
		Index: &BulkIndex{
			Index: "wikipedia-edits-2024-01-15",
			ID:    doc.ID,
		},
	}
	
	bulkJSON, err := json.Marshal(bulkOp)
	if err != nil {
		t.Fatalf("Failed to marshal bulk operation: %v", err)
	}
	
	// Verify JSON structure
	var parsed map[string]interface{}
	err = json.Unmarshal(bulkJSON, &parsed)
	if err != nil {
		t.Fatalf("Failed to unmarshal bulk JSON: %v", err)
	}
	
	indexOp := parsed["index"].(map[string]interface{})
	if indexOp["_index"] != "wikipedia-edits-2024-01-15" {
		t.Errorf("Expected _index wikipedia-edits-2024-01-15, got %s", indexOp["_index"])
	}
	
	if indexOp["_id"] != doc.ID {
		t.Errorf("Expected _id %s, got %s", doc.ID, indexOp["_id"])
	}
}

func TestElasticsearchMappings(t *testing.T) {
	// Test that our document structure matches expected mapping
	edit := &models.WikipediaEdit{
		ID:        12345,
		Title:     "Test Article",
		User:      "TestUser",
		Bot:       true,
		Wiki:      "enwiki",
		Timestamp: 1642248000000,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 1000, New: 1500},
		Comment: "Test comment",
	}

	doc := models.FromWikipediaEdit(edit, "hot_page")
	
	// Marshal to JSON to verify field names
	docJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Failed to marshal document: %v", err)
	}
	
	var parsed map[string]interface{}
	err = json.Unmarshal(docJSON, &parsed)
	if err != nil {
		t.Fatalf("Failed to unmarshal document: %v", err)
	}
	
	// Verify all expected fields exist
	expectedFields := []string{
		"id", "title", "user", "bot", "wiki", 
		"timestamp", "byte_change", "comment", 
		"language", "indexed_reason",
	}
	
	for _, field := range expectedFields {
		if _, exists := parsed[field]; !exists {
			t.Errorf("Expected field %s missing from document", field)
		}
	}
	
	// Verify field types in JSON
	if _, ok := parsed["bot"].(bool); !ok {
		t.Errorf("Field 'bot' should be boolean")
	}
	
	if _, ok := parsed["byte_change"].(float64); !ok {
		t.Errorf("Field 'byte_change' should be numeric")
	}
}

func BenchmarkDocumentTransformation(b *testing.B) {
	edit := &models.WikipediaEdit{
		ID:        12345,
		Title:     "Benchmark Test Article",
		User:      "BenchmarkUser",
		Bot:       false,
		Wiki:      "enwiki",
		Timestamp: 1642248000000,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 1000, New: 1200},
		Comment: "Benchmark test comment",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = models.FromWikipediaEdit(edit, "benchmark")
	}
}

func BenchmarkBulkOperationCreation(b *testing.B) {
	edit := &models.WikipediaEdit{
		ID:        12345,
		Title:     "Benchmark Test",
		User:      "TestUser",
		Wiki:      "enwiki",
		Timestamp: 1642248000000,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 1000, New: 1200},
	}

	doc := models.FromWikipediaEdit(edit, "test")
	indexName := "wikipedia-edits-2024-01-15"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bulkOp := BulkOperation{
			Index: &BulkIndex{
				Index: indexName,
				ID:    doc.ID,
			},
		}
		_, _ = json.Marshal(bulkOp)
		_, _ = json.Marshal(doc)
	}
}