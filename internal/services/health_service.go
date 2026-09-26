package services

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

// failoverThreshold is how many health checks in a row must fail before a
// project is failed over, so one slow response or a restart doesn't move it.
const failoverThreshold = 3

type HealthService struct {
	db               *gorm.DB
	migrationService *MigrationService
	nginxService     *NginxService
	// failures counts consecutive failed checks per project ID. Only the
	// checker goroutine touches it.
	failures map[string]int
}

func NewHealthService(db *gorm.DB, migrationService *MigrationService, nginxService *NginxService) *HealthService {
	return &HealthService{
		db:               db,
		migrationService: migrationService,
		nginxService:     nginxService,
		failures:         make(map[string]int),
	}
}

func (s *HealthService) StartHealthChecker() {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		log.Println("🩺 Health checker started")
		for range ticker.C {
			s.checkAllProjects()
		}
	}()
}

func (s *HealthService) checkAllProjects() {
	var projects []models.Project
	s.db.Where("status = ?", "running").Find(&projects)

	for _, project := range projects {
		if s.checkProjectHealth(&project) {
			delete(s.failures, project.ID)
			if project.HealthStatus != "healthy" {
				s.db.Model(&project).Update("health_status", "healthy")
			}
			continue
		}

		s.failures[project.ID]++
		if s.failures[project.ID] < failoverThreshold {
			s.db.Model(&project).Update("health_status", "degraded")
			log.Printf("⚠️ Project %s failed health check %d/%d", project.Name, s.failures[project.ID], failoverThreshold)
			continue
		}

		delete(s.failures, project.ID)
		log.Printf("⚠️ Project %s is unhealthy, attempting failover...", project.Name)
		s.handleUnhealthyProject(&project)
	}
}

func (s *HealthService) checkProjectHealth(project *models.Project) bool {
	var node models.Node
	if err := s.db.First(&node, "id = ?", project.NodeID).Error; err != nil {
		log.Printf("❌ Node not found for project %s: %v", project.Name, err)
		return false
	}

	if !node.Online {
		log.Printf("❌ Node %s is offline for project %s", node.Hostname, project.Name)
		return false
	}

	client := http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://%s:%s/health", node.VPNIP, project.HostPort())

	resp, err := client.Get(url)
	if err != nil {
		log.Printf("❌ Health check failed for %s: %v", project.Name, err)
		return false
	}
	defer resp.Body.Close()

	// Any non-5xx answer means the app is up and serving. Apps without a
	// /health route answer 404, which shouldn't trigger a failover.
	if resp.StatusCode < 500 {
		return true
	}

	log.Printf("❌ Project %s returned status %d", project.Name, resp.StatusCode)
	return false
}

func (s *HealthService) handleUnhealthyProject(project *models.Project) {
	var sourceNode models.Node
	if err := s.db.First(&sourceNode, "id = ?", project.NodeID).Error; err != nil {
		log.Printf("❌ Failed to get source node: %v", err)
		return
	}

	var nodes []models.Node
	s.db.Where("online = ? AND id != ?", true, project.NodeID).Find(&nodes)

	if len(nodes) == 0 {
		log.Printf("❌ No healthy nodes available for failover of %s", project.Name)
		project.Status = "failed"
		project.HealthStatus = "unhealthy"
		s.db.Save(project)
		return
	}

	targetNode := s.findHealthiestNode(nodes)
	log.Printf("🔄 Auto-failover: %s (%s) -> %s (%s)",
		project.Name,
		sourceNode.Hostname,
		targetNode.Hostname,
		targetNode.VPNIP)

	migration := &models.Migration{
		ID:           uuid.New().String(),
		ProjectID:    project.ID,
		ContainerID:  project.ContainerID,
		SourceNodeID: sourceNode.ID,
		TargetNodeID: targetNode.ID,
		Status:       models.MigrationStatusPending,
		CreatedAt:    time.Now(),
	}

	if err := s.db.Create(migration).Error; err != nil {
		log.Printf("❌ Failed to create migration for failover: %v", err)
		return
	}

	// Stop on source (if online)
	if sourceNode.Online {
		stopJob := &models.Job{
			ID:     uuid.New().String(),
			NodeID: sourceNode.ID,
			Type:   models.JobTypeFailoverStop,
			Status: models.JobStatusPending,
			Payload: &models.JobPayload{
				ContainerName: project.Name,
				Operation:     "stop",
			},
			MaxRetries: 2,
			CreatedAt:  time.Now(),
		}
		if err := s.db.Create(stopJob).Error; err != nil {
			log.Printf("❌ Failed to create stop job: %v", err)
		} else {
			log.Printf("🛑 Stop job created on %s", sourceNode.Hostname)
		}
	}

	// Pull image on target (if token exists)
	// Start on target
	startJob := &models.Job{
		ID:     uuid.New().String(),
		NodeID: targetNode.ID,
		Type:   models.JobTypeFailoverStart,
		Status: models.JobStatusPending,
		Payload: &models.JobPayload{
			ContainerName: project.Name,
			Image:         project.Image,
			Ports:         project.Ports,
			Volumes:       project.Volumes,
			EnvVars:       project.EnvVars,
			Operation:     "start",
		},
		MaxRetries: 3,
		CreatedAt:  time.Now(),
	}

	if err := s.db.Create(startJob).Error; err != nil {
		log.Printf("❌ Failed to create failover job: %v", err)
		return
	}

	migration.JobID = startJob.ID
	s.db.Save(migration)

	project.NodeID = targetNode.ID
	project.Status = "running"
	project.HealthStatus = "degraded"
	s.db.Save(project)

	if err := s.nginxService.UpdateNginxConfig(); err != nil {
		log.Printf("⚠️ Failed to update Nginx config: %v", err)
	}

	log.Printf("✅ Failover initiated: %s -> %s (job: %s)",
		project.Name, targetNode.Hostname, startJob.ID)
}

func (s *HealthService) findHealthiestNode(nodes []models.Node) *models.Node {
	if len(nodes) == 0 {
		return nil
	}

	var bestNode *models.Node
	for i := range nodes {
		if bestNode == nil || nodes[i].LastHeartbeat.After(bestNode.LastHeartbeat) {
			bestNode = &nodes[i]
		}
	}
	return bestNode
}

func (s *HealthService) GetProjectHealth(c *gin.Context) {
	id := c.Param("id")
	var project models.Project
	if err := s.db.First(&project, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}

	healthy := s.checkProjectHealth(&project)
	status := "healthy"
	if !healthy {
		status = "unhealthy"
	}

	c.JSON(http.StatusOK, gin.H{
		"project_id": project.ID,
		"name":       project.Name,
		"status":     project.Status,
		"health":     status,
		"node_id":    project.NodeID,
		"last_check": time.Now(),
	})
}
