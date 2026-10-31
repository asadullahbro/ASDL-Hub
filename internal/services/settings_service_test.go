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

func TestCreateUser_AfterDeletingSameName(t *testing.T) {
	svc := newSettingsTestService(t)
	if err := svc.db.AutoMigrate(&models.Session{}, &models.Passkey{}); err != nil {
		t.Fatal(err)
	}
	admin, err := svc.CreateUser("boss", "boss@example.com", "pw-boss-1", models.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	v, err := svc.CreateUser("vic", "vic@example.com", "pw-vic-1", models.RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteUser(v.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateUser("vic", "vic@example.com", "pw-vic-2", models.RoleViewer); err != nil {
		t.Fatalf("recreating a deleted user failed: %v", err)
	}
	// A row soft-deleted by an older version must not block the name either.
	old := &models.User{ID: "old", Username: "zed", Email: "zed@example.com", Password: "x", Role: models.RoleViewer}
	svc.db.Create(old)
	svc.db.Delete(old)
	if _, err := svc.CreateUser("zed", "zed@example.com", "pw-zed-1", models.RoleViewer); err != nil {
		t.Fatalf("creating over a soft-deleted leftover failed: %v", err)
	}
}
