package services

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

func newNodeTestService(t *testing.T) (*NodeService, *gorm.DB) {
	db := testutil.NewDB(t, &models.Node{})
	return NewNodeService(db), db
}

func TestNodeRegister_CreatesNewNode(t *testing.T) {
	svc, db := newNodeTestService(t)

	c, w := newTestContext(http.MethodPost, "/nodes", `{"hostname":"mac-mini","os":"darwin","cpu":8}`)
	c.Set("vpn_ip", "10.100.0.2")
	svc.Register(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for a new node, got %d: %s", w.Code, w.Body.String())
	}

	var nodes []models.Node
	db.Find(&nodes)
	if len(nodes) != 1 || nodes[0].VPNIP != "10.100.0.2" || nodes[0].Hostname != "mac-mini" {
		t.Fatalf("unexpected nodes after register: %+v", nodes)
	}
	if !nodes[0].Online {
		t.Fatal("expected newly registered node to be marked online")
	}
}

func TestNodeRegister_ReRegisterUpdatesExistingNodeByVPNIP(t *testing.T) {
	svc, db := newNodeTestService(t)
	db.Create(&models.Node{ID: "n1", Hostname: "old-name", VPNIP: "10.100.0.2", Online: false})

	c, w := newTestContext(http.MethodPost, "/nodes", `{"hostname":"new-name","os":"linux"}`)
	c.Set("vpn_ip", "10.100.0.2")
	svc.Register(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for re-registering an existing node, got %d: %s", w.Code, w.Body.String())
	}

	var nodes []models.Node
	db.Find(&nodes)
	if len(nodes) != 1 {
		t.Fatalf("expected re-registration to update in place, not create a second row, got %d nodes", len(nodes))
	}
	if nodes[0].Hostname != "new-name" || !nodes[0].Online {
		t.Fatalf("expected node to be updated and online, got %+v", nodes[0])
	}
}

func TestNodeRegister_RejectsMissingVPNIP(t *testing.T) {
	svc, _ := newNodeTestService(t)

	gin.SetMode(gin.TestMode)
	c, w := newTestContext(http.MethodPost, "/nodes", `{"hostname":"mac-mini"}`)
	// No vpn_ip in context and ClientIP() is empty for a bare httptest request
	// with no RemoteAddr set — Register must reject rather than silently
	// registering a node with no identity.
	c.Request.RemoteAddr = ""
	svc.Register(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no vpn_ip can be determined, got %d: %s", w.Code, w.Body.String())
	}
}

func TestNodeHeartbeat_MarksNodeOnlineAndUpdatesMetrics(t *testing.T) {
	svc, db := newNodeTestService(t)
	db.Create(&models.Node{ID: "n1", Hostname: "mac-mini", VPNIP: "10.100.0.2", Online: false})

	c, w := newTestContext(http.MethodPost, "/nodes/n1/heartbeat", `{"cpu_percent":12.5,"uptime":100}`)
	c.Params = gin.Params{{Key: "id", Value: "n1"}}
	c.Set("vpn_ip", "10.100.0.2")
	svc.Heartbeat(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var node models.Node
	db.First(&node, "id = ?", "n1")
	if !node.Online {
		t.Fatal("expected heartbeat to mark node online")
	}
	if node.Uptime != 100 {
		t.Fatalf("expected uptime to be updated, got %d", node.Uptime)
	}
}

func TestNodeHeartbeat_UnknownNodeNotFound(t *testing.T) {
	svc, _ := newNodeTestService(t)

	c, w := newTestContext(http.MethodPost, "/nodes/ghost/heartbeat", `{}`)
	c.Params = gin.Params{{Key: "id", Value: "ghost"}}
	c.Set("vpn_ip", "10.100.0.99")
	svc.Heartbeat(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown node, got %d", w.Code)
	}
}
