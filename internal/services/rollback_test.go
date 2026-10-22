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

const (
	imgV1 = "ghcr.io/o/api:v1"
	imgV2 = "ghcr.io/o/api:v2"
)

// seedDeploy records a deployment of p1 that finished minsAgo minutes ago.
func seedDeploy(db *gorm.DB, id, image, trigger, status string, minsAgo int) *models.Deployment {
	at := time.Now().Add(-time.Duration(minsAgo) * time.Minute)
	dep := &models.Deployment{ID: id, ProjectID: "p1", Trigger: trigger, Repository: "o/api", Branch: "main", NodeID: "good",
		Status: status, ImageName: image, CreatedAt: at.Add(-time.Minute)}
	if status == models.DeploymentStatusCompleted {
		dep.CompletedAt = &at
	}
	db.Create(dep)
	return dep
}

// rollbackEnv is a project running v2 on a healthy node, deployed minsAgo minutes ago after v1.
func rollbackEnv(t *testing.T, trigger string, minsAgo int) (*HealthService, *gorm.DB) {
	t.Helper()
	hs, _, db := newFailoverEnv(t)
	seedDeploy(db, "d1", imgV1, TriggerCI, models.DeploymentStatusCompleted, minsAgo+60)
	seedDeploy(db, "d2", imgV2, trigger, models.DeploymentStatusCompleted, minsAgo)
	db.Model(&models.Project{}).Where("id = ?", "p1").Updates(map[string]interface{}{"node_id": "good", "image": imgV2, "deployment_id": "d2"})
	return hs, db
}

func deploymentsOf(db *gorm.DB, trigger string) []models.Deployment {
	var out []models.Deployment
	db.Where("trigger = ?", trigger).Order("created_at").Find(&out)
	return out
}

func TestAutoRollbackTarget(t *testing.T) {
	cases := []struct {
		name    string
		trigger string
		minsAgo int
		want    string // image to go back to, "" for no rollback
	}{
		{"a push that just broke the app goes back to the previous image", TriggerCI, 4, imgV1},
		{"so does a manual redeploy that changed the image", TriggerManual, 4, imgV1},
		{"a deploy from long ago is not blamed", TriggerCI, 30, ""},
		{"a failover is never undone", TriggerFailover, 4, ""},
		{"a rollback is never undone", TriggerRollback, 4, ""},
	}
	for _, tc := range cases {
		_, db := rollbackEnv(t, tc.trigger, tc.minsAgo)
		var p models.Project
		db.First(&p, "id = ?", "p1")
		got := autoRollbackTarget(db, &p, time.Now())
		switch {
		case tc.want == "" && got != nil:
			t.Errorf("%s: want no rollback, got %s", tc.name, got.ImageName)
		case tc.want != "" && (got == nil || got.ImageName != tc.want):
			t.Errorf("%s: want %s, got %v", tc.name, tc.want, got)
		}
	}
}

func TestAutoRollbackTarget_NeedsAnEarlierDifferentImage(t *testing.T) {
	_, db := rollbackEnv(t, TriggerManual, 4)
	// d1 ran the same image: d2 was only a redeploy (a config change), which an older image would not fix.
	db.Model(&models.Deployment{}).Where("id = ?", "d1").Update("image_name", imgV2)
	var p models.Project
	db.First(&p, "id = ?", "p1")
	if got := autoRollbackTarget(db, &p, time.Now()); got != nil {
		t.Fatalf("a redeploy of the same image must not be rolled back, got %s", got.ImageName)
	}

	// No earlier deployment at all.
	db.Delete(&models.Deployment{}, "id = ?", "d1")
	if got := autoRollbackTarget(db, &p, time.Now()); got != nil {
		t.Fatalf("a first deploy has nothing to go back to, got %s", got.ImageName)
	}
}

func TestAutoRollbackTarget_IgnoresAnUnfinishedDeploy(t *testing.T) {
	_, db := rollbackEnv(t, TriggerCI, 4)
	db.Model(&models.Deployment{}).Where("id = ?", "d2").Updates(map[string]interface{}{"status": models.DeploymentStatusRunning, "completed_at": nil})
	var p models.Project
	db.First(&p, "id = ?", "p1")
	if got := autoRollbackTarget(db, &p, time.Now()); got != nil {
		t.Fatalf("a deploy that is still running has not gone bad yet, got %s", got.ImageName)
	}
}

func TestHandleUnhealthy_RollsBackABadReleaseInsteadOfFailingOver(t *testing.T) {
	hs, db := rollbackEnv(t, TriggerCI, 4)
	p := loadProject(db)
	hs.handleUnhealthyProject(&p)

	rb := deploymentsOf(db, TriggerRollback)
	if len(rb) != 1 || rb[0].ImageName != imgV1 || rb[0].NodeID != "good" {
		t.Fatalf("want one rollback to %s on the same node, got %+v", imgV1, rb)
	}
	var migrations int64
	db.Model(&models.Migration{}).Count(&migrations)
	if migrations != 0 || len(deploymentsOf(db, TriggerFailover)) != 0 {
		t.Fatal("a bad release must be rolled back, not failed over to another node")
	}
}

func TestHandleUnhealthy_WaitsWhileTheRollbackRuns(t *testing.T) {
	hs, db := rollbackEnv(t, TriggerCI, 4)
	p := loadProject(db)
	hs.handleUnhealthyProject(&p) // queues the rollback

	p = loadProject(db)
	hs.handleUnhealthyProject(&p) // the app is still failing while the rollback runs
	if n := len(deploymentsOf(db, TriggerRollback)); n != 1 {
		t.Fatalf("must not queue a second rollback, got %d", n)
	}
	var migrations int64
	db.Model(&models.Migration{}).Count(&migrations)
	if migrations != 0 || len(deploymentsOf(db, TriggerFailover)) != 0 {
		t.Fatal("must not fail over while the rollback is still running")
	}
}

func TestHandleUnhealthy_AFailedRollbackFallsBackToFailover(t *testing.T) {
	hs, db := rollbackEnv(t, TriggerCI, 4)
	p := loadProject(db)
	hs.handleUnhealthyProject(&p)

	// The rollback finished, and the app still does not answer: that is not the release's fault.
	now := time.Now()
	rb := deploymentsOf(db, TriggerRollback)[0]
	db.Model(&models.Deployment{}).Where("id = ?", rb.ID).Updates(map[string]interface{}{"status": models.DeploymentStatusCompleted, "completed_at": now})
	p = loadProject(db)
	hs.handleUnhealthyProject(&p)

	if n := len(deploymentsOf(db, TriggerRollback)); n != 1 {
		t.Fatalf("a rollback must never be rolled back again, got %d rollbacks", n)
	}
	var migrations int64
	db.Model(&models.Migration{}).Count(&migrations)
	if migrations != 1 {
		t.Fatalf("the normal failover should take over, got %d migrations", migrations)
	}
}

func TestHandleUnhealthy_NodeDownIsAFailoverNotARollback(t *testing.T) {
	hs, db := rollbackEnv(t, TriggerCI, 4)
	db.Model(&models.Node{}).Where("id = ?", "good").Update("online", false)
	p := loadProject(db)
	hs.handleUnhealthyProject(&p)
	if n := len(deploymentsOf(db, TriggerRollback)); n != 0 {
		t.Fatalf("a dead node says nothing about the release, got %d rollbacks", n)
	}
}

func TestRollbackAPI(t *testing.T) {
	hs, db := rollbackEnv(t, TriggerCI, 60)
	svc := NewProjectService(db, hs.deployer)

	c, w := newTestContext(http.MethodGet, "/projects/p1/rollback", "")
	c.Params = append(c.Params, ginParam("id", "p1"))
	svc.RollbackInfo(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), imgV1) || !strings.Contains(w.Body.String(), `"current_image":"`+imgV2) {
		t.Fatalf("info should name both images: %d %s", w.Code, w.Body.String())
	}

	c, w = newTestContext(http.MethodPost, "/projects/p1/rollback", "")
	c.Params = append(c.Params, ginParam("id", "p1"))
	svc.Rollback(c)
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", w.Code, w.Body.String())
	}
	if rb := deploymentsOf(db, TriggerRollback); len(rb) != 1 || rb[0].ImageName != imgV1 {
		t.Fatalf("want one rollback deploy of %s, got %+v", imgV1, rb)
	}

	// The rollback is now pending: a second one is refused until it finishes.
	c, w = newTestContext(http.MethodPost, "/projects/p1/rollback", "")
	c.Params = append(c.Params, ginParam("id", "p1"))
	svc.Rollback(c)
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409 while a deploy is in progress, got %d", w.Code)
	}
}

func TestRollbackAPI_NothingToGoBackTo(t *testing.T) {
	hs, _, db := newFailoverEnv(t)
	svc := NewProjectService(db, hs.deployer)
	c, w := newTestContext(http.MethodPost, "/projects/p1/rollback", "")
	c.Params = append(c.Params, ginParam("id", "p1"))
	svc.Rollback(c)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "no earlier version") {
		t.Fatalf("want 409 'no earlier version', got %d: %s", w.Code, w.Body.String())
	}
}

func ginParam(k, v string) gin.Param { return gin.Param{Key: k, Value: v} }
