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
	db := testutil.NewDB(t, &models.Node{}, &models.Job{}, &models.Migration{}, &models.Project{}, &models.ProjectPlugin{},
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

// Several apps failing over to one node at once queue there and run one after another. The ones further back
// must not be given up on after a minute just because the node is still busy with the first: that once put the
// only healthy node in cooldown and left two apps down for half an hour.
func TestStuckMigration_LiveTargetWorkingThroughAQueueIsNotStuck(t *testing.T) {
	_, _, db := newFailoverEnv(t)
	ms := NewMigrationService(db, nil, nil, NewDeployer(db))
	now := time.Now()
	db.Model(&models.Node{}).Where("id = ?", "good").Update("last_heartbeat", now.Add(-10*time.Second))
	waited := now.Add(-90 * time.Second)
	db.Create(&models.Job{ID: "queued", NodeID: "good", Type: models.JobTypeDeploy, Status: models.JobStatusPending, CreatedAt: waited})
	db.Create(&models.Migration{ID: "m1", ProjectID: "p1", ContainerID: "c", SourceNodeID: "dead", TargetNodeID: "good",
		Status: models.MigrationStatusRunning, JobID: "queued", CreatedAt: waited})

	ms.cleanupStuckMigrations()

	var m models.Migration
	db.First(&m, "id = ?", "m1")
	var j models.Job
	db.First(&j, "id = ?", "queued")
	if m.Status != models.MigrationStatusRunning || j.Status != models.JobStatusPending {
		t.Fatalf("queued behind other jobs on a live node: migration %q job %q, want running/pending", m.Status, j.Status)
	}

	// A queue that never moves is still given up on eventually.
	long := now.Add(-11 * time.Minute)
	db.Model(&models.Migration{}).Where("id = ?", "m1").Update("created_at", long)
	ms.cleanupStuckMigrations()
	db.First(&m, "id = ?", "m1")
	if m.Status != models.MigrationStatusFailed {
		t.Errorf("unclaimed for 11 minutes on a live node: migration %q, want failed", m.Status)
	}
}

func TestMigrationStuck(t *testing.T) {
	now := time.Now()
	live := &models.Node{Online: true, LastHeartbeat: now.Add(-5 * time.Second)}
	quiet := &models.Node{Online: true, LastHeartbeat: now.Add(-3 * time.Minute)}
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	started := ago(6 * time.Minute)
	cases := []struct {
		name   string
		mAge   time.Duration
		job    *models.Job
		target *models.Node
		want   bool
	}{
		{"pending on live node, 2m", 2 * time.Minute, &models.Job{Status: models.JobStatusPending}, live, false},
		{"pending on quiet node, 2m", 2 * time.Minute, &models.Job{Status: models.JobStatusPending}, quiet, true},
		{"pending, target gone", 2 * time.Minute, &models.Job{Status: models.JobStatusPending}, nil, true},
		{"running, started 6m ago", 7 * time.Minute, &models.Job{Status: models.JobStatusRunning, StartedAt: &started}, live, true},
		{"running, started just now after a long queue", 8 * time.Minute, &models.Job{Status: models.JobStatusRunning, StartedAt: func() *time.Time { t := ago(30 * time.Second); return &t }()}, live, false},
		{"job failed", 2 * time.Minute, &models.Job{Status: models.JobStatusFailed}, live, true},
		{"no job, 6m", 6 * time.Minute, nil, live, true},
	}
	for _, c := range cases {
		got, _ := migrationStuck(&models.Migration{CreatedAt: ago(c.mAge)}, c.job, c.target, now)
		if got != c.want {
			t.Errorf("%s: stuck = %v, want %v", c.name, got, c.want)
		}
	}
}

// A project marked failed because no node could take it is moved as soon as one can, instead of staying down
// until its own node returns.
func TestRecoverFailed_RetriesFailoverWhenANodeCanTakeIt(t *testing.T) {
	hs, _, db := newFailoverEnv(t)
	db.Model(&models.Project{}).Where("id = ?", "p1").Updates(map[string]interface{}{"status": "failed", "health_status": "unhealthy"})

	hs.recoverFailed()

	var job models.Job
	if err := db.First(&job, "type = ?", models.JobTypeDeploy).Error; err != nil {
		t.Fatalf("expected a failover deploy for the failed project: %v", err)
	}
	if job.NodeID != "good" {
		t.Errorf("retry went to %s, want good", job.NodeID)
	}

	// While that move is running, later rounds don't queue another.
	hs.recoverFailed()
	var n int64
	db.Model(&models.Job{}).Where("type = ?", models.JobTypeDeploy).Count(&n)
	if n != 1 {
		t.Errorf("deploy jobs = %d, want 1", n)
	}
}

func TestRecoverFailed_PinnedProjectIsNotMoved(t *testing.T) {
	hs, _, db := newFailoverEnv(t)
	db.Model(&models.Project{}).Where("id = ?", "p1").Updates(map[string]interface{}{"status": "failed", "pinned": true})

	hs.recoverFailed()

	var n int64
	db.Model(&models.Job{}).Where("type = ?", models.JobTypeDeploy).Count(&n)
	if n != 0 {
		t.Errorf("a pinned project was moved (%d deploy jobs)", n)
	}
}

// "answers again on good" after a failover reads as if the app never left; say it failed over, and from where.
func TestRecoveredEvent_SaysFailedOverWhenMoved(t *testing.T) {
	hs, _, db := newFailoverEnv(t)
	since := time.Now().Add(-2 * time.Minute)
	db.Model(&models.Project{}).Where("id = ?", "p1").Update("node_id", "good")
	db.Create(&models.Migration{ID: "m", ProjectID: "p1", ContainerID: "c", SourceNodeID: "dead", TargetNodeID: "good",
		Status: models.MigrationStatusCompleted, CreatedAt: since.Add(30 * time.Second)})
	p := loadProject(db)

	ev := hs.recoveredEvent(&p, since, true, false)
	if ev.Title != "api failed over to good" {
		t.Errorf("title = %q", ev.Title)
	}
	if !strings.Contains(ev.Message, "stopped answering on dead, so it was moved to good") || !strings.Contains(ev.Message, "down") {
		t.Errorf("message = %q", ev.Message)
	}

	// A move from before this outage is not this outage's story.
	ev = hs.recoveredEvent(&p, since.Add(time.Minute), true, false)
	if ev.Title != "api is back up" || !strings.HasPrefix(ev.Message, "api answers again on good") {
		t.Errorf("no move in this outage: %q / %q", ev.Title, ev.Message)
	}
}

func TestStuckMigration_UnclaimedJobGivesUpAfterAMinute(t *testing.T) {
	_, _, db := newFailoverEnv(t)
	ms := NewMigrationService(db, nil, nil, NewDeployer(db))
	old := time.Now().Add(-90 * time.Second)
	db.Create(&models.Job{ID: "unclaimed", NodeID: "good", Type: models.JobTypeDeploy, Status: models.JobStatusPending, CreatedAt: old})
	db.Create(&models.Job{ID: "busy", NodeID: "ok", Type: models.JobTypeDeploy, Status: models.JobStatusRunning, CreatedAt: old})
	db.Create(&models.Migration{ID: "m1", ProjectID: "p1", ContainerID: "c", SourceNodeID: "dead", TargetNodeID: "good",
		Status: models.MigrationStatusRunning, JobID: "unclaimed", CreatedAt: old})
	db.Create(&models.Migration{ID: "m2", ProjectID: "p1", ContainerID: "c", SourceNodeID: "dead", TargetNodeID: "ok",
		Status: models.MigrationStatusRunning, JobID: "busy", CreatedAt: old})

	ms.cleanupStuckMigrations()

	var m1, m2 models.Migration
	db.First(&m1, "id = ?", "m1")
	db.First(&m2, "id = ?", "m2")
	var j models.Job
	db.First(&j, "id = ?", "unclaimed")
	if m1.Status != models.MigrationStatusFailed || j.Status != models.JobStatusCancelled {
		t.Errorf("unclaimed after 90s: migration %q job %q, want failed/cancelled", m1.Status, j.Status)
	}
	if m2.Status != models.MigrationStatusRunning {
		t.Errorf("a job the target is running gets 5 minutes, migration = %q", m2.Status)
	}
}
