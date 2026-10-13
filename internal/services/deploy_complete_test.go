package services

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

// setupDeploy creates a project that runs on node "old" and a deploy job
// dispatched to node "new" for it.
func setupDeploy(t *testing.T, projectNode string) (*JobService, *gorm.DB) {
	t.Helper()
	db := testutil.NewDB(t, &models.Node{}, &models.Job{}, &models.Migration{},
		&models.Project{}, &models.Deployment{}, &models.OIDCDeployment{})
	db.Create(&models.Node{ID: "old", Hostname: "old", VPNIP: "10.0.0.2"})
	db.Create(&models.Node{ID: "new", Hostname: "new", VPNIP: "10.0.0.3"})
	db.Create(&models.Project{ID: "p1", Name: "api", Repository: "o/api", NodeID: projectNode,
		Image: "img:v1", Status: "deploying", DeploymentID: "d1"})
	db.Create(&models.Job{ID: "j1", NodeID: "new", Type: models.JobTypeDeploy, Status: models.JobStatusRunning,
		Environment: []string{RegistryTokenEnv + "=s3cret"}})
	db.Create(&models.Deployment{ID: "d1", JobID: "j1", NodeID: "new", Repository: "o/api", Branch: "main", ImageName: "img:v2"})
	db.Create(&models.OIDCDeployment{ID: "o1", JobID: "j1", Repository: "o/api", Status: "dispatched"})
	return NewJobService(db), db
}

func completeJob(t *testing.T, svc *JobService, status string) {
	t.Helper()
	c, w := newTestContext(http.MethodPost, "/jobs/j1/complete", `{"status":"`+status+`","logs":"done"}`)
	c.Params = gin.Params{{Key: "id", Value: "j1"}}
	c.Set("vpn_ip", "10.0.0.3")
	svc.Complete(c)
	if w.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", w.Code, w.Body.String())
	}
}

func TestDeployComplete_SuccessMovesProjectAndStopsOldContainer(t *testing.T) {
	svc, db := setupDeploy(t, "old")
	completeJob(t, svc, models.JobStatusCompleted)

	var p models.Project
	db.First(&p, "id = ?", "p1")
	if p.Status != "running" || p.NodeID != "new" || p.Image != "img:v2" {
		t.Errorf("project = status %q node %q image %q; want running on new with img:v2", p.Status, p.NodeID, p.Image)
	}
	var d models.Deployment
	db.First(&d, "id = ?", "d1")
	if d.Status != models.DeploymentStatusCompleted || d.Logs != "done" {
		t.Errorf("deployment = %q %q", d.Status, d.Logs)
	}
	var o models.OIDCDeployment
	db.First(&o, "id = ?", "o1")
	if o.Status != "succeeded" {
		t.Errorf("oidc deployment status = %q", o.Status)
	}
	var stop models.Job
	if err := db.First(&stop, "node_id = ? AND type = ?", "old", models.JobTypeFailoverStop).Error; err != nil {
		t.Fatalf("expected a stop job on the old node: %v", err)
	}
}

func TestDeployComplete_FailureOnSameNodeKeepsProjectForHealthCheck(t *testing.T) {
	svc, db := setupDeploy(t, "new")
	completeJob(t, svc, models.JobStatusFailed)

	var p models.Project
	db.First(&p, "id = ?", "p1")
	if p.Status != "running" || p.Image != "img:v1" {
		t.Errorf("project = status %q image %q; want running with the previous image", p.Status, p.Image)
	}
}

func TestDeployComplete_FailureOfFirstDeployMarksFailed(t *testing.T) {
	svc, db := setupDeploy(t, "")
	completeJob(t, svc, models.JobStatusFailed)

	var p models.Project
	db.First(&p, "id = ?", "p1")
	if p.Status != "failed" {
		t.Errorf("project status = %q, want failed", p.Status)
	}
}

func TestDeployComplete_IgnoresSupersededDeploy(t *testing.T) {
	svc, db := setupDeploy(t, "old")
	db.Model(&models.Project{}).Where("id = ?", "p1").Update("deployment_id", "newer")
	completeJob(t, svc, models.JobStatusCompleted)

	var p models.Project
	db.First(&p, "id = ?", "p1")
	if p.NodeID != "old" {
		t.Errorf("an older deploy finishing late must not move the project, node = %q", p.NodeID)
	}
}

// A migration to "new" is overtaken by a CI deploy to "old"; when the migration
// then finishes it leaves a container on "new" that nothing manages.
func TestDeployComplete_SupersededDeployElsewhereLeavesNoStrayCopy(t *testing.T) {
	svc, db := setupDeploy(t, "old")
	db.Create(&models.Deployment{ID: "newer", JobID: "j2", NodeID: "old", ImageName: "img:v3"})
	db.Model(&models.Project{}).Where("id = ?", "p1").Update("deployment_id", "newer")
	completeJob(t, svc, models.JobStatusCompleted)

	var stop models.Job
	if err := db.First(&stop, "node_id = ? AND type = ?", "new", models.JobTypeFailoverStop).Error; err != nil {
		t.Fatalf("expected the stray copy on the node the superseded deploy used to be removed: %v", err)
	}
	var p models.Project
	db.First(&p, "id = ?", "p1")
	if p.NodeID != "old" {
		t.Errorf("project moved to %q, want it to stay on old", p.NodeID)
	}
}

func TestDeployComplete_SupersededDeployOnSameNodeIsLeftForTheNewerOne(t *testing.T) {
	svc, db := setupDeploy(t, "old")
	db.Create(&models.Deployment{ID: "newer", JobID: "j2", NodeID: "new", ImageName: "img:v3"})
	db.Model(&models.Project{}).Where("id = ?", "p1").Update("deployment_id", "newer")
	completeJob(t, svc, models.JobStatusCompleted)

	var n int64
	db.Model(&models.Job{}).Where("type = ?", models.JobTypeFailoverStop).Count(&n)
	if n != 0 {
		t.Errorf("a newer deploy to the same node will replace the container; got %d stop jobs", n)
	}
}

func TestJobGet_RedactsEnvironment(t *testing.T) {
	svc, _ := setupDeploy(t, "old")
	c, w := newTestContext(http.MethodGet, "/jobs/j1", "")
	c.Params = gin.Params{{Key: "id", Value: "j1"}}
	svc.Get(c)

	var job models.Job
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if len(job.Environment) != 1 || job.Environment[0] != RegistryTokenEnv+"=********" {
		t.Errorf("environment not redacted: %v", job.Environment)
	}
}
