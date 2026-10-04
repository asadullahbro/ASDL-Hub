package services

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

var (
	WireGuardInterface   = getEnvOrDefault("WG_INTERFACE", "asdl0")
	WireGuardCIDR        = getEnvOrDefault("WG_CIDR", "10.100.0.0/24")
	WireGuardHubIP       = getEnvOrDefault("WG_HUB_IP", "10.100.0.1")
	WireGuardHubPubKey   = os.Getenv("WG_HUB_PUBKEY")
	WireGuardHubEndpoint = os.Getenv("WG_ENDPOINT")
	WireGuardStartIP     = getEnvOrDefault("WG_START_IP", "10.100.0.2")
	WireGuardNetwork     = getEnvOrDefault("WG_NETWORK", "10.100.0.0/24")
)

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

type WireGuardService struct {
	db *gorm.DB
}

func (s *WireGuardService) DB() *gorm.DB {
	return s.db
}

func NewWireGuardService(db *gorm.DB) *WireGuardService {
	if WireGuardHubPubKey == "" {
		log.Println("⚠️  WG_HUB_PUBKEY not set — enrollment will fail")
	}
	if WireGuardHubEndpoint == "" {
		log.Println("⚠️  WG_ENDPOINT not set — nodes won't know where to connect")
	}
	return &WireGuardService{db: db}
}

// AllocateIP finds the next free IP in the pool
func (s *WireGuardService) AllocateIP() (string, error) {
	var peers []models.WireGuardPeer
	s.db.Find(&peers)

	used := map[string]bool{
		WireGuardHubIP: true,
	}
	for _, p := range peers {
		used[p.AssignedIP] = true
	}
	// Node rows hold a unique vpn_ip too, soft-deleted ones included
	var nodeIPs []string
	s.db.Unscoped().Model(&models.Node{}).Pluck("vpn_ip", &nodeIPs)
	for _, ip := range nodeIPs {
		used[ip] = true
	}

	_, network, _ := net.ParseCIDR(WireGuardCIDR)
	ip := net.ParseIP(WireGuardStartIP).To4()

	for ip[3] < 255 {
		candidate := ip.String()
		if network.Contains(ip) && !used[candidate] {
			return candidate, nil
		}
		ip[3]++
	}
	return "", errors.New("IP pool exhausted")
}

// AddPeer calls `wg set` to add a new peer live, then persists to config
func (s *WireGuardService) AddPeer(publicKey, assignedIP string) error {
	cmd := exec.Command("wg", "set", WireGuardInterface,
		"peer", publicKey,
		"allowed-ips", assignedIP+"/32",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("wg set failed: %v — %s", err, string(out))
	}

	s.saveConfig()

	log.Printf("✅ WireGuard peer added: %s → %s", publicKey[:8]+"...", assignedIP)
	return nil
}

// RemovePeer removes a peer from WireGuard
func (s *WireGuardService) RemovePeer(publicKey string) error {
	cmd := exec.Command("wg", "set", WireGuardInterface,
		"peer", publicKey, "remove",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("wg peer remove failed: %v — %s", err, string(out))
	}

	s.saveConfig()

	log.Printf("🗑️ WireGuard peer removed: %s", publicKey[:8]+"...")
	return nil
}

// RemoveNodePeers removes a node's peer and any other live peer still
// routed to its VPN IP (left behind when the database and the interface
// drifted apart), then saves the interface config once.
func (s *WireGuardService) RemoveNodePeers(publicKey, vpnIP string) error {
	keys := map[string]bool{}
	if publicKey != "" {
		keys[publicKey] = true
	}
	if vpnIP != "" {
		out, err := exec.Command("wg", "show", WireGuardInterface, "dump").Output()
		if err != nil {
			log.Printf("⚠️ wg show dump failed: %v", err)
		}
		// First line is the interface; peer lines are
		// public-key, preshared-key, endpoint, allowed-ips, ...
		for i, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fields := strings.Split(line, "\t")
			if i == 0 || len(fields) < 4 {
				continue
			}
			for _, allowed := range strings.Split(fields[3], ",") {
				if allowed == vpnIP+"/32" {
					keys[fields[0]] = true
				}
			}
		}
	}

	var errs []error
	for key := range keys {
		out, err := exec.Command("wg", "set", WireGuardInterface, "peer", key, "remove").CombinedOutput()
		if err != nil {
			errs = append(errs, fmt.Errorf("wg peer remove %s: %v — %s", key[:8], err, string(out)))
			continue
		}
		log.Printf("🗑️ WireGuard peer removed: %s", key[:8]+"...")
	}
	s.saveConfig()
	return errors.Join(errs...)
}

// saveConfig writes the live interface to /etc/wireguard so peer changes
// survive a restart. wg-quick needs root to write there; the installer
// allows exactly this command in /etc/sudoers.d/asdl-hub.
func (s *WireGuardService) saveConfig() {
	if out, err := exec.Command("sudo", "-n", "wg-quick", "save", WireGuardInterface).CombinedOutput(); err != nil {
		log.Printf("⚠️ wg-quick save failed, peer changes won't survive a restart: %v — %s", err, string(out))
	}
}

// GenerateSSHKeypair generates a new RSA keypair for SSH terminal access
func (s *WireGuardService) GenerateSSHKeypair() (privateKeyPEM string, publicKeyAuthorized string, err error) {
	privateKey, err := generateRSAKey()
	if err != nil {
		return "", "", err
	}

	pubKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", err
	}

	privPEM := encodePrivateKeyToPEM(privateKey)
	pubAuth := string(ssh.MarshalAuthorizedKey(pubKey))

	return privPEM, pubAuth, nil
}

// Status returns current wg show output
func (s *WireGuardService) Status() (string, error) {
	cmd := exec.Command("wg", "show", WireGuardInterface)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("wg show failed: %v", err)
	}
	return string(out), nil
}

func GenerateSecureToken(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)[:length]
}
