package services

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/asdl/hub/internal/models"
)

func cand(name string, health int, uptime float64, outagesToday int) placementCandidate {
	return placementCandidate{Node: models.Node{ID: name, Hostname: name, HealthScore: health}, Uptime7d: uptime, OutagesToday: outagesToday}
}

func names(c []placementCandidate) []string {
	out := make([]string, len(c))
	for i := range c {
		out[i] = c[i].Node.Hostname
	}
	return out
}

func sameOrder(t *testing.T, got []placementCandidate, want ...string) {
	t.Helper()
	g := names(got)
	if len(g) != len(want) {
		t.Fatalf("got %v, want %v", g, want)
	}
	for i := range g {
		if g[i] != want[i] {
			t.Fatalf("got %v, want %v", g, want)
		}
	}
}

func TestRank_ReliableBeatsHealthyButFlaky(t *testing.T) {
	// "laptop" looks great right now (health 95) but has been up only 85% of the week.
	sameOrder(t, rankPlacements([]placementCandidate{cand("laptop", 95, 85, 0), cand("tower", 80, 99.9, 0)}), "tower", "laptop")
}

func TestRank_RecentOutagesCount(t *testing.T) {
	sameOrder(t, rankPlacements([]placementCandidate{cand("a", 90, 99, 2), cand("b", 85, 99, 0)}), "b", "a")
}

func TestRank_UnknownUptimeIsJudgedOnHealthAlone(t *testing.T) {
	sameOrder(t, rankPlacements([]placementCandidate{cand("new", 70, -1, 0), cand("old", 90, 60, 0)}), "new", "old")
	sameOrder(t, rankPlacements([]placementCandidate{cand("new", 90, -1, 0), cand("old", 70, 99, 0)}), "new", "old")
}

func TestRank_NodesWithRoomComeFirst(t *testing.T) {
	full := cand("full", 99, 100, 0)
	full.Node.MemoryTotal, full.Node.MemoryUsed = 16<<30, 15<<30 // 6% free
	roomy := cand("roomy", 50, 90, 0)
	roomy.Node.MemoryTotal, roomy.Node.MemoryUsed = 16<<30, 4<<30
	sameOrder(t, rankPlacements([]placementCandidate{full, roomy}), "roomy", "full")

	// With no node that has room, the best of the rest is still chosen rather than nothing.
	full2 := cand("full2", 60, 90, 0)
	full2.Node.DiskTotal, full2.Node.DiskUsed = 100<<30, 99<<30
	sameOrder(t, rankPlacements([]placementCandidate{full2, full}), "full", "full2")
}

func TestRank_MasterFirstAmongNodesWithRoom(t *testing.T) {
	m := cand("master", 60, 90, 0)
	m.IsMaster = true
	sameOrder(t, rankPlacements([]placementCandidate{cand("better", 99, 100, 0), m}), "master", "better")

	m.Node.DiskTotal, m.Node.DiskUsed = 100<<30, 99<<30 // master has no room: it loses its priority
	sameOrder(t, rankPlacements([]placementCandidate{cand("better", 99, 100, 0), m}), "better", "master")
}

func TestRank_TiesAreOrderedByNameAndInputIsUntouched(t *testing.T) {
	in := []placementCandidate{cand("b", 80, 99, 0), cand("a", 80, 99, 0)}
	sameOrder(t, rankPlacements(in), "a", "b")
	sameOrder(t, in, "b", "a")
}

func TestFailover_AvoidsANodeThatKeepsDropping(t *testing.T) {
	hs, _, db := newFailoverEnv(t)
	if err := db.AutoMigrate(&models.NodeOutage{}); err != nil {
		t.Fatal(err)
	}
	// "good" (health 90) went down three times today; "ok" (health 60) has been steady.
	now := time.Now()
	for i := 0; i < 3; i++ {
		end := now.Add(-time.Duration(i+1) * time.Hour)
		db.Create(&models.NodeOutage{NodeID: "good", StartedAt: end.Add(-10 * time.Minute), EndedAt: &end, Seconds: 600})
	}
	db.Save(&models.Setting{Key: settingTrackedSince, Value: now.Add(-7 * 24 * time.Hour).UTC().Format(time.RFC3339)})

	p := loadProject(db)
	target := hs.failoverTarget(&p)
	if target == nil || target.ID != "ok" {
		t.Fatalf("should pick the steady node, got %+v", target)
	}
}

func TestGetPlacement_ListsOptionsBestFirst(t *testing.T) {
	hs, _, db := newFailoverEnv(t)
	if err := db.AutoMigrate(&models.NodeOutage{}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/projects/p1/placement", nil)
	c.Params = gin.Params{{Key: "id", Value: "p1"}}
	hs.GetPlacement(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"node_id":"dead"`, `"recommended":true`, `"hostname":"good"`, `"hostname":"ok"`} {
		if want == `"node_id":"dead"` && !contains(body, want) {
			t.Errorf("response should name the node the project is on: %s", body)
		} else if want != `"node_id":"dead"` && !contains(body, want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
	if contains(body, `"hostname":"dead"`) {
		t.Errorf("the offline node must not be offered: %s", body)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
