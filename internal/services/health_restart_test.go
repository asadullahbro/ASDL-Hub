package services

import (
	"testing"
	"time"
)

func TestRestartAllowed(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if ok, _ := restartAllowed(nil, now); !ok {
		t.Fatal("first restart should be allowed")
	}
	two := []time.Time{now.Add(-5 * time.Minute), now.Add(-1 * time.Minute)}
	if ok, recent := restartAllowed(two, now); ok || len(recent) != 2 {
		t.Fatalf("two recent restarts should block the third, got ok=%v recent=%d", ok, len(recent))
	}
	old := []time.Time{now.Add(-2 * time.Hour), now.Add(-restartWindow - time.Second), now.Add(-time.Minute)}
	ok, recent := restartAllowed(old, now)
	if !ok || len(recent) != 1 {
		t.Fatalf("restarts outside the window must not count, got ok=%v recent=%d", ok, len(recent))
	}
}
