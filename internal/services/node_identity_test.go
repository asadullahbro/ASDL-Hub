package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

// Two nodes, and the mesh routes with the caller's address supplied by a test
// header the way VPNOnly supplies it in production. What a node sends (an ID
// in the URL or query) must never decide which node it is.
type identityFixture struct {
	db     *gorm.DB
	router *gin.Engine
	a, b   models.Node
}

func newIdentityFixture(t *testing.T) *identityFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := testutil.NewDB(t, &models.Node{}, &models.Heartbeat{}, &models.Job{}, &models.Project{}, &models.Migration{})
	f := &identityFixture{
		db: db,
		a:  models.Node{ID: "node-a", Hostname: "a", VPNIP: "10.100.0.2"},
		b:  models.Node{ID: "node-b", Hostname: "b", VPNIP: "10.100.0.3"},
	}
	db.Create(&f.a)
	db.Create(&f.b)

	nodes, jobs, ops := NewNodeService(db), NewJobService(db), NewNodeOpsService(db, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if addr := c.GetHeader("X-Test-Addr"); addr != "" {
			c.Set("vpn_ip", addr)
		}
	})
	r.POST("/nodes/:id/heartbeat", nodes.Heartbeat)
	r.POST("/nodes/:id/maintenance", ops.SetMaintenanceFromNode)
	r.POST("/jobs/claim", jobs.Claim)
	f.router = r
	return f
}

func (f *identityFixture) post(path, addr, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if addr != "" {
		req.Header.Set("X-Test-Addr", addr)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func (f *identityFixture) node(id string) models.Node {
	var n models.Node
	f.db.First(&n, "id = ?", id)
	return n
}

func TestHeartbeatIsFromTheCallerNotTheURL(t *testing.T) {
	f := newIdentityFixture(t)

	// Node A reports, with node B's ID in the URL: A is updated, B is not.
	if w := f.post("/nodes/node-b/heartbeat", "10.100.0.2", `{"uptime":777}`); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if got := f.node("node-a").Uptime; got != 777 {
		t.Errorf("node A uptime = %d, want 777", got)
	}
	if got := f.node("node-b").Uptime; got != 0 {
		t.Errorf("node B was reported on by node A: uptime = %d", got)
	}

	// New agents send "self"; old ones send their saved ID. Both work.
	for _, id := range []string{"self", "node-a", "anything-at-all"} {
		if w := f.post("/nodes/"+id+"/heartbeat", "10.100.0.2", `{"uptime":5}`); w.Code != http.StatusOK {
			t.Errorf("id %q: status %d", id, w.Code)
		}
	}

	// An address that belongs to no node is refused, whatever ID it sends.
	if w := f.post("/nodes/node-b/heartbeat", "10.100.0.99", `{"uptime":1}`); w.Code != http.StatusNotFound {
		t.Errorf("unknown address: status %d, want 404", w.Code)
	}
	if got := f.node("node-b").Uptime; got != 0 {
		t.Errorf("node B was reported on from an unknown address: uptime = %d", got)
	}
}

func TestClaimGivesOnlyTheCallersOwnJobs(t *testing.T) {
	f := newIdentityFixture(t)
	f.db.Create(&models.Job{ID: "job-for-b", NodeID: "node-b", Type: "command", Command: "echo b",
		Status: models.JobStatusPending, CreatedAt: time.Now()})

	// Node A names node B in the query: it gets nothing, and B's job stays put.
	if w := f.post("/jobs/claim?node_id=node-b", "10.100.0.2", ""); w.Code != http.StatusNoContent {
		t.Errorf("node A claiming with node B's ID: status %d (%s), want 204", w.Code, w.Body.String())
	}
	var job models.Job
	f.db.First(&job, "id = ?", "job-for-b")
	if job.Status != models.JobStatusPending {
		t.Fatalf("node B's job was taken by node A: status %q", job.Status)
	}

	// An address that belongs to no node can't claim anything either.
	if w := f.post("/jobs/claim?node_id=node-b", "10.100.0.99", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown address: status %d, want 404", w.Code)
	}

	// Node B, with no ID at all (new agents) or its old one, gets its job.
	w := f.post("/jobs/claim", "10.100.0.3", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "job-for-b") {
		t.Errorf("node B claiming its own job: %d %s", w.Code, w.Body.String())
	}
	f.db.First(&job, "id = ?", "job-for-b")
	if job.Status != models.JobStatusRunning {
		t.Errorf("job status after B claimed it: %q, want running", job.Status)
	}
}

func TestMaintenanceFromNodeChangesOnlyTheCaller(t *testing.T) {
	f := newIdentityFixture(t)

	// Node A asks, with node B's ID in the URL: A goes into maintenance, B doesn't.
	if w := f.post("/nodes/node-b/maintenance", "10.100.0.2", `{"enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !f.node("node-a").Maintenance {
		t.Error("node A should be in maintenance")
	}
	if f.node("node-b").Maintenance {
		t.Error("node B was put into maintenance by node A")
	}

	// "self", as new agents send it, works the same.
	if w := f.post("/nodes/self/maintenance", "10.100.0.2", `{"enabled":false}`); w.Code != http.StatusOK {
		t.Fatalf("self: status %d: %s", w.Code, w.Body.String())
	}
	if f.node("node-a").Maintenance {
		t.Error("node A should be out of maintenance")
	}

	// Unknown address: refused.
	if w := f.post("/nodes/node-b/maintenance", "10.100.0.99", `{"enabled":true}`); w.Code != http.StatusForbidden {
		t.Errorf("unknown address: status %d, want 403", w.Code)
	}
	if f.node("node-b").Maintenance {
		t.Error("node B was put into maintenance from an unknown address")
	}
}

func TestNodeByAddressNeedsAnAddress(t *testing.T) {
	f := newIdentityFixture(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if _, err := nodeByAddress(f.db, c); err == nil {
		t.Error("a request with no address must not identify a node")
	}
}
