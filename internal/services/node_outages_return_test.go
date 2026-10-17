package services

import (
	"net/http"
	"testing"
	"time"

	"github.com/asdl/hub/internal/models"
)

func outageFor(t *testing.T, svc *NodeService, nodeID string) models.NodeOutage {
	t.Helper()
	var o models.NodeOutage
	if err := svc.db.Where("node_id = ?", nodeID).First(&o).Error; err != nil {
		t.Fatalf("no outage for %s: %v", nodeID, err)
	}
	return o
}

// An agent that restarts registers again instead of sending a heartbeat; that must end the outage too.
func TestOutage_EndsWhenTheNodeRegistersAgain(t *testing.T) {
	svc, db := newNodeTestService(t)
	gone := time.Now().Add(-3 * 24 * time.Hour)
	db.Create(&models.Node{ID: "pc", Hostname: "pc", VPNIP: "10.100.0.3", Online: false, LastHeartbeat: gone})
	db.Create(&models.NodeOutage{NodeID: "pc", StartedAt: gone})

	c, w := newTestContext(http.MethodPost, "/nodes", `{"hostname":"pc","os":"linux","cpu":4}`)
	c.Set("vpn_ip", "10.100.0.3")
	svc.Register(c)
	if w.Code != http.StatusOK {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	o := outageFor(t, svc, "pc")
	if o.EndedAt == nil || o.Seconds < int64(3*24*time.Hour.Seconds())-5 {
		t.Fatalf("registering should end the 3 day outage and record its length: %+v", o)
	}
}

func TestOutage_SafetyNetClosesOutagesOfNodesThatAreOnline(t *testing.T) {
	svc, db := newNodeTestService(t)
	now := time.Now()
	start := now.Add(-time.Hour)
	back := now.Add(-10 * time.Minute)
	db.Create(&models.Node{ID: "up", Hostname: "up", VPNIP: "10.100.0.2", Online: true, LastHeartbeat: back})
	db.Create(&models.Node{ID: "down", Hostname: "down", VPNIP: "10.100.0.3", Online: false, LastHeartbeat: start})
	db.Create(&models.NodeOutage{NodeID: "up", StartedAt: start})
	db.Create(&models.NodeOutage{NodeID: "down", StartedAt: start})

	svc.sweepOffline(now, now) // inside the startup grace: the safety net still runs

	up, down := outageFor(t, svc, "up"), outageFor(t, svc, "down")
	if up.EndedAt == nil || !up.EndedAt.Equal(back) || up.Seconds != int64(back.Sub(start).Seconds()) {
		t.Fatalf("an outage of an online node should end at its last heartbeat: %+v", up)
	}
	if down.EndedAt != nil {
		t.Fatalf("an outage of a node that is still offline must stay open: %+v", down)
	}
}
