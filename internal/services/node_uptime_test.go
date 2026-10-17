package services

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"testing"
	"time"

	"github.com/asdl/hub/internal/models"
)

func at(t time.Time) *time.Time { return &t }

func TestUptimePercent(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	from := now.Add(-24 * time.Hour)
	cases := []struct {
		name    string
		outages []models.NodeOutage
		want    float64
	}{
		{"no outages", nil, 100},
		{"6 of 24 hours down", []models.NodeOutage{{StartedAt: now.Add(-10 * time.Hour), EndedAt: at(now.Add(-4 * time.Hour))}}, 75},
		{"an open outage counts up to now", []models.NodeOutage{{StartedAt: now.Add(-12 * time.Hour)}}, 50},
		{"an outage from before the window only counts inside it", []models.NodeOutage{{StartedAt: now.Add(-48 * time.Hour), EndedAt: at(now.Add(-12 * time.Hour))}}, 50},
		{"offline for the whole window", []models.NodeOutage{{StartedAt: now.Add(-72 * time.Hour)}}, 0},
		{"two outages add up", []models.NodeOutage{
			{StartedAt: now.Add(-20 * time.Hour), EndedAt: at(now.Add(-18 * time.Hour))},
			{StartedAt: now.Add(-6 * time.Hour), EndedAt: at(now.Add(-4 * time.Hour))},
		}, 83.33},
		{"an outage entirely before the window is ignored", []models.NodeOutage{{StartedAt: now.Add(-60 * time.Hour), EndedAt: at(now.Add(-30 * time.Hour))}}, 100},
	}
	for _, c := range cases {
		if got := uptimePercent(c.outages, from, now); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if uptimePercent(nil, now, now) != 100 {
		t.Error("an empty window is 100% by convention")
	}
}

func TestOutageTrackingSince_IsStable(t *testing.T) {
	_, db := newNodeTestService(t)
	first := time.Now().Add(-time.Hour).Truncate(time.Second)
	if got := outageTrackingSince(db, first); !got.Equal(first) {
		t.Fatalf("the first call should start the clock at now, got %v", got)
	}
	if got := outageTrackingSince(db, first.Add(time.Hour)); !got.Equal(first) {
		t.Fatalf("later calls must return the original start, got %v", got)
	}
}

func TestUptimeFor_CountsFromWhenTrackingBegan(t *testing.T) {
	svc, db := newNodeTestService(t)
	now := time.Now()
	node := models.Node{ID: "n1", Hostname: "a", VPNIP: "10.100.0.2", Online: true, LastHeartbeat: now, CreatedAt: now.Add(-90 * 24 * time.Hour)}
	db.Create(&node)
	// Tracking began 10 hours ago; the node was down for the first 2 of them
	db.Create(&models.NodeOutage{NodeID: "n1", StartedAt: now.Add(-10 * time.Hour), EndedAt: at(now.Add(-8 * time.Hour)), Seconds: 7200})

	u := svc.uptimeFor(&node, now, now.Add(-10*time.Hour))
	if u.Uptime7d == nil || *u.Uptime7d != 80 || *u.Uptime30d != 80 {
		t.Fatalf("2h down of the 10h measured should be 80%%, got %+v", u)
	}
	if u.Outages7d != 1 || u.LastOutage == nil || u.LastOutage.Seconds != 7200 {
		t.Fatalf("outage summary is wrong: %+v", u)
	}
}

func TestUptimeFor_NodeThatNeverReportedHasNoUptime(t *testing.T) {
	svc, db := newNodeTestService(t)
	node := models.Node{ID: "n1", Hostname: "a", VPNIP: "10.100.0.2"}
	db.Create(&node)
	u := svc.uptimeFor(&node, time.Now(), time.Now().Add(-time.Hour))
	if u.Uptime7d != nil || u.Uptime30d != nil || u.LastOutage != nil {
		t.Fatalf("a node that never reported has no uptime: %+v", u)
	}
}

func TestUptimeEndpointAndDetails(t *testing.T) {
	svc, db := newNodeTestService(t)
	now := time.Now()
	db.Create(&models.Node{ID: "n1", Hostname: "laptop", VPNIP: "10.100.0.2", Online: false, LastHeartbeat: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour * 100)})
	db.Create(&models.NodeOutage{NodeID: "n1", StartedAt: now.Add(-time.Hour)})

	c, w := newTestContext(http.MethodGet, "/nodes/uptime", "")
	svc.Uptime(c)
	var list struct {
		TrackedSince time.Time    `json:"tracked_since"`
		Nodes        []NodeUptime `json:"nodes"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Nodes) != 1 {
		t.Fatalf("bad uptime response: %d %s", w.Code, w.Body.String())
	}
	if list.TrackedSince.IsZero() || list.Nodes[0].LastOutage == nil || list.Nodes[0].LastOutage.EndedAt != nil {
		t.Fatalf("the open outage should show: %+v", list)
	}

	c2, w2 := newTestContext(http.MethodGet, "/nodes/n1/details", "")
	c2.Params = append(c2.Params, gin.Param{Key: "id", Value: "n1"})
	svc.GetNodeDetails(c2)
	var details map[string]json.RawMessage
	if w2.Code != http.StatusOK || json.Unmarshal(w2.Body.Bytes(), &details) != nil {
		t.Fatalf("bad details response: %d %s", w2.Code, w2.Body.String())
	}
	for _, key := range []string{"node", "projects", "containers", "health_score", "uptime", "outages", "tracked_since"} {
		if _, ok := details[key]; !ok {
			t.Errorf("details response lacks %q (the old fields must stay, the new ones must be there)", key)
		}
	}
}
