package services

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

type ContainerService struct {
	db *gorm.DB
}

func NewContainerService(db *gorm.DB) *ContainerService {
	return &ContainerService{db: db}
}

func (s *ContainerService) ListContainers(c *gin.Context) {
	nodeID := c.Query("node_id")
	var containers []models.Container

	query := s.db
	if nodeID != "" {
		query = query.Where("node_id = ?", nodeID)
	}

	if err := query.Find(&containers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, containers)
}

func (s *ContainerService) GetContainer(c *gin.Context) {
	id := c.Param("id")
	var container models.Container
	if err := s.db.First(&container, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
		return
	}
	c.JSON(http.StatusOK, container)
}

func (s *ContainerService) StopContainer(c *gin.Context) {
	id := c.Param("id")
	var container models.Container
	if err := s.db.First(&container, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
		return
	}

	job := &models.Job{
		ID:         uuid.New().String(),
		NodeID:     container.NodeID,
		Type:       "container_stop",
		Status:     models.JobStatusPending,
		Command:    fmt.Sprintf("docker stop %s", container.Name),
		MaxRetries: 2,
		CreatedAt:  time.Now(),
	}

	if err := s.db.Create(job).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	container.Status = "stopped"
	s.db.Save(&container)

	c.JSON(http.StatusOK, gin.H{
		"message": "container stop initiated",
		"job_id":  job.ID,
	})
}

func (s *ContainerService) StartContainer(c *gin.Context) {
	id := c.Param("id")
	var container models.Container
	if err := s.db.First(&container, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
		return
	}

	job := &models.Job{
		ID:         uuid.New().String(),
		NodeID:     container.NodeID,
		Type:       "container_start",
		Status:     models.JobStatusPending,
		Command:    fmt.Sprintf("docker start %s", container.Name),
		MaxRetries: 2,
		CreatedAt:  time.Now(),
	}

	if err := s.db.Create(job).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	container.Status = "running"
	s.db.Save(&container)

	c.JSON(http.StatusOK, gin.H{
		"message": "container start initiated",
		"job_id":  job.ID,
	})
}

func (s *ContainerService) RestartContainer(c *gin.Context) {
	id := c.Param("id")
	var container models.Container
	if err := s.db.First(&container, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
		return
	}

	job := &models.Job{
		ID:         uuid.New().String(),
		NodeID:     container.NodeID,
		Type:       "container_restart",
		Status:     models.JobStatusPending,
		Command:    fmt.Sprintf("docker restart %s", container.Name),
		MaxRetries: 3,
		CreatedAt:  time.Now(),
	}

	if err := s.db.Create(job).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "container restart initiated",
		"job_id":  job.ID,
	})
}
