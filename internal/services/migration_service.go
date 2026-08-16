package services

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

type MigrationService struct {
	db           *gorm.DB
	jobService   *JobService
	nginxService *NginxService
}

func NewMigrationService(db *gorm.DB, jobService *JobService, nginxService *NginxService) *MigrationService {
	return &MigrationService{
		db:           db,
		jobService:   jobService,
		nginxService: nginxService,
	}
}

func (s *MigrationService) getGitHubToken() string {
	var token models.GitHubToken
	if err := s.db.First(&token).Error; err == nil && token.Token != "" {
		return token.Token
	}
	return ""
}

func (s *MigrationService) StartMigrationSweeper() {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		log.Println("🔄 Migration sweeper started")
		for range ticker.C {
			s.cleanupStuckMigrations()
		}
	}()
}

func (s *MigrationService) cleanupStuckMigrations() {
	var migrations []models.Migration
	s.db.Where("status = ?", models.MigrationStatusRunning).
		Where("created_at < ?", time.Now().Add(-5*time.Minute)).
		Find(&migrations)

	for _, migration := range migrations {
		log.Printf("⚠️ Found stuck migration: %s (created: %v)", migration.ID, migration.CreatedAt)
		migration.Status = models.MigrationStatusFailed
		s.db.Save(&migration)
	}
}

func (s *MigrationService) MigrateToNode(projectID, targetNodeID string) {
	var project models.Project
	if err := s.db.First(&project, "id = ?", projectID).Error; err != nil {
		log.Printf("⚠️ MigrateToNode: project %s not found", projectID)
		return
	}

	if project.NodeID == targetNodeID {
		return // already there
	}

	var sourceNode models.Node
	if err := s.db.First(&sourceNode, "id = ?", project.NodeID).Error; err != nil {
		log.Printf("⚠️ MigrateToNode: source node not found for project %s", projectID)
		return
	}

	var targetNode models.Node
	if err := s.db.First(&targetNode, "id = ?", targetNodeID).Error; err != nil {
		log.Printf("⚠️ MigrateToNode: target node %s not found", targetNodeID)
		return
	}

	// Check for active migration
	var existing models.Migration
	err := s.db.Where("project_id = ? AND status IN (?, ?)",
		projectID,
		models.MigrationStatusPending,
		models.MigrationStatusRunning).
		First(&existing).Error
	if err == nil {
		log.Printf("⚠️ MigrateToNode: project %s already has active migration", projectID)
		return
	}

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
		log.Printf("⚠️ MigrateToNode: failed to create migration record: %v", err)
		return
	}

	// Stop on source
	if sourceNode.Online {
		stopJob := &models.Job{
			ID:     uuid.New().String(),
			NodeID: sourceNode.ID,
			Type:   models.JobTypeMigrateStop,
			Status: models.JobStatusPending,
			Payload: &models.JobPayload{
				ContainerName: project.Name,
				Operation:     "stop",
			},
			MaxRetries: 2,
			CreatedAt:  time.Now(),
		}
		s.db.Create(stopJob)
	}

	// Pull image on target
	token := s.getGitHubToken()
	if token != "" {
		pullJob := &models.Job{
			ID:     uuid.New().String(),
			NodeID: targetNode.ID,
			Type:   models.JobTypeImagePull,
			Status: models.JobStatusPending,
			Payload: &models.JobPayload{
				ContainerName: project.Name,
				Image:         project.Image,
				Repository:    strings.Split(project.Image, ":")[0],
				Operation:     "pull",
			},
			MaxRetries: 3,
			CreatedAt:  time.Now(),
		}
		if err := s.db.Create(pullJob).Error; err != nil {
			log.Printf("⚠️ MigrateToNode: failed to create pull job: %v", err)
			return
		}
		log.Printf("📥 Pull job created: %s on %s", pullJob.ID, targetNode.Hostname)
	}

	// Start on target
	startJob := &models.Job{
		ID:     uuid.New().String(),
		NodeID: targetNode.ID,
		Type:   models.JobTypeMigrateStart,
		Status: models.JobStatusPending,
		Payload: &models.JobPayload{
			ContainerName: project.Name,
			Image:         project.Image,
			Ports:         project.Ports,
			Volumes:       project.Volumes,
			EnvVars:       project.EnvVars,
			Operation:     "start",
			SourceNodeIP:  sourceNode.VPNIP,
		},
		MaxRetries: 3,
		CreatedAt:  time.Now(),
	}
	if err := s.db.Create(startJob).Error; err != nil {
		log.Printf("⚠️ MigrateToNode: failed to create start job: %v", err)
		return
	}

	project.NodeID = targetNode.ID
	s.db.Save(&project)

	migration.JobID = startJob.ID
	migration.Status = models.MigrationStatusRunning
	s.db.Save(&migration)

	if err := s.nginxService.UpdateNginxConfig(); err != nil {
		log.Printf("⚠️ MigrateToNode: nginx update failed: %v", err)
	}

	log.Printf("🎯 Master enforcement migration: %s → %s", sourceNode.Hostname, targetNode.Hostname)
}

func (s *MigrationService) EnforceMasterNode(masterNodeID string) {
	if masterNodeID == "" {
		return
	}

	var master models.Node
	if err := s.db.First(&master, "id = ?", masterNodeID).Error; err != nil {
		return
	}

	targetNodeID := masterNodeID

	if !master.Online {
		// Master is offline, find healthiest available node
		var healthiest models.Node
		if err := s.db.Where("id != ? AND online = ?", masterNodeID, true).
			Order("health_score DESC").
			First(&healthiest).Error; err != nil {
			log.Printf("⚠️ EnforceMasterNode: no available nodes to migrate to")
			return
		}
		log.Printf("⚠️ Master offline, falling back to healthiest node: %s", healthiest.Hostname)
		targetNodeID = healthiest.ID
	}

	var projects []models.Project
	s.db.Where("node_id != ? AND health_status != ?", targetNodeID, "migrating").Find(&projects)

	for _, project := range projects {
		log.Printf("Master node enforcement: migrating %s to %s", project.Name, targetNodeID)
		go s.MigrateToNode(project.ID, targetNodeID)
	}
}

func (s *MigrationService) MigrateProject(c *gin.Context) {
	var req struct {
		ProjectID    string `json:"project_id"     binding:"required"`
		TargetNodeID string `json:"target_node_id" binding:"required"`
		Image        string `json:"image"` // optional, updates if provided
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var project models.Project
	if err := s.db.First(&project, "id = ?", req.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}

	if req.Image != "" {
		project.Image = req.Image
		if err := s.db.Model(&project).Update("image", req.Image).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update project image"})
			return
		}
	}

	var sourceNode models.Node
	if err := s.db.First(&sourceNode, "id = ?", project.NodeID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "source node not found"})
		return
	}

	var targetNode models.Node
	if err := s.db.First(&targetNode, "id = ?", req.TargetNodeID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "target node not found"})
		return
	}

	if sourceNode.ID == targetNode.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source and target nodes are the same"})
		return
	}

	// Check for active migration
	var existingMigration models.Migration
	err := s.db.Where("project_id = ? AND status IN (?, ?)",
		project.ID,
		models.MigrationStatusPending,
		models.MigrationStatusRunning).
		First(&existingMigration).Error

	if err == nil {
		c.JSON(http.StatusConflict, gin.H{
			"error":        "project already has an active migration",
			"migration_id": existingMigration.ID,
		})
		return
	}

	// Create migration record
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var stopJobID string

	// Step 1: Stop on source (if online)
	if sourceNode.Online {
		stopJob := &models.Job{
			ID:     uuid.New().String(),
			NodeID: sourceNode.ID,
			Type:   models.JobTypeMigrateStop,
			Status: models.JobStatusPending,
			Payload: &models.JobPayload{
				ContainerName: project.Name,
				Operation:     "stop",
			},
			MaxRetries: 2,
			CreatedAt:  time.Now(),
		}
		if err := s.db.Create(stopJob).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		stopJobID = stopJob.ID
		log.Printf("🛑 Stop job created: %s on %s", stopJob.ID, sourceNode.Hostname)
	}

	// Step 2: Pull image on target
	token := s.getGitHubToken()
	if token != "" {
		pullJob := &models.Job{
			ID:     uuid.New().String(),
			NodeID: targetNode.ID,
			Type:   models.JobTypeImagePull,
			Status: models.JobStatusPending,
			Payload: &models.JobPayload{
				ContainerName: project.Name,
				Image:         project.Image,
				Repository:    strings.Split(project.Image, ":")[0],
				Operation:     "pull",
			},
			MaxRetries: 3,
			CreatedAt:  time.Now(),
		}
		if err := s.db.Create(pullJob).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		log.Printf("📥 Pull job created: %s on %s", pullJob.ID, targetNode.Hostname)
	}

	// Step 3: Start on target
	startJob := &models.Job{
		ID:     uuid.New().String(),
		NodeID: targetNode.ID,
		Type:   models.JobTypeMigrateStart,
		Status: models.JobStatusPending,
		Payload: &models.JobPayload{
			ContainerName: project.Name,
			Image:         project.Image,
			Ports:         project.Ports,
			Volumes:       project.Volumes,
			EnvVars:       project.EnvVars,
			Operation:     "start",
			SourceNodeIP:  sourceNode.VPNIP,
		},
		MaxRetries: 3,
		CreatedAt:  time.Now(),
	}

	if err := s.db.Create(startJob).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Update project
	project.NodeID = targetNode.ID
	s.db.Save(&project)

	migration.JobID = startJob.ID
	migration.Status = models.MigrationStatusRunning
	s.db.Save(&migration)

	if err := s.nginxService.UpdateNginxConfig(); err != nil {
		log.Printf("⚠️ Failed to update Nginx config: %v", err)
	}

	log.Printf(
		"🔄 Project migration started: %s (%s) -> %s",
		project.Name,
		sourceNode.Hostname,
		targetNode.Hostname,
	)

	c.JSON(http.StatusAccepted, gin.H{
		"migration":    migration,
		"stop_job_id":  stopJobID,
		"start_job_id": startJob.ID,
		"source_node":  sourceNode.Hostname,
		"target_node":  targetNode.Hostname,
	})
}

func (s *MigrationService) ListMigrations(c *gin.Context) {
	page := c.DefaultQuery("page", "1")
	limit := c.DefaultQuery("limit", "20")

	pageInt, _ := strconv.Atoi(page)
	limitInt, _ := strconv.Atoi(limit)

	if pageInt < 1 {
		pageInt = 1
	}
	if limitInt < 1 {
		limitInt = 20
	}
	if limitInt > 100 {
		limitInt = 100
	}

	offset := (pageInt - 1) * limitInt

	var migrations []models.Migration
	var total int64

	s.db.Model(&models.Migration{}).Count(&total)
	s.db.Order("created_at DESC").Limit(limitInt).Offset(offset).Find(&migrations)

	c.JSON(http.StatusOK, gin.H{
		"data": migrations,
		"pagination": gin.H{
			"page":  pageInt,
			"limit": limitInt,
			"total": total,
			"pages": (total + int64(limitInt) - 1) / int64(limitInt),
		},
	})
}

func (s *MigrationService) GetMigration(c *gin.Context) {
	id := c.Param("id")
	var migration models.Migration
	if err := s.db.First(&migration, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "migration not found"})
		return
	}
	c.JSON(http.StatusOK, migration)
}
