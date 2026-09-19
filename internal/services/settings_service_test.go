package services

import (
	"testing"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

func newSettingsTestService(t *testing.T) *SettingsService {
	db := testutil.NewDB(t, &models.User{}, &models.Node{}, &models.Setting{}, &models.PermanentToken{})
	return NewSettingsService(db, NewAuthService(db, "test-secret"), "test-secret")
}

func TestMasterNode_SetGetClear(t *testing.T) {
	svc := newSettingsTestService(t)

	node := &models.Node{ID: "node1", Hostname: "mac-mini", VPNIP: "10.100.0.5"}
	if err := svc.db.Create(node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}

	if err := svc.SetMasterNode("node1"); err != nil {
		t.Fatalf("expected set master node to succeed, got error: %v", err)
	}
	if got := svc.GetMasterNodeID(); got != "node1" {
		t.Fatalf("expected master node id node1, got %s", got)
	}

	if err := svc.ClearMasterNode(); err != nil {
		t.Fatalf("expected clear to succeed, got error: %v", err)
	}
	if got := svc.GetMasterNodeID(); got != "" {
		t.Fatalf("expected empty master node id after clear, got %s", got)
	}
}

func TestMasterNode_RejectsUnknownNode(t *testing.T) {
	svc := newSettingsTestService(t)

	if err := svc.SetMasterNode("does-not-exist"); err == nil {
		t.Fatal("expected setting an unknown node as master to fail")
	}
}
