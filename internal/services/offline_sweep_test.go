package services

import (
	"testing"
	"time"

	"github.com/asdl/hub/internal/models"
)

// Right after an update every node's last heartbeat is old; the sweeper must
// wait for fresh ones before marking anything offline.
func TestSweepOffline_WaitsAfterHubStart(t *testing.T) {
	svc, db := newNodeTestService(t)
	start := time.Now()
	db.Create(&models.Node{ID: "n1", Hostname: "a", VPNIP: "10.100.0.2", Online: true, LastHeartbeat: start.Add(-100 * time.Second)})

	online := func() bool {
		var n models.Node
		db.First(&n, "id = ?", "n1")
		return n.Online
	}

	// Heartbeat is 115s old (over the limit), but the Hub only just started.
	svc.sweepOffline(start.Add(15*time.Second), start)
	if !online() {
		t.Fatal("node was marked offline during the grace period after the Hub started")
	}

	// Grace is over and still no heartbeat: now it is offline.
	svc.sweepOffline(start.Add(offlineGrace+time.Second), start)
	if online() {
		t.Fatal("a node with no heartbeat should be offline once the grace period is over")
	}
}

func TestSweepOffline_KeepsNodesThatReport(t *testing.T) {
	svc, db := newNodeTestService(t)
	start := time.Now().Add(-time.Hour)
	now := time.Now()
	db.Create(&models.Node{ID: "fresh", Hostname: "fresh", VPNIP: "10.100.0.2", Online: true, LastHeartbeat: now.Add(-30 * time.Second)})
	db.Create(&models.Node{ID: "silent", Hostname: "silent", VPNIP: "10.100.0.3", Online: true, LastHeartbeat: now.Add(-3 * time.Minute)})

	svc.sweepOffline(now, start)

	var fresh, silent models.Node
	db.First(&fresh, "id = ?", "fresh")
	db.First(&silent, "id = ?", "silent")
	if !fresh.Online {
		t.Error("a node that sent a heartbeat 30s ago was marked offline")
	}
	if silent.Online {
		t.Error("a node silent for 3 minutes should be offline")
	}
}
