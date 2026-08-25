package watermeter

import (
	"testing"
	"time"
)

func TestShouldAlertForStall(t *testing.T) {
	now := time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)
	threshold := 12 * time.Hour
	realert := 24 * time.Hour

	tests := []struct {
		name        string
		lastPulse   time.Time
		lastAlerted time.Time
		want        bool
	}{
		{
			name:      "recent pulse, no alert",
			lastPulse: now.Add(-1 * time.Hour),
			want:      false,
		},
		{
			name:      "just under threshold, no alert",
			lastPulse: now.Add(-threshold + time.Minute),
			want:      false,
		},
		{
			name:      "past threshold, first alert",
			lastPulse: now.Add(-threshold - time.Minute),
			want:      true,
		},
		{
			name:        "already alerted recently, no re-alert",
			lastPulse:   now.Add(-20 * time.Hour),
			lastAlerted: now.Add(-6 * time.Hour),
			want:        false,
		},
		{
			name:        "still stalled a day after last alert, re-alert",
			lastPulse:   now.Add(-40 * time.Hour),
			lastAlerted: now.Add(-realert - time.Minute),
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldAlertForStall(tt.lastPulse, tt.lastAlerted, now, threshold, realert)
			if got != tt.want {
				t.Errorf("shouldAlertForStall() = %v, want %v", got, tt.want)
			}
		})
	}
}
