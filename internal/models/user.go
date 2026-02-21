package models

import (
	"time"
)

// DigestFrequency controls how often a user receives digest emails.
type DigestFrequency string

const (
	DigestFreqNone   DigestFrequency = "none"
	DigestFreqDaily  DigestFrequency = "daily"
	DigestFreqWeekly DigestFrequency = "weekly"
	DigestFreqBoth   DigestFrequency = "both"
)

// DigestContent controls what sections appear in the digest email.
type DigestContent string

const (
	DigestContentAll       DigestContent = "both"
	DigestContentWatchlist DigestContent = "watchlist"
	DigestContentGlobal    DigestContent = "global"
)

// User represents an authenticated WikiSurge user with digest preferences.
type User struct {
	ID             string          `json:"id"`
	Email          string          `json:"email"`
	PasswordHash   string          `json:"-"` // never exposed in JSON
	Watchlist      []string        `json:"watchlist"`
	DigestFreq     DigestFrequency `json:"digest_frequency"`
	DigestContent  DigestContent   `json:"digest_content"`
	SpikeThreshold float64         `json:"spike_threshold"` // Nx multiplier — only email if activity exceeds this
	UnsubToken     string          `json:"-"`                // one-click unsubscribe token
	Verified       bool            `json:"verified"`
	CreatedAt      time.Time       `json:"created_at"`
	LastDigestAt   time.Time       `json:"last_digest_at"`
}

// DigestPreferences is the subset of User that controls digest behavior.
// Used for API updates so users can't accidentally overwrite other fields.
type DigestPreferences struct {
	DigestFreq     DigestFrequency `json:"digest_frequency"`
	DigestContent  DigestContent   `json:"digest_content"`
	SpikeThreshold float64         `json:"spike_threshold"`
}

// Validate checks that digest preferences are within allowed values.
func (p *DigestPreferences) Validate() string {
	switch p.DigestFreq {
	case DigestFreqNone, DigestFreqDaily, DigestFreqWeekly, DigestFreqBoth:
		// OK
	default:
		return "digest_frequency must be one of: none, daily, weekly, both"
	}

	switch p.DigestContent {
	case DigestContentAll, DigestContentWatchlist, DigestContentGlobal:
		// OK
	default:
		return "digest_content must be one of: both, watchlist, global"
	}

	if p.SpikeThreshold < 0 || p.SpikeThreshold > 100 {
		return "spike_threshold must be between 0 and 100"
	}

	return ""
}
