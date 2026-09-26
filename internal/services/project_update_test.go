package services

import (
	"net/http"
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
