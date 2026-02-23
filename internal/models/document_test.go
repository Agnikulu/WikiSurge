package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromWikipediaEdit_Basic(t *testing.T) {
	edit := &WikipediaEdit{
		ID:        123,
		Title:     "Go_(programming_language)",
		User:      "TestUser",
		Bot:       false,
		Wiki:      "enwiki",
		Timestamp: time.Now().Unix(),
		Comment:   "Fixed typo",
	}
	edit.Length.Old = 1000
	edit.Length.New = 1050
	edit.Revision.Old = 100
	edit.Revision.New = 101

	doc := FromWikipediaEdit(edit, "trending_top_5")
	require.NotNil(t, doc)
	assert.Len(t, doc.ID, 16) // 16-character hex hash
	assert.Equal(t, "Go_(programming_language)", doc.Title)
	assert.Equal(t, "TestUser", doc.User)
	assert.False(t, doc.Bot)
	assert.Equal(t, "enwiki", doc.Wiki)
	assert.Equal(t, 50, doc.ByteChange)
	assert.Equal(t, "Fixed typo", doc.Comment)
	assert.Equal(t, "en", doc.Language)
	assert.Equal(t, "trending_top_5", doc.IndexedReason)
}

func TestFromWikipediaEdit_DeterministicID(t *testing.T) {
	edit := &WikipediaEdit{
		Wiki:      "enwiki",
		Title:     "Test",
		Timestamp: 1700000000,
	}
	edit.Revision.New = 42

	doc1 := FromWikipediaEdit(edit, "watchlist")
	doc2 := FromWikipediaEdit(edit, "watchlist")
	assert.Equal(t, doc1.ID, doc2.ID) // same inputs → same hash
}

func TestFromWikipediaEdit_EmptyWiki(t *testing.T) {
	edit := &WikipediaEdit{Wiki: "", Title: "X", Timestamp: time.Now().Unix()}
	doc := FromWikipediaEdit(edit, "test")
	assert.Equal(t, "unknown", doc.Language)
}

func TestFromWikipediaEdit_ShortWiki(t *testing.T) {
	edit := &WikipediaEdit{Wiki: "de", Title: "X", Timestamp: time.Now().Unix()}
	doc := FromWikipediaEdit(edit, "test")
	// Language() returns strings.TrimSuffix("de","wiki") = "de"
	assert.Equal(t, "de", doc.Language)
}

func TestFromWikipediaEdit_TimestampUTC(t *testing.T) {
	ts := int64(1700000000)
	edit := &WikipediaEdit{Wiki: "enwiki", Title: "X", Timestamp: ts}
	doc := FromWikipediaEdit(edit, "test")
	assert.Equal(t, time.UTC, doc.Timestamp.Location())
	assert.Equal(t, ts, doc.Timestamp.Unix())
}

func TestEditDocument_MarshalJSON(t *testing.T) {
	doc := &EditDocument{
		ID:            "abc123",
		Title:         "Test_Page",
		User:          "Editor",
		Bot:           true,
		Wiki:          "enwiki",
		Timestamp:     time.Date(2024, 1, 15, 12, 30, 45, 123000000, time.UTC),
		ByteChange:    -42,
		Comment:       "Reverted",
		Language:      "en",
		IndexedReason: "edit_war",
	}

	data, err := json.Marshal(doc)
	require.NoError(t, err)

	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))

	assert.Equal(t, "abc123", m["id"])
	assert.Equal(t, "Test_Page", m["title"])
	assert.Equal(t, true, m["bot"])
	assert.Equal(t, float64(-42), m["byte_change"])
	assert.Equal(t, "edit_war", m["indexed_reason"])

	// Timestamp should include milliseconds
	ts, ok := m["timestamp"].(string)
	require.True(t, ok)
	assert.Contains(t, ts, ".123Z")
}

func TestEditDocument_MarshalJSON_ZeroMillis(t *testing.T) {
	doc := &EditDocument{
		ID:        "x",
		Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	data, _ := json.Marshal(doc)
	var m map[string]interface{}
	json.Unmarshal(data, &m)
	assert.Contains(t, m["timestamp"].(string), ".000Z")
}
