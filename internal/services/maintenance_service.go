package services

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

// NodeOpsService covers what an operator does to a node as a whole:
// maintenance mode, and looking at or restarting the containers on it.
type NodeOpsService struct {
	db       *gorm.DB
	deployer *Deployer
}

func NewNodeOpsService(db *gorm.DB, deployer *Deployer) *NodeOpsService {
	return &NodeOpsService{db: db, deployer: deployer}
}

// MaintenanceResult says what happened to each app on the node.
type MaintenanceResult struct {
	Node   models.Node `json:"node"`
	Moving []string    `json:"moving"` // apps being moved to another node
	Stays  []string    `json:"stays"`  // apps left in place: no other node
}

// SetMaintenance turns maintenance on or off. Turning it on moves every app
// on the node to the healthiest other available node: the new copy starts
// first and the old one is removed once it runs, so the apps stay up. Apps
// with nowhere to go stay where they are. Turning it off moves nothing back.
func (s *NodeOpsService) SetMaintenance(node *models.Node, on bool, by string) (*MaintenanceResult, error) {
	res := &MaintenanceResult{Moving: []string{}, Stays: []string{}}
	if node.Maintenance != on {
		updates := map[string]interface{}{"maintenance": on, "maintenance_by": by, "maintenance_since": nil}
		if on {
			now := time.Now()
			updates["maintenance_since"] = &now
		} else {
			updates["maintenance_by"] = ""
		}
		if err := s.db.Model(node).Updates(updates).Error; err != nil {
			return nil, err
		}
		log.Printf("🔧 Node %s maintenance %s (by %s)", node.Hostname, map[bool]string{true: "on", false: "off"}[on], by)
	}
	s.db.First(node, "id = ?", node.ID)
	res.Node = *node
	if !on {
		return res, nil
	}

	var projects []models.Project
	s.db.Where("node_id = ? AND image <> ''", node.ID).Find(&projects)
	for i := range projects {
		p := &projects[i]
		var target models.Node
		if err := s.db.Scopes(models.Available).Where("id <> ?", node.ID).
			Order("health_score desc").First(&target).Error; err != nil {
			res.Stays = append(res.Stays, p.Name)
			log.Printf("⚠️ %s stays on %s during maintenance: no other node available", p.Name, node.Hostname)
			continue
		}
		if _, _, err := s.deployer.Dispatch(p, &target, p.Image, DeployMeta{
			Trigger:    TriggerMigration,
			Repository: p.Repository,
		}); err != nil {
			res.Stays = append(res.Stays, p.Name)
			log.Printf("⚠️ Could not move %s off %s: %v", p.Name, node.Hostname, err)
			continue
		}
		res.Moving = append(res.Moving, p.Name+" → "+target.Hostname)
	}
	return res, nil
}

// SetMaintenanceHandler handles PUT /nodes/:id/maintenance {"enabled": bool}
// from the dashboard.
func (s *NodeOpsService) SetMaintenanceHandler(c *gin.Context) {
	var node models.Node
	if err := s.db.First(&node, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}
	by := "a user"
	if u, ok := c.Get("user"); ok {
		if user, ok := u.(*models.User); ok {
			by = user.Username
		}
	}
	s.setMaintenance(c, &node, by)
}

// SetMaintenanceFromNode handles POST /nodes/:id/maintenance on the mesh:
// the node's own agent, identified by its VPN address, asking for it.
func (s *NodeOpsService) SetMaintenanceFromNode(c *gin.Context) {
	vpnIP, _ := c.Get("vpn_ip")
	var node models.Node
	if err := s.db.First(&node, "id = ? AND vpn_ip = ?", c.Param("id"), vpnIP).Error; err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "a node can only change its own maintenance mode"})
		return
	}
	s.setMaintenance(c, &node, "the node itself")
}

func (s *NodeOpsService) setMaintenance(c *gin.Context, node *models.Node, by string) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.BindJSON(&req); err != nil || req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": `send {"enabled": true|false}`})
		return
	}
	res, err := s.SetMaintenance(node, *req.Enabled, by)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

var containerNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// containerJob queues a shell command about one container on a node and
// returns the job; the dashboard follows it like any other job.
func (s *NodeOpsService) containerJob(c *gin.Context, command func(name string) string) {
	name := c.Param("name")
	if !containerNameRe.MatchString(name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid container name"})
		return
	}
	var node models.Node
	if err := s.db.First(&node, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}
	if !node.Online {
		c.JSON(http.StatusConflict, gin.H{"error": "node " + node.Hostname + " is offline"})
		return
	}
	job := models.Job{
		ID:         uuid.New().String(),
		NodeID:     node.ID,
		Type:       models.JobTypeCommand,
		Status:     models.JobStatusPending,
		Command:    command(name),
		MaxRetries: 0,
		Timeout:    60,
		CreatedAt:  time.Now(),
	}
	if err := s.db.Create(&job).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"job_id": job.ID})
}

// ContainerLogs handles POST /nodes/:id/containers/:name/logs?lines=N.
func (s *NodeOpsService) ContainerLogs(c *gin.Context) {
	lines, err := strconv.Atoi(c.DefaultQuery("lines", "200"))
	if err != nil || lines < 1 || lines > 2000 {
		lines = 200
	}
	s.containerJob(c, func(name string) string {
		return fmt.Sprintf("docker logs --tail %d --timestamps %s 2>&1", lines, shellQuote(name))
	})
}

// RestartContainer handles POST /nodes/:id/containers/:name/restart.
func (s *NodeOpsService) RestartContainer(c *gin.Context) {
	s.containerJob(c, func(name string) string {
		return fmt.Sprintf("docker restart %s", shellQuote(name))
	})
}

// Connection handles GET /nodes/:id/connection: how the Hub last heard from
// the node, over the API and over WireGuard.
func (s *NodeOpsService) Connection(c *gin.Context) {
	var node models.Node
	if err := s.db.First(&node, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}
	resp := gin.H{
		"online":         node.Online,
		"last_heartbeat": node.LastHeartbeat,
		"ping_latency":   node.PingLatency,
		"agent_version":  node.AgentVersion,
	}
	if hs, err := wireGuardHandshakes(); err != nil {
		resp["wg_error"] = err.Error()
	} else if t, ok := hs[node.VPNIP]; ok {
		resp["wg_handshake"] = t
	}
	c.JSON(http.StatusOK, resp)
}

// wireGuardHandshakes maps each peer's VPN address to its last handshake,
// from `wg show <iface> dump` on the Hub.
func wireGuardHandshakes() (map[string]time.Time, error) {
	out, err := exec.Command("sudo", "-n", "wg", "show", WireGuardInterface, "dump").Output()
	if err != nil {
		if out, err = exec.Command("wg", "show", WireGuardInterface, "dump").Output(); err != nil {
			return nil, fmt.Errorf("can't read WireGuard status: %v", err)
		}
	}
	return parseWGDump(out), nil
}

// parseWGDump reads peer lines of `wg show <iface> dump`: public key, preshared
// key, endpoint, allowed IPs, latest handshake (unix seconds), rx, tx, keepalive.
// The first line describes the interface itself and has fewer fields.
func parseWGDump(out []byte) map[string]time.Time {
	res := map[string]time.Time{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) < 8 {
			continue
		}
		secs, err := strconv.ParseInt(f[4], 10, 64)
		if err != nil || secs == 0 {
			continue
		}
		for _, cidr := range strings.Split(f[3], ",") {
			ip := strings.TrimSuffix(strings.TrimSpace(cidr), "/32")
			res[ip] = time.Unix(secs, 0)
		}
	}
	return res
}
