package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

func newJobTestService(t *testing.T) (*JobService, *gorm.DB) {
	db := testutil.NewDB(t, &models.Node{}, &models.Job{}, &models.Migration{})
	return NewJobService(db), db
}

func newTestContext(method, url, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c, w
}

func TestJobClaim_ReturnsOldestPendingJob(t *testing.T) {
	svc, db := newJobTestService(t)
	db.Create(&models.Node{ID: "n1", Hostname: "mac-mini", VPNIP: "10.100.0.2"})
	db.Create(&models.Job{ID: "j1", NodeID: "n1", Type: models.JobTypeCommand, Status: models.JobStatusPending, Command: "echo hi"})

	c, w := newTestContext(http.MethodPost, "/jobs/claim?node_id=n1", "")
	svc.Claim(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with a pending job, got %d: %s", w.Code, w.Body.String())
	}

	var job models.Job
	db.First(&job, "id = ?", "j1")
	if job.Status != models.JobStatusRunning {
		t.Fatalf("expected claimed job to move to running, got %s", job.Status)
	}
	if job.StartedAt == nil {
		t.Fatal("expected StartedAt to be set once claimed")
	}
}

func TestJobClaim_NoJobsReturnsNoContent(t *testing.T) {
	svc, db := newJobTestService(t)
	db.Create(&models.Node{ID: "n1", Hostname: "mac-mini", VPNIP: "10.100.0.2"})

	c, w := newTestContext(http.MethodPost, "/jobs/claim?node_id=n1", "")
	svc.Claim(c)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 with no pending jobs, got %d", w.Code)
	}
}

func TestJobClaim_FallsBackToVPNIPWhenNodeIDMissing(t *testing.T) {
	svc, db := newJobTestService(t)
	db.Create(&models.Node{ID: "n1", Hostname: "mac-mini", VPNIP: "10.100.0.2"})
	db.Create(&models.Job{ID: "j1", NodeID: "n1", Type: models.JobTypeCommand, Status: models.JobStatusPending, Command: "echo hi"})

	c, w := newTestContext(http.MethodPost, "/jobs/claim", "")
	c.Set("vpn_ip", "10.100.0.2")
	svc.Claim(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 resolving node by vpn_ip, got %d: %s", w.Code, w.Body.String())
	}
}

func TestJobClaim_UnknownNodeNotFound(t *testing.T) {
	svc, _ := newJobTestService(t)

	c, w := newTestContext(http.MethodPost, "/jobs/claim?node_id=ghost", "")
	svc.Claim(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown node, got %d", w.Code)
	}
}

func TestJobComplete_UpdatesStatusAndLogs(t *testing.T) {
	svc, db := newJobTestService(t)
	db.Create(&models.Node{ID: "n1", Hostname: "mac-mini", VPNIP: "10.100.0.2"})
	db.Create(&models.Job{ID: "j1", NodeID: "n1", Type: models.JobTypeCommand, Status: models.JobStatusRunning, Command: "echo hi"})

	c, w := newTestContext(http.MethodPost, "/jobs/j1/complete", `{"status":"completed","logs":"hi\n","exit_code":0}`)
	c.Params = gin.Params{{Key: "id", Value: "j1"}}
	c.Set("vpn_ip", "10.100.0.2")
	svc.Complete(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var job models.Job
	db.First(&job, "id = ?", "j1")
	if job.Status != "completed" || job.Logs != "hi\n" {
		t.Fatalf("unexpected job after complete: %+v", job)
	}
	if job.CompletedAt == nil {
		t.Fatal("expected CompletedAt to be set")
	}
}

func TestJobComplete_RejectsWrongNode(t *testing.T) {
	svc, db := newJobTestService(t)
	db.Create(&models.Node{ID: "n1", Hostname: "mac-mini", VPNIP: "10.100.0.2"})
	db.Create(&models.Job{ID: "j1", NodeID: "n1", Type: models.JobTypeCommand, Status: models.JobStatusRunning, Command: "echo hi"})

	c, w := newTestContext(http.MethodPost, "/jobs/j1/complete", `{"status":"completed"}`)
	c.Params = gin.Params{{Key: "id", Value: "j1"}}
	c.Set("vpn_ip", "10.999.0.99") // not this job's node

	svc.Complete(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when reporting IP doesn't match the job's node, got %d", w.Code)
	}
}

func TestJobComplete_UnknownJobNotFound(t *testing.T) {
	svc, _ := newJobTestService(t)

	c, w := newTestContext(http.MethodPost, "/jobs/ghost/complete", `{"status":"completed"}`)
	c.Params = gin.Params{{Key: "id", Value: "ghost"}}

	svc.Complete(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown job, got %d", w.Code)
	}
}

func TestJobCreate_QueuesPendingJob(t *testing.T) {
	svc, db := newJobTestService(t)
	db.Create(&models.Node{ID: "n1", Hostname: "mac-mini", VPNIP: "10.100.0.2"})

	c, w := newTestContext(http.MethodPost, "/jobs", `{"node_id":"n1","type":"command","command":"df -h"}`)
	svc.Create(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var jobs []models.Job
	db.Find(&jobs)
	if len(jobs) != 1 || jobs[0].Status != models.JobStatusPending {
		t.Fatalf("expected 1 pending job, got %+v", jobs)
	}
}

func TestJobCreate_UnknownNodeRejected(t *testing.T) {
	svc, _ := newJobTestService(t)

	c, w := newTestContext(http.MethodPost, "/jobs", `{"node_id":"ghost","type":"command","command":"df -h"}`)
	svc.Create(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown node, got %d", w.Code)
	}
}
