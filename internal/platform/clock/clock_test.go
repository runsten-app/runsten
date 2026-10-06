package clock

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)

func TestManualAdvance(t *testing.T) {
	m := NewManual(t0)
	m.Advance(90 * time.Second)
	if got, want := m.Now(), t0.Add(90*time.Second); !got.Equal(want) {
		t.Fatalf("Now() = %v, want %v", got, want)
	}
}

func TestAccelerated(t *testing.T) {
	tests := []struct {
		name    string
		factor  float64
		elapsed time.Duration
		want    time.Duration
	}{
		{"real time", 1, time.Minute, time.Minute},
		{"x60", 60, time.Minute, time.Hour},
		{"slowed down", 0.5, time.Minute, 30 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := NewManual(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
			epoch := t0
			c := NewAccelerated(base, epoch, tt.factor)
			if !c.Now().Equal(epoch) {
				t.Fatalf("at start Now() = %v, want %v", c.Now(), epoch)
			}
			base.Advance(tt.elapsed)
			if got, want := c.Now(), epoch.Add(tt.want); !got.Equal(want) {
				t.Fatalf("Now() = %v, want %v", got, want)
			}
		})
	}
}

func TestRealIsUTC(t *testing.T) {
	if loc := (Real{}).Now().Location(); loc != time.UTC {
		t.Fatalf("location = %v, want UTC", loc)
	}
}
