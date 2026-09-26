package services

import (
	"fmt"
	"log"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

type ProjectService struct {
	db       *gorm.DB
	deployer *Deployer
	// onRoutesChanged is called after a project's domain, port or node may
	// have changed, so nginx can be regenerated. Optional.
	onRoutesChanged func()
}

func NewProjectService(db *gorm.DB, deployer *Deployer) *ProjectService {
	return &ProjectService{db: db, deployer: deployer}
}

// SetRoutesChangedHook registers fn to run (in the background) after a
// project is created, updated or deleted.
func (s *ProjectService) SetRoutesChangedHook(fn func()) {
	s.onRoutesChanged = fn
}

func (s *ProjectService) routesChanged() {
	if s.onRoutesChanged != nil {
		go s.onRoutesChanged()
	}
}

// RepairInvalidPorts switches projects whose saved port mappings can't be
// used (such as "0000:8000") to automatic ports, so their next deploy works.
func (s *ProjectService) RepairInvalidPorts() {
	var projects []models.Project
	s.db.Find(&projects)
	for _, p := range projects {
		for _, m := range p.Ports {
			if _, _, err := models.ParsePortMapping(m); err != nil {
				log.Printf("🔧 Project %s had an invalid port mapping %q; switching it to automatic ports", p.Name, m)
				s.db.Model(&p).Select("ports", "auto_port").Updates(&models.Project{Ports: []string{}, AutoPort: true})
				break
			}
		}
	}
}

func (s *ProjectService) ListProjects(c *gin.Context) {
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

	var projects []models.Project
	var total int64

	s.db.Model(&models.Project{}).Count(&total)
	s.db.Order("name ASC").Limit(limitInt).Offset(offset).Find(&projects)

	c.JSON(http.StatusOK, gin.H{
		"data": projects,
		"pagination": gin.H{
			"page":  pageInt,
			"limit": limitInt,
			"total": total,
			"pages": (total + int64(limitInt) - 1) / int64(limitInt),
		},
	})
}

func (s *ProjectService) GetProject(c *gin.Context) {
	id := c.Param("id")
	var project models.Project
	if err := s.db.First(&project, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	c.JSON(http.StatusOK, project)
}

func (s *ProjectService) GetProjectsByNode(c *gin.Context) {
	nodeID := c.Param("id")
	var projects []models.Project
	s.db.Where("node_id = ?", nodeID).Find(&projects)
	c.JSON(http.StatusOK, projects)
}

func (s *ProjectService) CreateProject(c *gin.Context) {
	var req struct {
		Name        string          `json:"name" binding:"required"`
		Description string          `json:"description"`
		Domain      string          `json:"domain"`
		NodeID      string          `json:"node_id" binding:"required"`
		Image       string          `json:"image"`
		Ports       []string        `json:"ports"`
		EnvVars     []models.EnvVar `json:"env_vars"`
		Volumes     []string        `json:"volumes"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := models.ValidateProjectConfig(req.Name, req.Domain, req.Ports, req.EnvVars); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var node models.Node
	if err := s.db.First(&node, "id = ?", req.NodeID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}

	project := &models.Project{
		ID:           uuid.New().String(),
		Name:         req.Name,
		Description:  req.Description,
		Domain:       req.Domain,
		NodeID:       req.NodeID,
		Status:       "running",
		HealthStatus: "unknown",
		Image:        req.Image,
		Ports:        req.Ports,
		AutoPort:     len(req.Ports) == 0,
		EnvVars:      req.EnvVars,
		Volumes:      req.Volumes,
		LastDeployed: time.Now(),
		CreatedAt:    time.Now(),
	}

	if err := s.db.Create(project).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	s.routesChanged()
	c.JSON(http.StatusCreated, project)
}
func (s *ProjectService) UpdateProject(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Name         string          `json:"name"`
		Description  string          `json:"description"`
		Domain       string          `json:"domain"`
		NodeID       string          `json:"node_id"`
		Image        string          `json:"image"`
		Ports        []string        `json:"ports"`
		EnvVars      []models.EnvVar `json:"env_vars"`
		Volumes      []string        `json:"volumes"`
		Status       string          `json:"status"`
		HealthStatus string          `json:"health_status"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := models.ValidateProjectConfig(req.Name, req.Domain, req.Ports, req.EnvVars); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var project models.Project
	if err := s.db.First(&project, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}

	if req.Name != "" {
		project.Name = req.Name
	}
	if req.Description != "" {
		project.Description = req.Description
	}
	if req.Domain != "" {
		project.Domain = req.Domain
	}
	if req.NodeID != "" {
		project.NodeID = req.NodeID
	}
	if req.Image != "" {
		project.Image = req.Image
	}
	before := containerConfig(&project)
	if req.Ports != nil {
		// An empty list hands the port back to the hub to choose.
		project.Ports = req.Ports
		project.AutoPort = len(req.Ports) == 0
	}
	if req.EnvVars != nil {
		// Clients get values back masked; a masked value means "unchanged".
		project.EnvVars = models.MergeMasked(req.EnvVars, project.EnvVars)
	}
	if req.Volumes != nil {
		project.Volumes = req.Volumes
	}
	if req.Status != "" {
		project.Status = req.Status
	}
	if req.HealthStatus != "" {
		project.HealthStatus = req.HealthStatus
	}

	s.db.Save(&project)
	s.routesChanged()
	// Containers only read their config at start, so apply it now.
	if !reflect.DeepEqual(before, containerConfig(&project)) {
		if _, err := s.redeploy(&project); err != nil {
			log.Printf("⚠️ Config of %s changed but it could not be redeployed yet: %v", project.Name, err)
		}
	}
	c.JSON(http.StatusOK, project)
}

// containerConfig is the part of a project a running container has baked in.
func containerConfig(p *models.Project) []interface{} {
	return []interface{}{
		append([]string{}, p.Ports...), p.AutoPort,
		append([]models.EnvVar{}, p.EnvVars...),
		append([]string{}, p.Volumes...),
	}
}

// Redeploy handles POST /projects/:id/redeploy: restart the project's current
// image on its node with its current config.
func (s *ProjectService) Redeploy(c *gin.Context) {
	var project models.Project
	if err := s.db.First(&project, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	job, err := s.redeploy(&project)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"job_id": job.ID, "node_id": job.NodeID})
}

func (s *ProjectService) redeploy(project *models.Project) (*models.Job, error) {
	if project.Image == "" {
		return nil, fmt.Errorf("project has no image yet; deploy it from CI first")
	}
	var node models.Node
	if err := s.db.First(&node, "id = ? AND online = ?", project.NodeID, true).Error; err != nil {
		if err := s.db.Where("online = ?", true).Order("health_score desc").First(&node).Error; err != nil {
			return nil, fmt.Errorf("no online node to run it on")
		}
	}
	job, _, err := s.deployer.Dispatch(project, &node, project.Image, DeployMeta{
		Trigger:    TriggerMigration,
		Repository: project.Repository,
	})
	return job, err
}

func (s *ProjectService) UpdateProjectStatus(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Status       string `json:"status"`
		HealthStatus string `json:"health_status"`
		Uptime       int64  `json:"uptime"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var project models.Project
	if err := s.db.First(&project, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}

	if req.Status != "" {
		project.Status = req.Status
	}
	if req.HealthStatus != "" {
		project.HealthStatus = req.HealthStatus
	}
	if req.Uptime > 0 {
		project.Uptime = req.Uptime
	}

	s.db.Save(&project)
	c.JSON(http.StatusOK, project)
}

func (s *ProjectService) DeleteProject(c *gin.Context) {
	id := c.Param("id")
	if err := s.db.Delete(&models.Project{}, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	s.routesChanged()
	c.JSON(http.StatusOK, gin.H{"message": "project deleted"})
}
