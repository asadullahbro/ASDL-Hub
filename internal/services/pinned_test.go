package services

import (
	"testing"

	"github.com/asdl/hub/internal/models"
)

func TestPinned_FailoverNeverMovesItAndTheAppStaysDown(t *testing.T) {
	hs, _, db := newFailoverEnv(t)
	// p1 runs on "dead" (offline); "good" and "ok" are healthy and would normally take it over
	db.Model(&models.Project{}).Where("id = ?", "p1").Update("pinned", true)
	p := loadProject(db)
	hs.handleUnhealthyProject(&p)

	var jobs, migrations int64
	db.Model(&models.Job{}).Count(&jobs)
	db.Model(&models.Migration{}).Count(&migrations)
	if jobs != 0 || migrations != 0 {
		t.Errorf("a pinned app must not be moved: %d jobs, %d migrations", jobs, migrations)
	}
	got := loadProject(db)
	if got.NodeID != "dead" || got.Status != "failed" || got.HealthStatus != "unhealthy" {
		t.Errorf("pinned app should stay on its node and show as down: node=%s status=%s health=%s", got.NodeID, got.Status, got.HealthStatus)
	}
}

func TestPinned_UnpinnedStillFailsOver(t *testing.T) {
	hs, _, db := newFailoverEnv(t)
	p := loadProject(db)
	hs.handleUnhealthyProject(&p)

	var job models.Job
	if err := db.First(&job, "type = ?", models.JobTypeDeploy).Error; err != nil || job.NodeID != "good" {
		t.Fatalf("an app that is not pinned fails over as before: %+v %v", job, err)
	}
}

func TestPinned_MaintenanceLeavesItInPlace(t *testing.T) {
	_, _, db := newFailoverEnv(t)
	db.Model(&models.Project{}).Where("id = ?", "p1").Updates(map[string]interface{}{"node_id": "good", "pinned": true})
	ops := NewNodeOpsService(db, NewDeployer(db))

	var good models.Node
	db.First(&good, "id = ?", "good")
	res, err := ops.SetMaintenance(&good, true, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Moving) != 0 || len(res.Stays) != 1 || res.Stays[0] != "api" {
		t.Fatalf("a pinned app stays during maintenance: %+v", res)
	}
	var jobs int64
	db.Model(&models.Job{}).Count(&jobs)
	if jobs != 0 {
		t.Errorf("nothing should be dispatched, got %d jobs", jobs)
	}
}

func TestPinned_CanBeSetAndClearedThroughTheAPIWithoutRedeploying(t *testing.T) {
	_, _, db := newFailoverEnv(t)
	db.Model(&models.Project{}).Where("id = ?", "p1").Update("node_id", "good")
	svc := NewProjectService(db, NewDeployer(db))

	updateProject(t, svc, `{"pinned":true}`)
	if !loadProject(db).Pinned {
		t.Fatal("pinned should be set")
	}
	updateProject(t, svc, `{"description":"unrelated"}`)
	if !loadProject(db).Pinned {
		t.Fatal("leaving pinned out must keep it")
	}
	updateProject(t, svc, `{"pinned":false}`)
	if loadProject(db).Pinned {
		t.Fatal("pinned should be cleared")
	}
	var jobs int64
	db.Model(&models.Job{}).Count(&jobs)
	if jobs != 0 {
		t.Errorf("changing pinned must not redeploy, got %d jobs", jobs)
	}
}
