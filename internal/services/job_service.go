package services

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

type JobService struct {
	db *gorm.DB
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

	var node models.Node
	if err := s.db.First(&node, "id = ? OR vpn_ip = ?", job.NodeID, vpnIP).Error; err != nil {
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

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
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

	c.JSON(http.StatusCreated, newJob)
}
