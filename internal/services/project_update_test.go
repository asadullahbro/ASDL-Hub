package services

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/asdl/hub/internal/models"
)

func updateProject(t *testing.T, svc *ProjectService, body string) {
	t.Helper()
	c, w := newTestContext(http.MethodPut, "/projects/p1", body)
	c.Params = gin.Params{{Key: "id", Value: "p1"}}
	svc.UpdateProject(c)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
}

func TestUpdateProject_RedeploysOnlyWhenConfigChanges(t *testing.T) {
	_, _, db := newFailoverEnv(t)
	db.Model(&models.Project{}).Where("id = ?", "p1").Update("node_id", "good")
	svc := NewProjectService(db, NewDeployer(db))

	// Sending back the masked value and unchanged fields: nothing to redeploy.
	updateProject(t, svc, `{"description":"x","env_vars":[{"key":"DB_PASSWORD","value":"********"}]}`)
	var jobs int64
	db.Model(&models.Job{}).Count(&jobs)
	if jobs != 0 {
		t.Fatalf("unchanged config must not redeploy, got %d jobs", jobs)
	}
	if got := loadProject(db).EnvVars; len(got) != 1 || got[0].Value != "hunter2" {
		t.Fatalf("masked value must keep the stored secret, got %v", got)
	}

	updateProject(t, svc, `{"env_vars":[{"key":"DB_PASSWORD","value":"********"},{"key":"NEW","value":"1"}]}`)
	db.Model(&models.Job{}).Count(&jobs)
	if jobs != 1 {
		t.Fatalf("an env change must redeploy once, got %d jobs", jobs)
	}
}

func TestUpdateProject_NodeChangeMovesApp(t *testing.T) {
	_, _, db := newFailoverEnv(t)
	db.Model(&models.Project{}).Where("id = ?", "p1").Update("node_id", "good")
	svc := NewProjectService(db, NewDeployer(db))

	updateProject(t, svc, `{"node_id":"ok"}`)

	var job models.Job
	if err := db.First(&job, "type = ?", models.JobTypeDeploy).Error; err != nil || job.NodeID != "ok" {
		t.Fatalf("expected a deploy job on the new node: %+v %v", job, err)
	}
	if got := loadProject(db).NodeID; got != "good" {
		t.Errorf("project must stay on its node until the new container runs, node = %q", got)
	}
}

func TestCreateProject_DeploysInsteadOfClaimingRunning(t *testing.T) {
	_, _, db := newFailoverEnv(t)
	svc := NewProjectService(db, NewDeployer(db))

	c, w := newTestContext(http.MethodPost, "/projects", `{"name":"whoami","node_id":"good","image":"traefik/whoami:v1.10"}`)
	svc.CreateProject(c)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var p models.Project
	db.First(&p, "name = ?", "whoami")
	if p.Status != "deploying" || p.NodeID != "" || !p.AutoPort {
		t.Errorf("new project = status %q node %q auto %v; want deploying, no node until it runs, auto port", p.Status, p.NodeID, p.AutoPort)
	}
	var job models.Job
	if err := db.First(&job, "type = ? AND node_id = ?", models.JobTypeDeploy, "good").Error; err != nil {
		t.Fatalf("expected a deploy job on the chosen node: %v", err)
	}
	// p1 already uses 20000 in newFailoverEnv, but on node "dead"; on "good"
	// nothing is taken, so the suggestion starts at the first auto port.
	if !strings.Contains(job.Command, "HPORT=20000") {
		t.Errorf("unexpected port suggestion in:\n%s", job.Command)
	}
}

func TestDeleteProject_RemovesContainerAndCancelsPendingDeploys(t *testing.T) {
	_, _, db := newFailoverEnv(t)
	db.Model(&models.Project{}).Where("id = ?", "p1").Update("node_id", "good")
	svc := NewProjectService(db, NewDeployer(db))
	// A deploy to "ok" that no node has picked up yet.
	if _, _, err := NewDeployer(db).Dispatch(func() *models.Project { p := loadProject(db); return &p }(),
		&models.Node{ID: "ok", Hostname: "ok"}, "ghcr.io/o/api:v2", DeployMeta{Trigger: TriggerManual}); err != nil {
		t.Fatal(err)
	}

	c, w := newTestContext(http.MethodDelete, "/projects/p1", "")
	c.Params = gin.Params{{Key: "id", Value: "p1"}}
	svc.DeleteProject(c)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}

	var deploy models.Job
	db.First(&deploy, "type = ?", models.JobTypeDeploy)
	if deploy.Status != models.JobStatusCancelled {
		t.Errorf("pending deploy = %q, want cancelled", deploy.Status)
	}
	var stops []models.Job
	db.Where("type = ?", models.JobTypeFailoverStop).Find(&stops)
	got := map[string]bool{}
	for _, j := range stops {
		got[j.NodeID] = true
		if !strings.Contains(j.Command, "docker rm -f 'api'") {
			t.Errorf("unexpected stop command %q", j.Command)
		}
	}
	if !got["good"] || !got["ok"] || len(stops) != 2 {
		t.Errorf("containers must be removed from good and ok, got %v", got)
	}
}
