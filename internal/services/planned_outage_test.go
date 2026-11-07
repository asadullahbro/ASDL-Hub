package services

import (
	"net/http"
	"testing"
	"time"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

// An agent that says it is going away (someone stopped it) leaves a planned outage behind, and planned outages
// don't lower the node's placement score the way real drop-outs do.
func TestPlannedOutage_DoesNotCountAgainstTheNode(t *testing.T) {
	db := testutil.NewDB(t, &models.Node{}, &models.NodeOutage{}, &models.Project{}, &models.Setting{})
	svc := NewNodeService(db)
	last := time.Now().Add(-2 * time.Minute)
	db.Create(&models.Node{ID: "n1", Hostname: "laptop", VPNIP: "10.100.0.2", Online: true, LastHeartbeat: last, HealthScore: 80})
	db.Create(&models.Node{ID: "n2", Hostname: "pc", VPNIP: "10.100.0.3", Online: true, LastHeartbeat: time.Now(), HealthScore: 80})

	c, w := newTestContext(http.MethodPost, "/nodes/self/going-away", "")
	c.Set("vpn_ip", "10.100.0.2")
	svc.GoingAway(c)
	if w.Code != http.StatusOK {
		t.Fatalf("going-away answered %d", w.Code)
	}
	// The agent said goodbye seconds after its last heartbeat.
	db.Model(&models.Node{}).Where("id = ?", "n1").Update("going_away_at", last.Add(5*time.Second))

	svc.sweepOffline(time.Now(), time.Now().Add(-time.Hour))
	var o models.NodeOutage
	if err := db.First(&o, "node_id = ?", "n1").Error; err != nil || !o.Planned {
		t.Fatalf("outage after going away: %+v (%v), want planned", o, err)
	}

	// A real drop-out on the other node is not planned, and only it is held against its node.
	db.Create(&models.NodeOutage{NodeID: "n2", StartedAt: time.Now().Add(-time.Hour)})
	cands := placementCandidates(db, &models.Project{ID: "p", NodeID: "elsewhere"}, nil, time.Now())
	for _, c := range cands {
		if c.Node.ID == "n2" && c.OutagesToday != 1 {
			t.Errorf("pc's unplanned outage counted %d times, want 1", c.OutagesToday)
		}
	}
	db.Model(&models.Node{}).Where("id = ?", "n1").Update("online", true)
	for _, c := range placementCandidates(db, &models.Project{ID: "p", NodeID: "elsewhere"}, nil, time.Now()) {
		if c.Node.ID == "n1" && c.OutagesToday != 0 {
			t.Errorf("laptop's planned outage counted against it (%d)", c.OutagesToday)
		}
	}
}

// A goodbye long before the node went quiet (an agent that said it was stopping but kept running) doesn't make
// a later, real outage planned.
func TestPlannedOutage_StaleGoodbyeIgnored(t *testing.T) {
	long := time.Now().Add(-3 * time.Hour)
	n := &models.Node{LastHeartbeat: time.Now().Add(-2 * time.Minute), GoingAwayAt: &long}
	if wentAwayOnPurpose(n) {
		t.Error("a goodbye from hours before counted")
	}
	soon := n.LastHeartbeat.Add(3 * time.Second)
	n.GoingAwayAt = &soon
	if !wentAwayOnPurpose(n) {
		t.Error("a goodbye right after the last heartbeat didn't count")
	}
}
