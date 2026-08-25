package pulselisteners

import (
	"testing"
	"time"
)

func TestBuildCreateTimeSeriesRequestInterval(t *testing.T) {
	base := time.Date(2026, 8, 24, 3, 17, 0, 0, time.UTC)

	tests := []struct {
		name     string
		earliest time.Time
		latest   time.Time
		wantEnd  int64
	}{
		{
			name:     "normal interval",
			earliest: base,
			latest:   base.Add(31 * time.Second),
			wantEnd:  base.Unix() + 31,
		},
		{
			name:     "same second gets widened",
			earliest: base,
			latest:   base.Add(500 * time.Millisecond),
			wantEnd:  base.Unix() + 1,
		},
		{
			name:     "latest before earliest gets widened",
			earliest: base,
			latest:   base.Add(-2 * time.Second),
			wantEnd:  base.Unix() + 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := buildCreateTimeSeriesRequest("test-project", tt.earliest, tt.latest, 3)
			interval := req.TimeSeries[0].Points[0].Interval
			if interval.StartTime.Seconds != tt.earliest.Unix() {
				t.Errorf("start = %d, want %d", interval.StartTime.Seconds, tt.earliest.Unix())
			}
			if interval.EndTime.Seconds != tt.wantEnd {
				t.Errorf("end = %d, want %d", interval.EndTime.Seconds, tt.wantEnd)
			}
			if interval.EndTime.Seconds <= interval.StartTime.Seconds {
				t.Errorf("end (%d) must be after start (%d)", interval.EndTime.Seconds, interval.StartTime.Seconds)
			}
			got := req.TimeSeries[0].Points[0].Value.GetDoubleValue()
			if got < 0.3-1e-9 || got > 0.3+1e-9 {
				t.Errorf("value = %v, want ~0.3", got)
			}
		})
	}
}
