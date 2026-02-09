package models

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// EditDocument represents a Wikipedia edit document for Elasticsearch indexing
type EditDocument struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	User          string    `json:"user"`
	Bot           bool      `json:"bot"`
	Wiki          string    `json:"wiki"`
	Timestamp     time.Time `json:"timestamp"`
	ByteChange    int       `json:"byte_change"`
	Comment       string    `json:"comment"`
	Language      string    `json:"language"`
	IndexedReason string    `json:"indexed_reason"`
}

// FromWikipediaEdit transforms a WikipediaEdit into an EditDocument for ES indexing
func FromWikipediaEdit(edit *WikipediaEdit, reason string) *EditDocument {
	// Generate unique document ID by hashing key fields
	idStr := fmt.Sprintf("%s-%s-%d-%d", 
		edit.Wiki, 
		edit.Title, 
		edit.Timestamp, 
		edit.Revision.New,
	)
	
	hash := sha256.Sum256([]byte(idStr))
	id := fmt.Sprintf("%x", hash)[:16] // Use first 16 characters of hash

	// Parse timestamp from Unix seconds and convert to UTC
	timestamp := time.Unix(edit.Timestamp, 0).UTC()

	// Extract language from wiki field (e.g., "enwiki" -> "en")
	language := edit.Language()
	if language == "" {
		// Fallback: try to extract from wiki field
		if len(edit.Wiki) >= 2 {
			language = strings.ToLower(edit.Wiki[:2])
		} else {
			language = "unknown"
		}
	}

	return &EditDocument{
		ID:            id,
		Title:         edit.Title,
		User:          edit.User,
		Bot:           edit.Bot,
		Wiki:          edit.Wiki,
		Timestamp:     timestamp,
		ByteChange:    edit.ByteChange(),
		Comment:       edit.Comment,
		Language:      language,
		IndexedReason: reason,
	}
}

// MarshalJSON implements custom JSON marshaling for EditDocument
// to ensure timestamp is formatted with milliseconds for Elasticsearch
func (d *EditDocument) MarshalJSON() ([]byte, error) {
	// Format timestamp with milliseconds for ES: yyyy-MM-dd'T'HH:mm:ss.SSS'Z'
	// Extract milliseconds from the timestamp
	millis := d.Timestamp.UnixNano() / 1000000 % 1000
	timestampStr := fmt.Sprintf("%s.%03dZ",
		d.Timestamp.UTC().Format("2006-01-02T15:04:05"),
		millis)
	
	// Create anonymous struct to avoid recursion
	return json.Marshal(&struct {
		ID            string `json:"id"`
		Title         string `json:"title"`
		User          string `json:"user"`
		Bot           bool   `json:"bot"`
		Wiki          string `json:"wiki"`
		Timestamp     string `json:"timestamp"`
		ByteChange    int    `json:"byte_change"`
		Comment       string `json:"comment"`
		Language      string `json:"language"`
		IndexedReason string `json:"indexed_reason"`
	}{
		ID:            d.ID,
		Title:         d.Title,
		User:          d.User,
		Bot:           d.Bot,
		Wiki:          d.Wiki,
		Timestamp:     timestampStr,
		ByteChange:    d.ByteChange,
		Comment:       d.Comment,
		Language:      d.Language,
		IndexedReason: d.IndexedReason,
	})
}