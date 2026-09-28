package incident

import (
	"testing"
	"time"
)

func TestCorrelatesWithinWindow(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if !Correlates(base, base.Add(2*time.Minute)) {
		t.Fatal("expected incidents 2 minutes apart to correlate")
	}
	if !Correlates(base, base.Add(-2*time.Minute)) {
		t.Fatal("expected correlation to be symmetric regardless of order")
	}
}

func TestCorrelatesOutsideWindow(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if Correlates(base, base.Add(10*time.Minute)) {
		t.Fatal("expected incidents 10 minutes apart not to correlate")
	}
}
