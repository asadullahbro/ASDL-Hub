package services

import (
	"fmt"
	"io"
	"log"
	"time"

	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

type TerminalService struct {
	db            *gorm.DB
	enrollmentSvc *EnrollmentService
}

func NewTerminalService(db *gorm.DB, enrollmentSvc *EnrollmentService) *TerminalService {
	return &TerminalService{db: db, enrollmentSvc: enrollmentSvc}
}

type SSHSession struct {
	Client  *ssh.Client
	Session *ssh.Session
	Stdin   io.WriteCloser
	Stdout  io.Reader
	Stderr  io.Reader
}

func (s *TerminalService) GetSSHConfig(nodeID string) (*models.NodeSSHKey, *models.Node, error) {
	var node models.Node
	if err := s.db.First(&node, "id = ?", nodeID).Error; err != nil {
		return nil, nil, fmt.Errorf("node not found")
	}

	var sshKey models.NodeSSHKey
	if err := s.db.First(&sshKey, "node_id = ?", nodeID).Error; err != nil {
		return nil, nil, fmt.Errorf("no SSH key found for node — was it enrolled via the install script?")
	}

	return &sshKey, &node, nil
}

func (s *TerminalService) OpenSession(nodeID string) (*SSHSession, error) {
	sshKey, node, err := s.GetSSHConfig(nodeID)
	if err != nil {
		return nil, err
	}

	// Decrypt private key
	privKeyPEM, err := s.enrollmentSvc.Decrypt(sshKey.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt SSH key: %v", err)
	}

	// Parse private key
	signer, err := ssh.ParsePrivateKey([]byte(privKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("failed to parse SSH key: %v", err)
	}

	// Connect
	addr := fmt.Sprintf("%s:%d", node.VPNIP, sshKey.SSHPort)
	config := &ssh.ClientConfig{
		User: sshKey.SSHUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // mesh-internal, acceptable
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("SSH dial failed to %s: %v", addr, err)
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("SSH session failed: %v", err)
	}

	// Request PTY
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", 40, 160, modes); err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("PTY request failed: %v", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	if err := session.Shell(); err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("shell start failed: %v", err)
	}

	log.Printf("🖥️ SSH session opened: node=%s user=%s addr=%s", nodeID, sshKey.SSHUser, addr)

	return &SSHSession{
		Client:  client,
		Session: session,
		Stdin:   stdin,
		Stdout:  stdout,
		Stderr:  stderr,
	}, nil
}

func (s *TerminalService) ResizeTerminal(session *ssh.Session, rows, cols uint32) error {
	return session.WindowChange(int(rows), int(cols))
}
