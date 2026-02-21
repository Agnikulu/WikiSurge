package models

import "testing"

func TestDigestPreferencesValidate(t *testing.T) {
	tests := []struct {
		name    string
		prefs   DigestPreferences
		wantErr bool
	}{
		{
			name:    "valid daily + both",
			prefs:   DigestPreferences{DigestFreq: DigestFreqDaily, DigestContent: DigestContentAll, SpikeThreshold: 2.0},
			wantErr: false,
		},
		{
			name:    "valid weekly + watchlist",
			prefs:   DigestPreferences{DigestFreq: DigestFreqWeekly, DigestContent: DigestContentWatchlist, SpikeThreshold: 5.0},
			wantErr: false,
		},
		{
			name:    "valid none",
			prefs:   DigestPreferences{DigestFreq: DigestFreqNone, DigestContent: DigestContentGlobal, SpikeThreshold: 0},
			wantErr: false,
		},
		{
			name:    "valid both freq",
			prefs:   DigestPreferences{DigestFreq: DigestFreqBoth, DigestContent: DigestContentAll, SpikeThreshold: 10},
			wantErr: false,
		},
		{
			name:    "invalid frequency",
			prefs:   DigestPreferences{DigestFreq: "hourly", DigestContent: DigestContentAll, SpikeThreshold: 2.0},
			wantErr: true,
		},
		{
			name:    "invalid content",
			prefs:   DigestPreferences{DigestFreq: DigestFreqDaily, DigestContent: "everything", SpikeThreshold: 2.0},
			wantErr: true,
		},
		{
			name:    "negative threshold",
			prefs:   DigestPreferences{DigestFreq: DigestFreqDaily, DigestContent: DigestContentAll, SpikeThreshold: -1},
			wantErr: true,
		},
		{
			name:    "threshold too high",
			prefs:   DigestPreferences{DigestFreq: DigestFreqDaily, DigestContent: DigestContentAll, SpikeThreshold: 101},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errMsg := tt.prefs.Validate()
			if tt.wantErr && errMsg == "" {
				t.Error("expected validation error but got none")
			}
			if !tt.wantErr && errMsg != "" {
				t.Errorf("unexpected validation error: %s", errMsg)
			}
		})
	}
}
