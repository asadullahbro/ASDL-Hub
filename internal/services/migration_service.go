package services

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
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
	deployer     *Deployer
}

func NewMigrationService(db *gorm.DB, jobService *JobService, nginxService *NginxService, deployer *Deployer) *MigrationService {
	return &MigrationService{
		db:           db,
		jobService:   jobService,
		nginxService: nginxService,
		deployer:     deployer,
	}
}

func (s *MigrationService) StartMigrationSweeper() {
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		log.Println("🔄 Migration sweeper started")
		for range ticker.C {
			s.cleanupStuckMigrations()
		}
	}()
}

// How long a migration may take before it is given up on, so the health checker can try the next node.
const (
	// unclaimedOnQuietNode: the target hasn't picked the job up and hasn't sent a heartbeat lately either, so it
	// is probably down too.
	unclaimedOnQuietNode = time.Minute
	// unclaimedOnLiveNode: the target is up but still busy with jobs queued before this one (several apps
	// failing over to it at once run one after another). Give it time to get through the queue.
	unclaimedOnLiveNode = 10 * time.Minute
	// migrationRunLimit: the target has started the job but not finished it.
	migrationRunLimit = 5 * time.Minute
)

// migrationStuck reports whether a running migration should be given up on, and why. job and target are nil
// when they no longer exist.
func migrationStuck(m *models.Migration, job *models.Job, target *models.Node, now time.Time) (bool, string) {
	age := now.Sub(m.CreatedAt)
	if job == nil {
		return age > migrationRunLimit, "no job"
	}
	switch job.Status {
	case models.JobStatusPending:
		live := target != nil && target.Online && now.Sub(target.LastHeartbeat) <= heartbeatTimeout
		if !live {
			return age > unclaimedOnQuietNode, "target is not answering"
		}
		return age > unclaimedOnLiveNode, "target never got to it"
	case models.JobStatusRunning:
		started := job.CreatedAt
		if job.StartedAt != nil {
			started = *job.StartedAt
		}
		return now.Sub(started) > migrationRunLimit, "target did not finish it"
	default:
		// The job ended without its completion reaching the migration (failed as stuck, cancelled).
		return job.Status != models.JobStatusCompleted || age > migrationRunLimit, "job " + job.Status
	}
}

// cleanupStuckMigrations fails migrations that migrationStuck gives up on, which lets the health checker try
// the next node.
func (s *MigrationService) cleanupStuckMigrations() {
	var running []models.Migration
	s.db.Where("status = ? AND created_at < ?", models.MigrationStatusRunning,
		time.Now().Add(-unclaimedOnQuietNode)).Find(&running)

	now := time.Now()
	var migrations []models.Migration
	for i := range running {
		m := &running[i]
		var job *models.Job
		if m.JobID != "" {
			var j models.Job
			if s.db.First(&j, "id = ?", m.JobID).Error == nil {
				job = &j
			}
		}
		var target *models.Node
		var n models.Node
		if s.db.First(&n, "id = ?", m.TargetNodeID).Error == nil {
			target = &n
		}
		if stuck, why := migrationStuck(m, job, target, now); stuck {
			log.Printf("⚠️ Found stuck migration: %s (created: %v, %s)", m.ID, m.CreatedAt, why)
			migrations = append(migrations, *m)
		}
	}

	for _, migration := range migrations {
		migration.Status = models.MigrationStatusFailed
		s.db.Save(&migration)
		// If the target never picked the job up, make sure it doesn't start
		// a stray copy when it comes back.
		if migration.JobID != "" {
			s.db.Model(&models.Job{}).
				Where("id = ? AND status = ?", migration.JobID, models.JobStatusPending).
				Update("status", models.JobStatusCancelled)
		}
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

	migration, err := s.startMigration(&project, &sourceNode, &targetNode)
	if err != nil {
		log.Printf("⚠️ MigrateToNode: %v", err)
		return
	}
	log.Printf("🎯 Master enforcement migration %s: %s → %s", migration.ID, sourceNode.Hostname, targetNode.Hostname)
}

// startMigration redeploys project on target and records the migration. The
// old container is removed only once the new one runs (JobService.Complete).
func (s *MigrationService) startMigration(project *models.Project, source, target *models.Node) (*models.Migration, error) {
	if project.Image == "" {
		return nil, fmt.Errorf("project %s has no image to migrate", project.Name)
	}
	job, _, err := s.deployer.Dispatch(project, target, project.Image, DeployMeta{
		Trigger:    TriggerMigration,
		Repository: project.Repository,
	})
	if err != nil {
		return nil, err
	}
	migration := &models.Migration{
		ID:           uuid.New().String(),
		ProjectID:    project.ID,
		ContainerID:  project.ContainerID,
		SourceNodeID: source.ID,
		TargetNodeID: target.ID,
		Status:       models.MigrationStatusRunning,
		JobID:        job.ID,
		CreatedAt:    time.Now(),
	}
	if err := s.db.Create(migration).Error; err != nil {
		return nil, fmt.Errorf("failed to record migration: %w", err)
	}
	s.db.Model(project).Update("health_status", "migrating")
	return migration, nil
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

	if !master.Online || master.Maintenance {
		// Master is offline or in maintenance, find healthiest available node
		var healthiest models.Node
		if err := s.db.Scopes(models.Available).Where("id != ?", masterNodeID).
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

	migration, err := s.startMigration(&project, &sourceNode, &targetNode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("🔄 Project migration started: %s (%s) -> %s", project.Name, sourceNode.Hostname, targetNode.Hostname)

	c.JSON(http.StatusAccepted, gin.H{
		"migration":    migration,
		"start_job_id": migration.JobID,
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
