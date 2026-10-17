package services

import (
	"testing"
	"time"

	"github.com/asdl/hub/internal/models"
)

// A project on a node that is online, with no other node to move to, so the only recovery is an in-place restart.
func inPlaceEnv(t *testing.T, containers []models.NodeContainer, lastBeat time.Time) (*HealthService, func() models.Project, func() int64) {
	t.Helper()
	hs, _, db := newFailoverEnv(t)
	db.Model(&models.Node{}).Where("id IN ?", []string{"dead", "ok"}).Update("online", false)
	db.Model(&models.Project{}).Where("id = ?", "p1").Update("node_id", "good")
	db.Save(&models.Node{ID: "good", Hostname: "good", VPNIP: "10.0.0.3", Online: true, HealthScore: 90, LastHeartbeat: lastBeat, Containers: containers})
	jobs := func() int64 {
		var n int64
		db.Model(&models.Job{}).Count(&n)
		return n
	}
	return hs, func() models.Project { return loadProject(db) }, jobs
}

func TestInPlaceRestart_RedeploysWhenTheAgentReportsTheContainerDown(t *testing.T) {
	hs, project, jobs := inPlaceEnv(t, []models.NodeContainer{{Name: "redis", State: "running"}, {Name: "api", State: "exited"}}, time.Now())
	p := project()
	hs.handleUnhealthyProject(&p)
	if jobs() != 1 {
		t.Fatalf("a container the agent reports exited should be redeployed in place, got %d jobs", jobs())
	}
	if project().Status != "running" {
		t.Errorf("the project must not be marked failed while it is being restarted, status = %q", project().Status)
	}
}

func TestInPlaceRestart_DoesNotRedeployAHealthyAppDuringANetworkBlip(t *testing.T) {
	// The Hub's checks fail, but the node's own agent says the container is running
	hs, project, jobs := inPlaceEnv(t, []models.NodeContainer{{Name: "api", State: "running"}}, time.Now())
	for i := 1; i <= maxUnconfirmedWaits; i++ {
		p := project()
		hs.handleUnhealthyProject(&p)
		if jobs() != 0 || project().Status != "running" {
			t.Fatalf("round %d: should just wait (no job, still running), got %d jobs, status %q", i, jobs(), project().Status)
		}
	}
	p := project()
	hs.handleUnhealthyProject(&p) // waited long enough: it is treated as failed, still without redeploying it
	if jobs() != 0 {
		t.Fatalf("a container the agent reports running must never be redeployed, got %d jobs", jobs())
	}
	if project().Status != "failed" {
		t.Errorf("after waiting it should be marked failed (and recover by itself when it answers), status = %q", project().Status)
	}
}

func TestInPlaceRestart_WaitsForAFreshReportAfterARealCrash(t *testing.T) {
	// Right after a crash the agent's report still shows the container running; a later one shows it exited
	hs, project, jobs := inPlaceEnv(t, []models.NodeContainer{{Name: "api", State: "running"}}, time.Now())
	p := project()
	hs.handleUnhealthyProject(&p)
	if jobs() != 0 {
		t.Fatal("the first round should wait")
	}
	hs.db.Model(&models.Node{}).Where("id = ?", "good").Updates(map[string]interface{}{
		"containers": `[{"name":"api","state":"exited"}]`, "last_heartbeat": time.Now()})
	p = project()
	hs.handleUnhealthyProject(&p)
	if jobs() != 1 {
		t.Fatalf("once the agent reports it exited, it should be redeployed, got %d jobs", jobs())
	}
}
