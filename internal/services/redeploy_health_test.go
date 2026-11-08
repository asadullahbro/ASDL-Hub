package services

import (
	"testing"
	"time"

	"github.com/asdl/hub/internal/models"
)

// A CI deploy of gamer-ak-bot swapped its container on the tower; the Hub counted the checks that failed meanwhile,
// failed it over to the laptop (a slow pull), and removed the tower copy once that deploy finished. While an app is
// redeployed on its own node, its checks don't count.
func TestHealth_NoFailoverWhileRedeployingInPlace(t *testing.T) {
	hs, _, db := newFailoverEnv(t)
	// The app runs on "good"; checks there fail at once (nothing listens on port 1).
	db.Model(&models.Node{}).Where("id = ?", "good").Update("vpn_ip", "127.0.0.1")
	db.Create(&models.Deployment{ID: "ci", ProjectID: "p1", NodeID: "good", Status: models.DeploymentStatusRunning,
		Repository: "o/api", Branch: "main", CreatedAt: time.Now()})
	db.Model(&models.Project{}).Where("id = ?", "p1").Select("node_id", "deployment_id", "ports", "auto_port").
		Updates(&models.Project{NodeID: "good", DeploymentID: "ci", Ports: []string{"1:8000"}, AutoPort: false})

	for round := 0; round < failoverThreshold+1; round++ {
		hs.checkAllProjects()
	}
	var n int64
	db.Model(&models.Job{}).Where("type = ?", models.JobTypeDeploy).Count(&n)
	if n != 0 {
		t.Fatalf("failed over during its own deploy (%d deploy jobs)", n)
	}

	// The deploy is done and the app still doesn't answer: now it is a real failure.
	db.Model(&models.Deployment{}).Where("id = ?", "ci").Update("status", models.DeploymentStatusFailed)
	for round := 0; round < failoverThreshold; round++ {
		hs.checkAllProjects()
	}
	db.Model(&models.Job{}).Where("type = ?", models.JobTypeDeploy).Count(&n)
	if n == 0 {
		t.Error("once the deploy ended, failing checks should lead to a failover or restart")
	}
}

func TestRedeployingInPlace(t *testing.T) {
	hs, _, db := newFailoverEnv(t)
	now := time.Now()
	online := &models.Node{ID: "good", Online: true}
	project := &models.Project{ID: "p1", NodeID: "good", DeploymentID: "d"}
	set := func(nodeID, status string, age time.Duration) {
		db.Unscoped().Where("id = ?", "d").Delete(&models.Deployment{})
		db.Create(&models.Deployment{ID: "d", ProjectID: "p1", NodeID: nodeID, Status: status, Repository: "r", Branch: "b", CreatedAt: now.Add(-age)})
	}

	set("good", models.DeploymentStatusRunning, time.Minute)
	if !hs.redeployingInPlace(project, online, now) {
		t.Error("a running deploy on its own online node should pause the checks")
	}
	if hs.redeployingInPlace(project, &models.Node{ID: "good", Online: false}, now) {
		t.Error("a deploy for an offline node must not hold off failover")
	}
	set("ok", models.DeploymentStatusRunning, time.Minute)
	if hs.redeployingInPlace(project, online, now) {
		t.Error("a deploy to another node (a failover underway) isn't a redeploy in place")
	}
	set("good", models.DeploymentStatusPending, 11*time.Minute)
	if hs.redeployingInPlace(project, online, now) {
		t.Error("a deploy stuck for over 10 minutes should no longer pause the checks")
	}
	set("good", models.DeploymentStatusCompleted, time.Minute)
	if hs.redeployingInPlace(project, online, now) {
		t.Error("a finished deploy doesn't pause the checks (the startup grace covers what follows)")
	}
}
