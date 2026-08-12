package services

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

type NodeService struct {
	db *gorm.DB
}

func NewNodeService(db *gorm.DB) *NodeService {
	return &NodeService{db: db}
}

func (s *NodeService) Register(c *gin.Context) {
	var req struct {
		Hostname     string   `json:"hostname"`
		VPNIP        string   `json:"vpn_ip"`
		OS           string   `json:"os"`
		Architecture string   `json:"architecture"`
		CPU          int      `json:"cpu"`
		MemoryTotal  int64    `json:"memory_total"`
		DiskTotal    int64    `json:"disk_total"`
		Capabilities []string `json:"capabilities"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("Register request: hostname=%s, vpn_ip=%s", req.Hostname, req.VPNIP)

	vpnIP := req.VPNIP
	if vpnIP == "" {
		if ip, exists := c.Get("vpn_ip"); exists {
			vpnIP = ip.(string)
		}
	}
	if vpnIP == "" {
		vpnIP = c.ClientIP()
	}

	if vpnIP == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "vpn_ip not found"})
		return
	}

	log.Printf("Using VPN IP: %s", vpnIP)

	var node models.Node
	err := s.db.Where("vpn_ip = ?", vpnIP).First(&node).Error

	if err == nil {
		node.Hostname = req.Hostname
		node.OS = req.OS
		node.Architecture = req.Architecture
		node.CPU = req.CPU
		node.MemoryTotal = req.MemoryTotal
		node.DiskTotal = req.DiskTotal
		node.Capabilities = req.Capabilities
		node.Online = true
		node.LastHeartbeat = time.Now()

		s.db.Save(&node)
		c.JSON(http.StatusOK, node)
		return
	}

	node = models.Node{
		ID:            uuid.New().String(),
		Hostname:      req.Hostname,
		VPNIP:         vpnIP,
		OS:            req.OS,
		Architecture:  req.Architecture,
		CPU:           req.CPU,
		MemoryTotal:   req.MemoryTotal,
		DiskTotal:     req.DiskTotal,
		Online:        true,
		Capabilities:  req.Capabilities,
		LastHeartbeat: time.Now(),
	}

	s.db.Create(&node)
	log.Printf("Node registered: %s with ID: %s, VPN IP: %s", node.Hostname, node.ID, node.VPNIP)
	c.JSON(http.StatusCreated, node)
}

func (s *NodeService) Heartbeat(c *gin.Context) {
	nodeID := c.Param("id")
	vpnIP, _ := c.Get("vpn_ip")

	var req struct {
		CPUPercent  float64 `json:"cpu_percent"`
		MemoryUsed  int64   `json:"memory_used"`
		DiskUsed    int64   `json:"disk_used"`
		Uptime      int64   `json:"uptime"`
		LoadAvg1    float64 `json:"load_avg_1"`
		LoadAvg5    float64 `json:"load_avg_5"`
		LoadAvg15   float64 `json:"load_avg_15"`
		PingLatency float64 `json:"ping_latency"`
		WiFiSignal  int     `json:"wifi_signal"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var node models.Node
	if err := s.db.First(&node, "id = ? OR vpn_ip = ?", nodeID, vpnIP).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}

	node.Online = true
	node.LastHeartbeat = time.Now()
	node.Uptime = req.Uptime
	node.MemoryUsed = req.MemoryUsed
	node.DiskUsed = req.DiskUsed
	node.LoadAvg1 = req.LoadAvg1
	node.LoadAvg5 = req.LoadAvg5
	node.LoadAvg15 = req.LoadAvg15
	node.PingLatency = req.PingLatency
	node.WiFiSignal = req.WiFiSignal

	// Recalculate health score
	healthScore := s.calculateHealthScore(&node)
	node.HealthScore = healthScore
	node.LastCheck = time.Now()

	s.db.Save(&node)

	heartbeat := models.Heartbeat{
		NodeID:      node.ID,
		CPUPercent:  req.CPUPercent,
		MemoryUsed:  req.MemoryUsed,
		MemoryTotal: node.MemoryTotal,
		DiskUsed:    req.DiskUsed,
		DiskTotal:   node.DiskTotal,
		LoadAvg1:    req.LoadAvg1,
		LoadAvg5:    req.LoadAvg5,
		LoadAvg15:   req.LoadAvg15,
		Uptime:      req.Uptime,
		Timestamp:   time.Now(),
	}
	s.db.Create(&heartbeat)

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *NodeService) List(c *gin.Context) {
	var nodes []models.Node
	s.db.Order("online DESC, vpn_ip ASC").Find(&nodes)
	c.JSON(http.StatusOK, nodes)
}

func (s *NodeService) Get(c *gin.Context) {
	id := c.Param("id")
	var node models.Node
	if err := s.db.First(&node, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}
	c.JSON(http.StatusOK, node)
}

func (s *NodeService) GetNodeDetails(c *gin.Context) {
	id := c.Param("id")
	var node models.Node
	if err := s.db.First(&node, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}

	var projects []models.Project
	s.db.Where("node_id = ?", id).Find(&projects)

	var containers []models.Container
	s.db.Where("node_id = ?", id).Find(&containers)

	healthScore := s.calculateHealthScore(&node)

	c.JSON(http.StatusOK, gin.H{
		"node":            node,
		"projects":        projects,
		"containers":      containers,
		"project_count":   len(projects),
		"container_count": len(containers),
		"health_score":    healthScore,
	})
}

// calculateHealthScore computes a weighted 0-100 health score for a node based on
// heartbeat freshness, memory, disk, CPU load, ping latency, WiFi signal, and
// recent failure count. It also stores a per-component breakdown as JSON on
// node.HealthDetails (mutates the passed-in node; caller must persist via db.Save
// if the breakdown needs to survive beyond the current request).
func (s *NodeService) calculateHealthScore(node *models.Node) int {
	score := 100
	details := map[string]interface{}{
		"cpu_score":       100,
		"memory_score":    100,
		"disk_score":      100,
		"load_score":      100,
		"ping_score":      100,
		"wifi_score":      100,
		"heartbeat_score": 100,
	}

	// 1. Heartbeat freshness (20% weight)
	if time.Since(node.LastHeartbeat) > 30*time.Second {
		details["heartbeat_score"] = 80
		score -= 10
	}
	if time.Since(node.LastHeartbeat) > 60*time.Second {
		details["heartbeat_score"] = 50
		score -= 20
	}
	if time.Since(node.LastHeartbeat) > 120*time.Second {
		details["heartbeat_score"] = 0
		score -= 30
	}

	// 2. Memory usage (20% weight)
	if node.MemoryTotal > 0 {
		memPercent := float64(node.MemoryUsed) / float64(node.MemoryTotal) * 100
		if memPercent > 95 {
			details["memory_score"] = 10
			score -= 30
		} else if memPercent > 85 {
			details["memory_score"] = 40
			score -= 20
		} else if memPercent > 75 {
			details["memory_score"] = 70
			score -= 10
		} else if memPercent > 60 {
			details["memory_score"] = 85
			score -= 5
		} else {
			details["memory_score"] = 100
		}
	}

	// 3. Disk usage (10% weight)
	if node.DiskTotal > 0 {
		diskPercent := float64(node.DiskUsed) / float64(node.DiskTotal) * 100
		if diskPercent > 95 {
			details["disk_score"] = 20
			score -= 15
		} else if diskPercent > 85 {
			details["disk_score"] = 50
			score -= 10
		} else if diskPercent > 75 {
			details["disk_score"] = 75
			score -= 5
		} else {
			details["disk_score"] = 100
		}
	}

	// 4. CPU Load (15% weight)
	if node.LoadAvg1 > float64(node.CPU) {
		details["load_score"] = 30
		score -= 20
	} else if node.LoadAvg1 > float64(node.CPU)*0.8 {
		details["load_score"] = 60
		score -= 10
	} else if node.LoadAvg1 > float64(node.CPU)*0.6 {
		details["load_score"] = 80
		score -= 5
	} else {
		details["load_score"] = 100
	}

	// 5. Ping Latency (15% weight)
	if node.PingLatency > 0 {
		if node.PingLatency > 100 {
			details["ping_score"] = 20
			score -= 20
		} else if node.PingLatency > 50 {
			details["ping_score"] = 60
			score -= 10
		} else if node.PingLatency > 20 {
			details["ping_score"] = 80
			score -= 5
		} else {
			details["ping_score"] = 100
		}
	}

	// 6. WiFi Signal (10% weight)
	if node.WiFiSignal > 0 {
		if node.WiFiSignal < 30 {
			details["wifi_score"] = 20
			score -= 15
		} else if node.WiFiSignal < 50 {
			details["wifi_score"] = 50
			score -= 10
		} else if node.WiFiSignal < 70 {
			details["wifi_score"] = 75
			score -= 5
		} else {
			details["wifi_score"] = 100
		}
	}

	// 7. Failure count penalty
	if node.FailureCount > 5 {
		score -= 20
	} else if node.FailureCount > 3 {
		score -= 10
	} else if node.FailureCount > 1 {
		score -= 5
	}

	// 8. Online status
	if !node.Online {
		score = 0
		details["online"] = false
	}

	// Clamp score
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	// Save details as JSON
	detailsJSON, _ := json.Marshal(details)
	node.HealthDetails = string(detailsJSON)

	return score
}

func (s *NodeService) GetNodeHealth(c *gin.Context) {
	id := c.Param("id")

	var node models.Node
	if err := s.db.First(&node, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}

	healthScore := s.calculateHealthScore(&node)

	var healthDetails map[string]interface{}
	if node.HealthDetails != "" {
		if err := json.Unmarshal([]byte(node.HealthDetails), &healthDetails); err != nil {
			healthDetails = map[string]interface{}{}
		}
	} else {
		healthDetails = map[string]interface{}{}
	}

	var memoryPercent float64
	if node.MemoryTotal > 0 {
		memoryPercent = float64(node.MemoryUsed) / float64(node.MemoryTotal) * 100
	}

	c.JSON(http.StatusOK, gin.H{
		"node_id":        node.ID,
		"hostname":       node.Hostname,
		"status":         node.Status,
		"health_score":   healthScore,
		"health_details": healthDetails,
		"online":         node.Online,
		"last_heartbeat": node.LastHeartbeat,
		"memory_used":    node.MemoryUsed,
		"memory_total":   node.MemoryTotal,
		"memory_percent": memoryPercent,
		"load_avg":       node.LoadAvg1,
		"ping_latency":   node.PingLatency,
		"wifi_signal":    node.WiFiSignal,
	})
}

func (s *NodeService) CheckAllNodesHealth(c *gin.Context) {
	var nodes []models.Node
	s.db.Find(&nodes)

	results := []gin.H{}
	for _, node := range nodes {
		healthScore := s.calculateHealthScore(&node)
		results = append(results, gin.H{
			"node_id":      node.ID,
			"hostname":     node.Hostname,
			"status":       node.Status,
			"health_score": healthScore,
			"online":       node.Online,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"nodes": results,
		"total": len(results),
	})
}

func (s *NodeService) StartOfflineSweeper() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			var nodes []models.Node
			s.db.Where("online = ?", true).Find(&nodes)

			for _, node := range nodes {
				if time.Since(node.LastHeartbeat) > 2*time.Minute {
					node.Online = false
					s.db.Save(&node)
					log.Printf("🔴 Node %s marked offline (last heartbeat: %v)", node.Hostname, node.LastHeartbeat)
				}
			}
		}
	}()
}
