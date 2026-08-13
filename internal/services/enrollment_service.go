package services

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

type EnrollmentService struct {
	db            *gorm.DB
	wireGuard     *WireGuardService
	encryptionKey []byte
	hubPort       int
}

func NewEnrollmentService(db *gorm.DB, wg *WireGuardService, jwtSecret string, hubPort int) *EnrollmentService {
	key := make([]byte, 32)
	copy(key, []byte(jwtSecret))
	return &EnrollmentService{
		db:            db,
		wireGuard:     wg,
		encryptionKey: key,
		hubPort:       hubPort,
	}
}

// CreateToken generates a new enrollment token
func (s *EnrollmentService) CreateToken(label, createdBy string) (*models.EnrollmentToken, error) {
	token := &models.EnrollmentToken{
		ID:        uuid.New().String(),
		Token:     GenerateSecureToken(32),
		Label:     label,
		CreatedBy: createdBy,
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}
	if err := s.db.Create(token).Error; err != nil {
		return nil, err
	}
	return token, nil
}

func (s *EnrollmentService) ListTokens() ([]models.EnrollmentToken, error) {
	var tokens []models.EnrollmentToken
	if err := s.db.Order("created_at DESC").Find(&tokens).Error; err != nil {
		return nil, err
	}
	return tokens, nil
}

func (s *EnrollmentService) RevokeToken(id string) error {
	return s.db.Delete(&models.EnrollmentToken{}, "id = ?", id).Error
}

// Enroll processes a node enrollment request
func (s *EnrollmentService) Enroll(req EnrollRequest) (*EnrollResponse, error) {
	// Validate token
	var token models.EnrollmentToken
	if err := s.db.Where("token = ? AND used = ? AND expires_at > ?",
		req.Token, false, time.Now()).First(&token).Error; err != nil {
		return nil, errors.New("invalid or expired enrollment token")
	}

	// Allocate WireGuard IP
	assignedIP, err := s.wireGuard.AllocateIP()
	if err != nil {
		return nil, err
	}

	// Add peer to WireGuard
	if err := s.wireGuard.AddPeer(req.WireGuardPublicKey, assignedIP); err != nil {
		return nil, err
	}

	// Store WireGuard peer
	peer := &models.WireGuardPeer{
		ID:         uuid.New().String(),
		PublicKey:  req.WireGuardPublicKey,
		AssignedIP: assignedIP,
		CreatedAt:  time.Now(),
	}

	// Generate SSH keypair for this node
	privPEM, pubAuth, err := s.wireGuard.GenerateSSHKeypair()
	if err != nil {
		log.Printf("⚠️ SSH keypair generation failed: %v", err)
		// Non-fatal — SSH terminal won't work but enrollment continues
	}

	// Encrypt private key before storing
	encryptedPriv, err := s.encrypt(privPEM)
	if err != nil {
		log.Printf("⚠️ SSH key encryption failed: %v", err)
	}

	// Mark token as used
	now := time.Now()
	token.Used = true
	token.UsedAt = &now
	s.db.Save(&token)

	// Create node record
	node := &models.Node{
		ID:            uuid.New().String(),
		Hostname:      req.Hostname,
		VPNIP:         assignedIP,
		OS:            req.OS,
		Architecture:  req.Architecture,
		CPU:           req.CPU,
		MemoryTotal:   req.MemoryTotal,
		DiskTotal:     req.DiskTotal,
		Online:        false, // will come online once WireGuard connects
		Capabilities:  req.Capabilities,
		LastHeartbeat: time.Now(),
	}
	if err := s.db.Create(node).Error; err != nil {
		return nil, err
	}

	peer.NodeID = node.ID
	s.db.Create(peer)
	token.UsedBy = node.ID
	s.db.Save(&token)

	// Store SSH key
	if encryptedPriv != "" && pubAuth != "" {
		sshKey := &models.NodeSSHKey{
			ID:         uuid.New().String(),
			NodeID:     node.ID,
			PublicKey:  pubAuth,
			PrivateKey: encryptedPriv,
			SSHUser:    req.SSHUser,
			SSHPort:    22,
			CreatedAt:  time.Now(),
		}
		s.db.Create(sshKey)
	}

	log.Printf("✅ Node enrolled: %s assigned %s", req.Hostname, assignedIP)

	return &EnrollResponse{
		NodeID:               node.ID,
		AssignedIP:           assignedIP,
		HubWireGuardPubKey:   WireGuardHubPubKey,
		HubWireGuardEndpoint: WireGuardHubEndpoint,
		SSHPublicKey:         pubAuth, // agent adds this to authorized_keys
		HubVPNIP:             WireGuardHubIP,
		HubPort:              s.hubPort,
		WireGuardNetwork:     WireGuardNetwork,
	}, nil
}

type EnrollRequest struct {
	Token              string   `json:"token" binding:"required"`
	Hostname           string   `json:"hostname" binding:"required"`
	WireGuardPublicKey string   `json:"wireguard_public_key" binding:"required"`
	OS                 string   `json:"os"`
	Architecture       string   `json:"arch"`
	CPU                int      `json:"cpu"`
	MemoryTotal        int64    `json:"memory_total"`
	DiskTotal          int64    `json:"disk_total"`
	Capabilities       []string `json:"capabilities"`
	SSHUser            string   `json:"ssh_user"`
}

type EnrollResponse struct {
	NodeID               string `json:"node_id"`
	AssignedIP           string `json:"assigned_ip"`
	HubWireGuardPubKey   string `json:"hub_wireguard_public_key"`
	HubWireGuardEndpoint string `json:"hub_wireguard_endpoint"`
	SSHPublicKey         string `json:"ssh_public_key"`
	HubVPNIP             string `json:"hub_vpn_ip"`
	HubPort              int    `json:"hub_port"`
	WireGuardNetwork     string `json:"wireguard_network"`
}

// encrypt encrypts a string using AES-GCM
func (s *EnrollmentService) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt decrypts an AES-GCM encrypted string — used by SSH terminal service
func (s *EnrollmentService) Decrypt(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, sealed := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// helpers for RSA key generation
func generateRSAKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 4096)
}

func encodePrivateKeyToPEM(key *rsa.PrivateKey) string {
	privDER := x509.MarshalPKCS1PrivateKey(key)
	privBlock := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privDER}
	return string(pem.EncodeToMemory(privBlock))
}
