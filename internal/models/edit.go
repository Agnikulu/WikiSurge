package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// WikipediaEdit represents a single Wikipedia edit event from the SSE stream
type WikipediaEdit struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Namespace int    `json:"ns"`  // Wikipedia namespace: 0=Main, 1=Talk, 2=User, etc.
	Title     string `json:"title"`
	User      string `json:"user"`
	Bot       bool   `json:"bot"`
	Wiki      string `json:"wiki"`
	ServerURL string `json:"server_url"`
	Timestamp int64  `json:"timestamp"`
	Length    struct {
		Old int `json:"old"`
		New int `json:"new"`
	} `json:"length"`
	Revision struct {
		Old int64 `json:"old"`
		New int64 `json:"new"`
	} `json:"revision"`
	Comment string `json:"comment"`
}

// ByteChange calculates the change in bytes for this edit
func (e *WikipediaEdit) ByteChange() int {
	return e.Length.New - e.Length.Old
}

// Language extracts the language code from the wiki field
// For example, "enwiki" returns "en", "eswiki" returns "es", "simplewiki" returns "simple"
func (e *WikipediaEdit) Language() string {
	if len(e.Wiki) < 2 {
		return ""
	}
	return strings.TrimSuffix(e.Wiki, "wiki")
}

// IsMainNamespace returns true if this edit is in the main article namespace (ns=0)
func (e *WikipediaEdit) IsMainNamespace() bool {
	return e.Namespace == 0
}

// IsSignificant returns true if the absolute byte change is greater than 100
func (e *WikipediaEdit) IsSignificant() bool {
	byteChange := e.ByteChange()
	if byteChange < 0 {
		byteChange = -byteChange
	}
	return byteChange > 100
}

// ToJSON marshals the edit to JSON for Kafka
func (e *WikipediaEdit) ToJSON() []byte {
	data, err := json.Marshal(e)
	if err != nil {
		// Return empty JSON object if marshaling fails
		return []byte("{}")
	}
	return data
}

// Validate checks if the edit has all required fields and valid values
func (e *WikipediaEdit) Validate() error {
	if e.ID == 0 {
		return fmt.Errorf("ID is required and cannot be zero")
	}
	
	if strings.TrimSpace(e.Type) == "" {
		return fmt.Errorf("Type is required")
	}
	
	if strings.TrimSpace(e.Title) == "" {
		return fmt.Errorf("Title is required")
	}
	
	if len(e.Title) > 256 {
		return fmt.Errorf("Title exceeds maximum length of 256 characters")
	}
	
	if strings.TrimSpace(e.User) == "" {
		return fmt.Errorf("User is required")
	}
	
	if strings.TrimSpace(e.Wiki) == "" {
		return fmt.Errorf("Wiki is required")
	}
	
	if strings.TrimSpace(e.ServerURL) == "" {
		return fmt.Errorf("ServerURL is required")
	}
	
	if e.Timestamp == 0 {
		return fmt.Errorf("Timestamp is required and cannot be zero")
	}
	
	// Check if timestamp is reasonable (not in the future, not too far in the past)
	// Wikipedia sends timestamps in seconds (Unix timestamp), not milliseconds
	now := time.Now().Unix()
	if e.Timestamp > now+3600 { // Allow up to 1 hour in the future to account for timezone differences
		return fmt.Errorf("Timestamp cannot be in the future")
	}
	
	// Check timestamp is not more than 1 day ago (should be recent changes)
	oneDayAgo := now - (24 * 60 * 60)
	if e.Timestamp < oneDayAgo {
		return fmt.Errorf("Timestamp is too far in the past")
	}
	
	if e.Length.Old < 0 || e.Length.New < 0 {
		return fmt.Errorf("Length values cannot be negative")
	}
	
	if e.Revision.Old < 0 || e.Revision.New < 0 {
		return fmt.Errorf("Revision values cannot be negative")
	}
	
	if len(e.Comment) > 500 {
		return fmt.Errorf("Comment exceeds maximum length of 500 characters")
	}
	
	return nil
}