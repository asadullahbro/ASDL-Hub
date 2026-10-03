package services

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

func TestRemoveNode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewDB(t, &models.Node{}, &models.Project{}, &models.WireGuardPeer{}, &models.NodeSSHKey{},
		&models.Heartbeat{}, &models.Job{}, &models.Setting{})
	db.Create(&models.Node{ID: "busy", Hostname: "busy", VPNIP: "10.0.0.2"})
	db.Create(&models.Node{ID: "old", Hostname: "old", VPNIP: "10.0.0.3"})
	db.Create(&models.Project{ID: "p1", Name: "api", NodeID: "busy"})
	db.Create(&models.WireGuardPeer{ID: "w1", NodeID: "old", PublicKey: "pk-old", AssignedIP: "10.0.0.3"})
	db.Create(&models.Job{ID: "j1", NodeID: "old", Status: models.JobStatusPending, CreatedAt: time.Now()})
	db.Create(&models.Job{ID: "j2", NodeID: "old", Status: models.JobStatusCompleted, CreatedAt: time.Now()})
	db.Create(&models.Setting{Key: "master_node_id", Value: "old"})

	s := NewNodeService(db)
	var removed []string
	r := gin.New()
	r.DELETE("/nodes/:id", func(c *gin.Context) {
		s.Remove(c, func(pk string) error { removed = append(removed, pk); return nil })
	})
	do := func(id string) int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/nodes/"+id, nil))
		return w.Code
	}

	if code := do("busy"); code != http.StatusConflict {
		t.Errorf("node with an app: %d, want 409", code)
	}
	if code := do("old"); code != http.StatusOK {
		t.Fatalf("remove: %d", code)
	}
	var n int64
	db.Model(&models.Node{}).Where("id = ?", "old").Count(&n)
	if n != 0 || len(removed) != 1 || removed[0] != "pk-old" {
		t.Errorf("node left: %d, peers removed: %v", n, removed)
	}
	db.Model(&models.Job{}).Where("node_id = ?", "old").Count(&n)
	if n != 1 {
		t.Errorf("want only the finished job kept, have %d", n)
	}
	db.Model(&models.Setting{}).Where("key = ?", "master_node_id").Count(&n)
	if n != 0 {
		t.Error("master node setting still points at the removed node")
	}
	if code := do("nope"); code != http.StatusNotFound {
		t.Errorf("unknown node: %d", code)
	}
}

func TestRollback_OnlyByTheEnrollingInstaller(t *testing.T) {
	db := testutil.NewDB(t, &models.Node{}, &models.EnrollmentToken{}, &models.WireGuardPeer{}, &models.NodeSSHKey{})
	used := time.Now()
	old := time.Now().Add(-2 * time.Hour)
	db.Create(&models.Node{ID: "n1", Hostname: "new", VPNIP: "10.0.0.5"})
	db.Create(&models.Node{ID: "n2", Hostname: "established", VPNIP: "10.0.0.6"})
	db.Create(&models.EnrollmentToken{ID: "t1", Token: "tok1", Used: true, UsedBy: "n1", UsedAt: &used, ExpiresAt: used})
	db.Create(&models.EnrollmentToken{ID: "t2", Token: "tok2", Used: true, UsedBy: "n2", UsedAt: &old, ExpiresAt: old})
	s := &EnrollmentService{db: db, wireGuard: &WireGuardService{}}

	for _, c := range []struct{ node, token string }{{"n1", ""}, {"n1", "tok2"}, {"n2", "tok2"}} {
		if err := s.Rollback(c.node, c.token); err != ErrRollbackDenied {
			t.Errorf("rollback of %s with %q: %v, want denied", c.node, c.token, err)
		}
	}
	if err := s.Rollback("n1", "tok1"); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&models.Node{}).Count(&n)
	if n != 1 {
		t.Errorf("%d nodes left, want 1", n)
	}
}
