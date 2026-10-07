package services

import (
	"net"
	"strings"
	"testing"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

func TestTerminalReachable(t *testing.T) {
	db := testutil.NewDB(t, &models.NodeSSHKey{})
	s := NewNodeOpsService(db, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	openPort := ln.Addr().(*net.TCPAddr).Port
	// A port that was open a moment ago and is now closed: connection refused
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	closedPort := closed.Addr().(*net.TCPAddr).Port
	closed.Close()
	defer ln.Close()

	db.Create(&models.NodeSSHKey{ID: "k1", NodeID: "up", SSHPort: openPort, SSHUser: "u", PublicKey: "p", PrivateKey: "x"})
	db.Create(&models.NodeSSHKey{ID: "k2", NodeID: "nossh", SSHPort: closedPort, SSHUser: "u", PublicKey: "p", PrivateKey: "x"})

	if port, msg := s.terminalReachable(models.Node{ID: "up", VPNIP: "127.0.0.1"}); msg != "" || port != openPort {
		t.Errorf("listening node: port %d, %q", port, msg)
	}
	if _, msg := s.terminalReachable(models.Node{ID: "nossh", VPNIP: "127.0.0.1"}); !strings.Contains(msg, "no SSH server listening") {
		t.Errorf("node without SSH server: %q", msg)
	}
	if _, msg := s.terminalReachable(models.Node{ID: "nokey", VPNIP: "127.0.0.1"}); msg != "no SSH key for this node" {
		t.Errorf("node without key: %q", msg)
	}
}
