package services

import (
	"testing"
	"time"
)

func TestInStartupGrace(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"just deployed", now.Add(-10 * time.Second), true},
		{"two minutes in", now.Add(-2 * time.Minute), true},
		{"after the window", now.Add(-startupGrace - time.Second), false},
		{"long ago", now.Add(-24 * time.Hour), false},
		{"never deployed through the Hub", time.Time{}, false},
		{"clock skew (deployed in the future)", now.Add(5 * time.Minute), false},
	}
	for _, c := range cases {
		if got := inStartupGrace(c.at, now); got != c.want {
			t.Errorf("%s: inStartupGrace = %v, want %v", c.name, got, c.want)
		}
	}
}
