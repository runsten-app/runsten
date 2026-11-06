package bmwapi

import (
	"testing"
	"time"
)

// TestScaled: at 60 times real time, a device code lasts its 5 minutes of real time,
// which its client is told, the owner entering it in real time.
func TestScaled(t *testing.T) {
	if got := DefaultBehavior().Scaled(60).CodeLifetime; got != 5*time.Hour {
		t.Errorf("at ×60: %v", got)
	}
	if got := DefaultBehavior().Scaled(1).CodeLifetime; got != 5*time.Minute {
		t.Errorf("at ×1: %v", got)
	}
}
