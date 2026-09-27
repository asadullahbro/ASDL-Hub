package services

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

// failoverThreshold is how many health checks in a row must fail before a
// project is failed over, so one slow response or a restart doesn't move it.
// With healthCheckInterval that is about 30 seconds of downtime.
const (
	failoverThreshold   = 3
	healthCheckInterval = 10 * time.Second
	healthCheckTimeout  = 3 * time.Second
)

type HealthService struct {
	db               *gorm.DB
	migrationService *MigrationService
	nginxService     *NginxService
	deployer         *Deployer
	// failures counts consecutive failed checks per project ID. Only the
	// checker goroutine touches it.
	failures map[string]int
	// down holds when each project was announced down, so "back up" is
	// announced once it answers again. Only the checker goroutine touches it.
	down map[string]time.Time
	// failingSince holds when each project's current run of failed checks
	// began. Only the checker goroutine touches it.
	failingSince map[string]time.Time
}

func NewHealthService(db *gorm.DB, migrationService *MigrationService, nginxService *NginxService, deployer *Deployer) *HealthService {
	return &HealthService{
		db:               db,
		migrationService: migrationService,
		nginxService:     nginxService,
		deployer:         deployer,
		failures:         make(map[string]int),
		down:             make(map[string]time.Time),
		failingSince:     make(map[string]time.Time),
	}
}

func (s *HealthService) StartHealthChecker() {
	ticker := time.NewTicker(healthCheckInterval)
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

	// Check all projects at once so one dead node (every check timing out)
	// doesn't delay the rest; act on the results one by one.
	healthy := make([]bool, len(projects))
	var wg sync.WaitGroup
	for i := range projects {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			healthy[i] = s.checkProjectHealth(&projects[i])
		}(i)
	}
	wg.Wait()

	for i, project := range projects {
		if healthy[i] {
			delete(s.failures, project.ID)
			delete(s.failingSince, project.ID)
			if project.HealthStatus != "healthy" {
				s.db.Model(&project).Update("health_status", "healthy")
			}
			if since, ok := s.down[project.ID]; ok {
				delete(s.down, project.ID)
				node := s.nodeName(project.NodeID)
				notify(Event{
					Type: EventAppRecovered, Level: LevelSuccess,
					Title:   project.Name + " is back up",
					Message: fmt.Sprintf("%s answers again on %s, after %s down.", project.Name, node, time.Since(since).Round(time.Second)),
					URL:     link("/projects"), ProjectID: project.ID, Project: project.Name, Node: node,
				})
			}
			continue
		}

		s.failures[project.ID]++
		if _, ok := s.failingSince[project.ID]; !ok {
			s.failingSince[project.ID] = time.Now()
		}
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

	client := http.Client{Timeout: healthCheckTimeout}
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

// failoverCooldown is how long a node that failed to take over a project is
// skipped as a failover target for that project.
const failoverCooldown = 30 * time.Minute

// handleUnhealthyProject redeploys the project on the healthiest other online
// node. Nodes that already failed to take it over recently are skipped; only
// when no node is left is the project marked failed.
func (s *HealthService) handleUnhealthyProject(project *models.Project) {
	var active int64
	s.db.Model(&models.Migration{}).
		Where("project_id = ? AND status IN ?", project.ID,
			[]string{models.MigrationStatusPending, models.MigrationStatusRunning}).
		Count(&active)
	if active > 0 {
		log.Printf("⏳ %s already has a failover in progress", project.Name)
		return
	}

	image := project.Image
	if image == "" {
		log.Printf("❌ %s has no image to fail over with", project.Name)
		s.announceDown(project, "and it has no image to start elsewhere.")
		s.markFailed(project)
		return
	}

	target := s.failoverTarget(project)
	if target == nil {
		log.Printf("❌ No node left to run %s; marking it failed", project.Name)
		s.announceDown(project, "and no other node can take it over, so it stays down until one can or it recovers.")
		s.markFailed(project)
		return
	}
	s.announceDown(project, fmt.Sprintf("so it is being moved to %s.", target.Hostname))

	log.Printf("🔄 Failing over %s: %s -> %s", project.Name, project.NodeID, target.Hostname)
	job, _, err := s.deployer.Dispatch(project, target, image, DeployMeta{
		Trigger:    TriggerFailover,
		Repository: project.Repository,
	})
	if err != nil {
		log.Printf("❌ Failed to dispatch failover of %s: %v", project.Name, err)
		return
	}

	now := time.Now()
	migration := &models.Migration{
		ID:           uuid.New().String(),
		ProjectID:    project.ID,
		ContainerID:  project.ContainerID,
		SourceNodeID: project.NodeID,
		TargetNodeID: target.ID,
		Status:       models.MigrationStatusRunning,
		JobID:        job.ID,
		CreatedAt:    now,
	}
	if err := s.db.Create(migration).Error; err != nil {
		log.Printf("⚠️ Failed to record failover of %s: %v", project.Name, err)
	}
	s.db.Model(project).Update("health_status", "migrating")
}

// failoverTarget returns the best online node for project other than its
// current one, preferring the master node, or nil if there is none.
func (s *HealthService) failoverTarget(project *models.Project) *models.Node {
	var failed []string
	s.db.Model(&models.Migration{}).
		Where("project_id = ? AND status = ? AND created_at > ?", project.ID,
			models.MigrationStatusFailed, time.Now().Add(-failoverCooldown)).
		Pluck("target_node_id", &failed)

	q := s.db.Scopes(models.Available).Where("id != ?", project.NodeID)
	if len(failed) > 0 {
		q = q.Where("id NOT IN ?", failed)
	}
	var nodes []models.Node
	q.Order("health_score desc").Find(&nodes)
	if len(nodes) == 0 {
		return nil
	}

	var master models.Setting
	if s.db.First(&master, "key = ?", "master_node_id").Error == nil {
		for i := range nodes {
			if nodes[i].ID == master.Value {
				return &nodes[i]
			}
		}
	}
	return &nodes[0]
}

// announceDown sends "app down" once per outage; what continues the
// sentence says what happens next.
func (s *HealthService) announceDown(project *models.Project, next string) {
	if _, ok := s.down[project.ID]; ok {
		return
	}
	since, ok := s.failingSince[project.ID]
	if !ok {
		since = time.Now()
	}
	s.down[project.ID] = since
	node := s.nodeName(project.NodeID)
	notify(Event{
		Type: EventAppDown, Level: LevelError,
		Title:   project.Name + " is down",
		Message: fmt.Sprintf("%s stopped answering on %s, %s", project.Name, node, next),
		URL:     link("/projects"), ProjectID: project.ID, Project: project.Name, Node: node,
	})
}

func (s *HealthService) nodeName(id string) string {
	var node models.Node
	if s.db.Select("hostname").First(&node, "id = ?", id).Error != nil {
		return "an unknown node"
	}
	return node.Hostname
}

func (s *HealthService) markFailed(project *models.Project) {
	s.db.Model(project).Updates(map[string]interface{}{"status": "failed", "health_status": "unhealthy"})
	if err := s.nginxService.UpdateNginxConfig(); err != nil {
		log.Printf("⚠️ Failed to update Nginx config: %v", err)
	}
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
