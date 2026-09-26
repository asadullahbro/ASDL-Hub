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

type JobService struct {
	db *gorm.DB
	// onRoutesChanged is called after a deploy changes which node serves a
	// project, so nginx can be regenerated. Optional.
	onRoutesChanged func()
}

// SetRoutesChangedHook registers fn to run (in the background) whenever a
// finished deploy changes a project's status or node.
func (s *JobService) SetRoutesChangedHook(fn func()) {
	s.onRoutesChanged = fn
}

func NewJobService(db *gorm.DB) *JobService {
	return &JobService{
		db: db,
	}
}

func (s *JobService) Create(c *gin.Context) {
	var req struct {
		NodeID      string             `json:"node_id" binding:"required"`
		Type        string             `json:"type" binding:"required"`
		Command     string             `json:"command" binding:"required"`
		WorkingDir  string             `json:"working_dir"`
		Environment []string           `json:"environment"`
		Timeout     int                `json:"timeout"`
		Payload     *models.JobPayload `json:"payload,omitempty"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var node models.Node
	if err := s.db.First(&node, "id = ?", req.NodeID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}

	job := &models.Job{
		ID:          uuid.New().String(),
		NodeID:      req.NodeID,
		Type:        req.Type,
		Status:      models.JobStatusPending,
		Command:     req.Command,
		Payload:     req.Payload,
		WorkingDir:  req.WorkingDir,
		Environment: req.Environment,
		Timeout:     req.Timeout,
		MaxRetries:  3,
		CreatedAt:   time.Now(),
	}

	if err := s.db.Create(job).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, job)
}

func (s *JobService) Claim(c *gin.Context) {
	nodeID := c.Query("node_id")

	vpnIP, exists := c.Get("vpn_ip")
	if !exists {
		vpnIP = c.ClientIP()
	}

	log.Printf("Claiming job for node: %s, VPN IP: %v", nodeID, vpnIP)

	var node models.Node
	err := s.db.Where("id = ? OR vpn_ip = ?", nodeID, vpnIP).First(&node).Error
	if err != nil {
		log.Printf("Node not found: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}

	log.Printf("Found node: %s (ID: %s)", node.Hostname, node.ID)

	var job models.Job
	err = s.db.Where("node_id = ? AND status = ?", node.ID, models.JobStatusPending).
		Order("created_at ASC").
		First(&job).Error

	if err != nil {
		c.JSON(http.StatusNoContent, nil)
		return
	}

	job.Status = models.JobStatusRunning
	now := time.Now()
	job.StartedAt = &now
	s.db.Save(&job)

	c.JSON(http.StatusOK, job)
}

func (s *JobService) Complete(c *gin.Context) {
	jobID := c.Param("id")

	vpnIP, exists := c.Get("vpn_ip")
	if !exists {
		vpnIP = c.ClientIP()
	}

	var req struct {
		Status   string `json:"status"`
		Logs     string `json:"logs"`
		ExitCode int    `json:"exit_code"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var job models.Job
	if err := s.db.First(&job, "id = ?", jobID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	// The completing caller must actually BE the node the job was assigned
	// to. The old check ("id = job.NodeID OR vpn_ip = vpnIP") matched the
	// first clause unconditionally — job.NodeID is always a valid node id —
	// so any caller who knew a job ID could mark it complete regardless of
	// which node they were calling from.
	var node models.Node
	if err := s.db.First(&node, "vpn_ip = ?", vpnIP).Error; err != nil || node.ID != job.NodeID {
		c.JSON(http.StatusForbidden, gin.H{"error": "not authorized"})
		return
	}

	job.Status = req.Status
	job.Logs = req.Logs
	job.ExitCode = req.ExitCode
	now := time.Now()
	job.CompletedAt = &now
	s.db.Save(&job)

	if job.Type == models.JobTypeMigrateStart || job.Type == models.JobTypeFailoverStart {
		var migration models.Migration
		if err := s.db.Where("job_id = ?", job.ID).First(&migration).Error; err == nil {
			if job.Status == models.JobStatusCompleted {
				migration.Status = models.MigrationStatusCompleted
			} else {
				migration.Status = models.MigrationStatusFailed
			}
			completedAt := now
			migration.CompletedAt = &completedAt
			s.db.Save(&migration)
			log.Printf("✅ Migration %s marked as %s", migration.ID, migration.Status)
		}
	}

	if job.Type == models.JobTypeDeploy {
		s.completeDeploy(&job, now)
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// completeDeploy records a finished deploy job on its deployment records and
// project. On success the project is marked running on the job's node, and if
// it previously ran on another node, the old container there is removed.
func (s *JobService) completeDeploy(job *models.Job, now time.Time) {
	succeeded := job.Status == models.JobStatusCompleted

	var dep models.Deployment
	if err := s.db.Where("job_id = ?", job.ID).First(&dep).Error; err != nil {
		log.Printf("⚠️ No deployment record for deploy job %s: %v", job.ID, err)
		return
	}
	depStatus := models.DeploymentStatusCompleted
	if !succeeded {
		depStatus = models.DeploymentStatusFailed
	}
	s.db.Model(&dep).Updates(map[string]interface{}{
		"status":       depStatus,
		"logs":         job.Logs,
		"started_at":   job.StartedAt,
		"completed_at": now,
	})

	oidcStatus := "succeeded"
	if !succeeded {
		oidcStatus = "failed"
	}
	s.db.Model(&models.OIDCDeployment{}).Where("job_id = ?", job.ID).Update("status", oidcStatus)

	var project models.Project
	if err := s.db.Where("repository = ?", dep.Repository).First(&project).Error; err != nil {
		log.Printf("⚠️ No project for deployment %s (%s): %v", dep.ID, dep.Repository, err)
		return
	}
	// A newer deploy has been dispatched since this one; let that one decide.
	if project.DeploymentID != "" && project.DeploymentID != dep.ID {
		return
	}

	if !succeeded {
		log.Printf("❌ Deploy of %s failed on node %s (job %s)", project.Name, job.NodeID, job.ID)
		if project.NodeID == job.NodeID {
			// The deploy pulls before removing the old container, so a failed
			// pull leaves the previous version serving. Hand the project back
			// to the health checker to find out which it is.
			s.db.Model(&project).Updates(map[string]interface{}{"status": "running", "health_status": "unknown"})
		} else {
			s.db.Model(&project).Updates(map[string]interface{}{"status": "failed", "health_status": "unhealthy"})
		}
		s.routesChanged()
		return
	}

	previousNode := project.NodeID
	s.db.Model(&project).Updates(map[string]interface{}{
		"status":        "running",
		"health_status": "unknown",
		"node_id":       job.NodeID,
		"image":         dep.ImageName,
		"last_deployed": now,
	})
	log.Printf("✅ Deployed %s (%s) on node %s", project.Name, dep.ImageName, job.NodeID)

	if previousNode != "" && previousNode != job.NodeID {
		stop := &models.Job{
			ID:         uuid.New().String(),
			NodeID:     previousNode,
			Type:       models.JobTypeFailoverStop,
			Status:     models.JobStatusPending,
			Command:    BuildStopCommand(project.Name),
			MaxRetries: 2,
			CreatedAt:  now,
		}
		if err := s.db.Create(stop).Error; err != nil {
			log.Printf("⚠️ Failed to queue removal of old %s container on node %s: %v", project.Name, previousNode, err)
		}
	}
	s.routesChanged()
}

func (s *JobService) routesChanged() {
	if s.onRoutesChanged != nil {
		go s.onRoutesChanged()
	}
}

// redactEnvironment hides env var values, which can hold registry tokens,
// from job API responses. Only the claiming node gets them in full.
func redactEnvironment(job *models.Job) {
	for i, e := range job.Environment {
		if k, _, ok := strings.Cut(e, "="); ok {
			job.Environment[i] = k + "=********"
		}
	}
}

func (s *JobService) List(c *gin.Context) {
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

	var jobs []models.Job
	var total int64

	s.db.Model(&models.Job{}).Count(&total)
	s.db.Order("created_at DESC").Limit(limitInt).Offset(offset).Find(&jobs)
	for i := range jobs {
		redactEnvironment(&jobs[i])
	}

	c.JSON(http.StatusOK, gin.H{
		"data": jobs,
		"pagination": gin.H{
			"page":  pageInt,
			"limit": limitInt,
			"total": total,
			"pages": (total + int64(limitInt) - 1) / int64(limitInt),
		},
	})
}

func (s *JobService) Get(c *gin.Context) {
	id := c.Param("id")
	var job models.Job
	if err := s.db.First(&job, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	redactEnvironment(&job)
	c.JSON(http.StatusOK, job)
}

func (s *JobService) GetLogs(c *gin.Context) {
	id := c.Param("id")
	var job models.Job
	if err := s.db.First(&job, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": job.Logs})
}

func (s *JobService) Retry(c *gin.Context) {
	id := c.Param("id")
	var job models.Job
	if err := s.db.First(&job, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	if job.Status != models.JobStatusFailed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only failed jobs can be retried"})
		return
	}

	newJob := &models.Job{
		ID:          uuid.New().String(),
		NodeID:      job.NodeID,
		Type:        job.Type,
		Status:      models.JobStatusPending,
		Command:     job.Command,
		Payload:     job.Payload,
		WorkingDir:  job.WorkingDir,
		Environment: job.Environment,
		Timeout:     job.Timeout,
		MaxRetries:  job.MaxRetries,
		CreatedAt:   time.Now(),
	}

	if err := s.db.Create(newJob).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	redactEnvironment(newJob)
	c.JSON(http.StatusCreated, newJob)
}
