package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

func newSiriTestHandler(t *testing.T) (*SiriHandler, *gorm.DB) {
	db := testutil.NewDB(t, &models.Node{}, &models.Job{})
	return NewSiriHandler(db), db
}

func newJSONContext(method, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c, w
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	return out
}

func TestSiriNodes_ReturnsSortedHostnames(t *testing.T) {
	h, db := newSiriTestHandler(t)
	db.Create(&models.Node{ID: "n1", Hostname: "nas", VPNIP: "10.100.0.3"})
	db.Create(&models.Node{ID: "n2", Hostname: "mac-mini", VPNIP: "10.100.0.2"})

	c, w := newJSONContext(http.MethodGet, "")
	h.Nodes(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var hostnames []string
	if err := json.Unmarshal(w.Body.Bytes(), &hostnames); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	if len(hostnames) != 2 || hostnames[0] != "mac-mini" || hostnames[1] != "nas" {
		t.Fatalf("expected [mac-mini nas], got %v", hostnames)
	}
}

func TestSiriNodes_EmptyWhenNoNodes(t *testing.T) {
	h, _ := newSiriTestHandler(t)

	c, w := newJSONContext(http.MethodGet, "")
	h.Nodes(c)

	var hostnames []string
	if err := json.Unmarshal(w.Body.Bytes(), &hostnames); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	if len(hostnames) != 0 {
		t.Fatalf("expected empty list, got %v", hostnames)
	}
}

func TestSiriHealth_AllOnline(t *testing.T) {
	h, db := newSiriTestHandler(t)
	db.Create(&models.Node{ID: "n1", Hostname: "mac-mini", VPNIP: "10.100.0.2", Online: true})
	db.Create(&models.Node{ID: "n2", Hostname: "nas", VPNIP: "10.100.0.3", Online: true})

	c, w := newJSONContext(http.MethodGet, "")
	h.Health(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["online"].(float64) != 2 {
		t.Fatalf("expected 2 online, got %v", body["online"])
	}
	if body["message"] != "2 of 2 nodes online." {
		t.Fatalf("unexpected message: %v", body["message"])
	}
}

func TestSiriHealth_ReportsOfflineNodes(t *testing.T) {
	h, db := newSiriTestHandler(t)
	db.Create(&models.Node{ID: "n1", Hostname: "mac-mini", VPNIP: "10.100.0.2", Online: true})
	db.Create(&models.Node{ID: "n2", Hostname: "nas", VPNIP: "10.100.0.3", Online: false})

	c, w := newJSONContext(http.MethodGet, "")
	h.Health(c)

	body := decodeJSON(t, w)
	msg := body["message"].(string)
	if !strings.Contains(msg, "1 of 2 nodes online") || !strings.Contains(msg, "nas") {
		t.Fatalf("expected message to mention offline node nas, got %q", msg)
	}
}

func TestSiriHealth_NoNodesEnrolled(t *testing.T) {
	h, _ := newSiriTestHandler(t)

	c, w := newJSONContext(http.MethodGet, "")
	h.Health(c)

	body := decodeJSON(t, w)
	if body["message"] != "No nodes are enrolled yet." {
		t.Fatalf("unexpected message: %v", body["message"])
	}
}

func TestSiriRun_QueuesJobForExistingNode(t *testing.T) {
	h, db := newSiriTestHandler(t)
	db.Create(&models.Node{ID: "n1", Hostname: "mac-mini", VPNIP: "10.100.0.2"})

	c, w := newJSONContext(http.MethodPost, `{"hostname":"MAC-MINI","command":"df -h"}`)
	h.Run(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var jobs []models.Job
	db.Find(&jobs)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job to be queued, got %d", len(jobs))
	}
	if jobs[0].NodeID != "n1" || jobs[0].Command != "df -h" {
		t.Fatalf("unexpected job: %+v", jobs[0])
	}
	if jobs[0].Status != models.JobStatusPending {
		t.Fatalf("expected job to start pending, got %s", jobs[0].Status)
	}
}

func TestSiriRun_HostnameLookupIsCaseInsensitive(t *testing.T) {
	h, db := newSiriTestHandler(t)
	db.Create(&models.Node{ID: "n1", Hostname: "Mac-Mini", VPNIP: "10.100.0.2"})

	c, w := newJSONContext(http.MethodPost, `{"hostname":"mac-mini","command":"uptime"}`)
	h.Run(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSiriRun_UnknownHostname(t *testing.T) {
	h, _ := newSiriTestHandler(t)

	c, w := newJSONContext(http.MethodPost, `{"hostname":"ghost","command":"uptime"}`)
	h.Run(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown hostname, got %d", w.Code)
	}
}

func TestSiriRun_MissingCommandRejected(t *testing.T) {
	h, db := newSiriTestHandler(t)
	db.Create(&models.Node{ID: "n1", Hostname: "mac-mini", VPNIP: "10.100.0.2"})

	c, w := newJSONContext(http.MethodPost, `{"hostname":"mac-mini"}`)
	h.Run(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when command is missing, got %d", w.Code)
	}
}

func TestSiriShutdown_QueuesShutdownCommand(t *testing.T) {
	h, db := newSiriTestHandler(t)
	db.Create(&models.Node{ID: "n1", Hostname: "mac-mini", VPNIP: "10.100.0.2", OS: "darwin"})

	c, w := newJSONContext(http.MethodPost, `{"hostname":"mac-mini"}`)
	h.Shutdown(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var jobs []models.Job
	db.Find(&jobs)
	if len(jobs) != 1 || !strings.Contains(jobs[0].Command, "shutdown") {
		t.Fatalf("expected a shutdown job to be queued, got %+v", jobs)
	}
}

func TestSiriShutdown_UnknownHostname(t *testing.T) {
	h, _ := newSiriTestHandler(t)

	c, w := newJSONContext(http.MethodPost, `{"hostname":"ghost"}`)
	h.Shutdown(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown hostname, got %d", w.Code)
	}
}
