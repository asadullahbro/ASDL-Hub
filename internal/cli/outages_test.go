package cli

import (
	"strings"
	"testing"
	"time"
)

func startsAt(hour int) outageRow {
	return outageRow{StartedAt: time.Date(2026, 10, 1, hour, 30, 0, 0, time.UTC)}
}

func TestOutagePattern(t *testing.T) {
	if got := outagePattern([]outageRow{startsAt(2), startsAt(2), startsAt(3)}, time.UTC); got != "" {
		t.Errorf("three outages are too few to call a pattern, got %q", got)
	}
	clustered := []outageRow{startsAt(2), startsAt(3), startsAt(3), startsAt(4), startsAt(14)}
	if got := outagePattern(clustered, time.UTC); got != "4 of 5 outages began between 02:00 and 05:00" {
		t.Errorf("got %q", got)
	}
	spread := []outageRow{startsAt(1), startsAt(6), startsAt(11), startsAt(16), startsAt(21)}
	if got := outagePattern(spread, time.UTC); got != "" {
		t.Errorf("evenly spread outages have no pattern, got %q", got)
	}
	// A cluster that straddles midnight is still one cluster.
	overnight := []outageRow{startsAt(23), startsAt(23), startsAt(0), startsAt(1), startsAt(12)}
	if got := outagePattern(overnight, time.UTC); got != "4 of 5 outages began between 23:00 and 02:00" {
		t.Errorf("got %q", got)
	}
}

func TestOutagePattern_UsesTheGivenTimezone(t *testing.T) {
	pk := time.FixedZone("PKT", 5*3600)
	rows := []outageRow{startsAt(21), startsAt(21), startsAt(22), startsAt(21)} // 02:30-03:30 in PKT
	if got := outagePattern(rows, pk); !strings.Contains(got, "between 02:00 and 05:00") {
		t.Errorf("hours should be counted in the local zone, got %q", got)
	}
}

func TestCauseSummary(t *testing.T) {
	end := time.Now()
	rows := []outageRow{
		{EndedAt: &end, Cause: "weak_link"}, {EndedAt: &end, Cause: "reboot"}, {EndedAt: &end, Cause: "weak_link"},
		{EndedAt: &end}, {Cause: "reboot"}, // no cause recorded, and still ongoing: neither counts
	}
	if got := causeSummary(rows); got != "2 weak link, 1 restarted" {
		t.Errorf("got %q", got)
	}
}
