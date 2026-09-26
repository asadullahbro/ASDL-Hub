package services

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

func newFailoverEnv(t *testing.T) (*HealthService, *JobService, *gorm.DB) {
	t.Helper()
	db := testutil.NewDB(t, &models.Node{}, &models.Job{}, &models.Migration{}, &models.Project{},
		&models.Deployment{}, &models.OIDCDeployment{}, &models.Setting{}, &models.GitHubToken{})
	db.Create(&models.Node{ID: "dead", Hostname: "dead", VPNIP: "10.0.0.2", Online: false})
	db.Create(&models.Node{ID: "good", Hostname: "good", VPNIP: "10.0.0.3", Online: true, HealthScore: 90})
	db.Create(&models.Node{ID: "ok", Hostname: "ok", VPNIP: "10.0.0.4", Online: true, HealthScore: 60})
	db.Create(&models.Project{ID: "p1", Name: "api", NodeID: "dead", Image: "ghcr.io/o/api:v1",
		Status: "running", AutoPort: true, Ports: []string{"20000:8000"},
		EnvVars: models.SecretEnvVars{{Key: "DB_PASSWORD", Value: "hunter2"}}})
	// NginxService with a missing routes dir is a no-op.
	nginx := &NginxService{db: db, routesDir: t.TempDir() + "/missing", certs: map[string]bool{}, certFailed: map[string]time.Time{}}
	return NewHealthService(db, nil, nginx, NewDeployer(db)), NewJobService(db), db
}

func loadProject(db *gorm.DB) models.Project {
	var p models.Project
	db.First(&p, "id = ?", "p1")
	return p
}

func TestFailover_RedeploysOnHealthiestNodeWithSecrets(t *testing.T) {
	hs, _, db := newFailoverEnv(t)
	p := loadProject(db)
	hs.handleUnhealthyProject(&p)

	var job models.Job
	if err := db.First(&job, "type = ?", models.JobTypeDeploy).Error; err != nil {
		t.Fatalf("expected a deploy job: %v", err)
	}
	if job.NodeID != "good" {
		t.Errorf("failover went to %s, want the healthiest online node", job.NodeID)
	}
	if !strings.Contains(strings.Join(job.Environment, "\n"), "ASDL_ENV_DB_PASSWORD=hunter2") {
		t.Errorf("secrets must travel with the failover, env = %v", job.Environment)
	}
	var m models.Migration
	if err := db.First(&m, "job_id = ?", job.ID).Error; err != nil || m.Status != models.MigrationStatusRunning {
		t.Errorf("expected a running migration linked to the job: %+v %v", m, err)
	}
}

func TestFailover_SkipsNodesThatAlreadyFailedThenGivesUp(t *testing.T) {
	hs, _, db := newFailoverEnv(t)
	for _, n := range []string{"good", "ok"} {
		db.Create(&models.Migration{ID: "m-" + n, ProjectID: "p1", ContainerID: "c", SourceNodeID: "dead",
			TargetNodeID: n, Status: models.MigrationStatusFailed, CreatedAt: time.Now()})
	}
	p := loadProject(db)
	hs.handleUnhealthyProject(&p)

	var jobs int64
	db.Model(&models.Job{}).Count(&jobs)
	if jobs != 0 {
		t.Errorf("no node is left, so nothing should be dispatched (got %d jobs)", jobs)
	}
	if got := loadProject(db).Status; got != "failed" {
		t.Errorf("status = %q, want failed when no node is left", got)
	}
}

func TestFailover_CompletionMovesProjectRecordsPortAndCleansUp(t *testing.T) {
	hs, js, db := newFailoverEnv(t)
	p := loadProject(db)
	hs.handleUnhealthyProject(&p)
	var job models.Job
	db.First(&job, "type = ?", models.JobTypeDeploy)

	c, w := newTestContext(http.MethodPost, "/jobs/x/complete",
		`{"status":"completed","logs":"...\nASDL_PORT_MAPPING=20000:8000\n"}`)
	c.Params = gin.Params{{Key: "id", Value: job.ID}}
	c.Set("vpn_ip", "10.0.0.3")
	js.Complete(c)
	if w.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", w.Code, w.Body.String())
	}

	got := loadProject(db)
	if got.NodeID != "good" || got.Status != "running" || len(got.Ports) != 1 || got.Ports[0] != "20000:8000" {
		t.Errorf("project after failover = node %q status %q ports %v", got.NodeID, got.Status, got.Ports)
	}
	var m models.Migration
	db.First(&m, "job_id = ?", job.ID)
	if m.Status != models.MigrationStatusCompleted {
		t.Errorf("migration status = %q", m.Status)
	}
	var stop models.Job
	if err := db.First(&stop, "node_id = ? AND type = ?", "dead", models.JobTypeFailoverStop).Error; err != nil {
		t.Errorf("expected the dead node's container to be queued for removal: %v", err)
	}
	var done models.Job
	db.First(&done, "id = ?", job.ID)
	if len(done.Environment) != 0 {
		t.Errorf("secrets must be wiped from finished jobs, env = %v", done.Environment)
	}
}

func TestProjectEnv_StoredEncrypted(t *testing.T) {
	_, _, db := newFailoverEnv(t)
	var raw string
	db.Raw("SELECT env_vars FROM projects WHERE id = ?", "p1").Scan(&raw)
	if strings.Contains(raw, "hunter2") || !strings.Contains(raw, "enc:v1:") {
		t.Errorf("env_vars column should be encrypted, got %s", raw)
	}
}
