package services

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

const gib = int64(1) << 30

func TestRecommendedParallelJobs(t *testing.T) {
	cases := []struct {
		cpus int
		mem  int64
		want int
	}{
		{8, 15 * gib, 3},  // laptop: memory decides
		{12, 31 * gib, 4}, // tower: capped at 4
		{4, 31 * gib, 2},  // few cores
		{4, 7 * gib, 1},   // small machine
		{2, 2 * gib, 1},   // never below 1
		{0, 0, 4},         // nothing reported: no limit from hardware beyond the cap
	}
	for _, c := range cases {
		if got := models.RecommendedParallelJobs(c.cpus, c.mem); got != c.want {
			t.Errorf("RecommendedParallelJobs(%d cores, %d GB) = %d, want %d", c.cpus, c.mem/gib, got, c.want)
		}
	}
}

// A node whose agent can run 3 jobs at once, with plenty of room.
func parallelNode(db *gorm.DB) *models.Node {
	n := &models.Node{ID: "n1", Hostname: "laptop", VPNIP: "10.100.0.2", Online: true, CPU: 8,
		MemoryTotal: 16 * gib, MemoryUsed: 4 * gib, ParallelJobs: 3,
		Capabilities: []string{"docker", models.ParallelJobsCapability}}
	db.Create(n)
	return n
}

func pendingJob(db *gorm.DB, id, scope string, age time.Duration) {
	db.Create(&models.Job{ID: id, NodeID: "n1", Type: models.JobTypeDeploy, Status: models.JobStatusPending,
		Scope: scope, CreatedAt: time.Now().Add(-age)})
}

func claimAll(svc *JobService, node *models.Node) []string {
	var got []string
	for {
		j := svc.nextJob(node, time.Now())
		if j == nil {
			return got
		}
		got = append(got, j.ID)
	}
}

// Four apps failing over to one node: with a limit of 3 the first three start together, never two for one app.
func TestNextJob_RunsDifferentAppsTogetherUpToTheLimit(t *testing.T) {
	svc, db := newJobTestService(t)
	node := parallelNode(db)
	pendingJob(db, "a1", "app:a", 5*time.Minute)
	pendingJob(db, "a2", "app:a", 4*time.Minute) // second job for app a: waits for a1
	pendingJob(db, "b1", "app:b", 3*time.Minute)
	pendingJob(db, "c1", "app:c", 2*time.Minute)
	pendingJob(db, "d1", "app:d", 1*time.Minute)

	got := claimAll(svc, node)
	want := []string{"a1", "b1", "c1"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("claimed %v, want %v", got, want)
	}

	// a1 finishes: a2 may go now, before d1 (it is older).
	db.Model(&models.Job{}).Where("id = ?", "a1").Update("status", models.JobStatusCompleted)
	if j := svc.nextJob(node, time.Now()); j == nil || j.ID != "a2" {
		t.Errorf("after a1 finished, claimed %v, want a2", j)
	}
}

func TestNextJob_OldAgentRunsOneAtATime(t *testing.T) {
	svc, db := newJobTestService(t)
	node := parallelNode(db)
	node.Capabilities = []string{"docker"}
	pendingJob(db, "a1", "app:a", 2*time.Minute)
	pendingJob(db, "b1", "app:b", time.Minute)
	if got := claimAll(svc, node); len(got) != 1 || got[0] != "a1" {
		t.Errorf("an agent without parallel_jobs got %v, want only a1", got)
	}
}

// Agent updates and other unscoped jobs run alone: they wait for an idle node, nothing overtakes them, and nothing
// starts beside them.
func TestNextJob_UnscopedJobRunsAlone(t *testing.T) {
	svc, db := newJobTestService(t)
	node := parallelNode(db)
	pendingJob(db, "a1", "app:a", 3*time.Minute)
	pendingJob(db, "upd", "", 2*time.Minute)
	pendingJob(db, "b1", "app:b", time.Minute)

	if got := claimAll(svc, node); len(got) != 1 || got[0] != "a1" {
		t.Fatalf("claimed %v, want only a1 (the update waits, b1 may not overtake it)", got)
	}
	db.Model(&models.Job{}).Where("id = ?", "a1").Update("status", models.JobStatusCompleted)
	if got := claimAll(svc, node); len(got) != 1 || got[0] != "upd" {
		t.Fatalf("on an idle node claimed %v, want only the update", got)
	}
}

func TestNextJob_NoExtraJobWithoutFreeMemory(t *testing.T) {
	svc, db := newJobTestService(t)
	node := parallelNode(db)
	node.MemoryUsed = node.MemoryTotal - gib // 1 GB free
	pendingJob(db, "a1", "app:a", 2*time.Minute)
	pendingJob(db, "b1", "app:b", time.Minute)
	if got := claimAll(svc, node); len(got) != 1 {
		t.Errorf("with 1 GB free claimed %v, want just the first", got)
	}
}

// Jobs left "running" by an agent process that has since restarted don't take up the node's slots.
func TestNextJob_IgnoresJobsFromBeforeTheAgentRestarted(t *testing.T) {
	svc, db := newJobTestService(t)
	node := parallelNode(db)
	node.ParallelJobs = 1
	old := time.Now().Add(-10 * time.Minute)
	db.Create(&models.Job{ID: "orphan", NodeID: "n1", Type: models.JobTypeDeploy, Status: models.JobStatusRunning,
		Scope: "app:x", StartedAt: &old})
	started := time.Now().Add(-time.Minute)
	node.AgentStartedAt = &started
	pendingJob(db, "a1", "app:a", time.Minute)
	if j := svc.nextJob(node, time.Now()); j == nil || j.ID != "a1" {
		t.Errorf("an orphaned job blocked the queue: claimed %v", j)
	}
}

func TestJobsInFlight(t *testing.T) {
	_, db := newJobTestService(t)
	parallelNode(db)
	db.Create(&models.Node{ID: "off", Hostname: "off", VPNIP: "10.100.0.9", Online: false})
	pendingJob(db, "a1", "app:a", time.Minute)
	db.Create(&models.Job{ID: "r1", NodeID: "n1", Type: models.JobTypeDeploy, Status: models.JobStatusRunning})
	db.Create(&models.Job{ID: "logs", NodeID: "n1", Type: models.JobTypeLogsFollow, Status: models.JobStatusRunning})
	db.Create(&models.Job{ID: "done", NodeID: "n1", Type: models.JobTypeDeploy, Status: models.JobStatusCompleted})
	db.Create(&models.Job{ID: "away", NodeID: "off", Type: models.JobTypeDeploy, Status: models.JobStatusPending})
	if n := JobsInFlight(db); n != 2 {
		t.Errorf("JobsInFlight = %d, want 2 (a1 and r1)", n)
	}
}

// An agent retries a report whose answer it didn't get. The repeat must not be acted on twice.
func TestJobComplete_RepeatIsIgnored(t *testing.T) {
	svc, db := newJobTestService(t)
	db.Create(&models.Node{ID: "n1", Hostname: "mac-mini", VPNIP: "10.100.0.2"})
	db.Create(&models.Job{ID: "j1", NodeID: "n1", Type: models.JobTypeCommand, Status: models.JobStatusRunning})
	complete := func(body string) string {
		c, w := newTestContext(http.MethodPost, "/jobs/j1/complete", body)
		c.Params = gin.Params{{Key: "id", Value: "j1"}}
		c.Set("vpn_ip", "10.100.0.2")
		svc.Complete(c)
		return w.Body.String()
	}
	complete(`{"status":"completed","logs":"first"}`)
	if body := complete(`{"status":"completed","logs":"second"}`); !strings.Contains(body, "duplicate") {
		t.Errorf("repeat report answered %s, want it marked duplicate", body)
	}
	var j models.Job
	db.First(&j, "id = ?", "j1")
	if j.Logs != "first" {
		t.Errorf("the repeat overwrote the job: logs %q", j.Logs)
	}
}

func TestUpdate_WaitsForJobsThenUpgrades(t *testing.T) {
	inFlight := 2
	defer func(f func() bool) { canSelfUpdate = f }(canSelfUpdate)
	canSelfUpdate = func() bool { return true }
	s := NewUpdateService("v1.0.0")
	s.latest = "v1.1.0"
	s.SetInFlightJobs(func() int { return inFlight })

	c, w := newTestContext(http.MethodPost, "/system/update", "")
	s.Update(c)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"waiting_for_jobs":2`) {
		t.Fatalf("update with jobs running answered %d %s, want 202 waiting for 2", w.Code, w.Body.String())
	}
	s.mu.RLock()
	waiting := s.waitingFor
	s.mu.RUnlock()
	if waiting != 2 {
		t.Errorf("waitingFor = %d, want 2", waiting)
	}
}
