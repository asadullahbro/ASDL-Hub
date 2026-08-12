package services

import (
    "strconv"
    "net/http"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
    "gorm.io/gorm"

    "github.com/asdl/hub/internal/models"
)

type ProjectService struct {
    db *gorm.DB
}

func NewProjectService(db *gorm.DB) *ProjectService {
    return &ProjectService{db: db}
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
            "page": pageInt,
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
        EnvVars:      req.EnvVars,
        Volumes:      req.Volumes,
        LastDeployed: time.Now(),
        CreatedAt:    time.Now(),
    }

    if err := s.db.Create(project).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusCreated, project)
}
func (s *ProjectService) UpdateProject(c *gin.Context) {
    id := c.Param("id")
    var req struct {
        Name        string          `json:"name"`
        Description string          `json:"description"`
        Domain      string          `json:"domain"`
        NodeID      string          `json:"node_id"`
        Image       string          `json:"image"`
        Ports       []string        `json:"ports"`
        EnvVars     []models.EnvVar `json:"env_vars"`
        Volumes     []string        `json:"volumes"`
        Status      string          `json:"status"`
        HealthStatus string         `json:"health_status"`
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

    if req.Name != ""        { project.Name = req.Name }
    if req.Description != "" { project.Description = req.Description }
    if req.Domain != ""      { project.Domain = req.Domain }
    if req.NodeID != ""      { project.NodeID = req.NodeID }
    if req.Image != ""       { project.Image = req.Image }
    if req.Ports != nil      { project.Ports = req.Ports }
    if req.EnvVars != nil    { project.EnvVars = req.EnvVars }
    if req.Volumes != nil    { project.Volumes = req.Volumes }
    if req.Status != ""      { project.Status = req.Status }
    if req.HealthStatus != "" { project.HealthStatus = req.HealthStatus }

    s.db.Save(&project)
    c.JSON(http.StatusOK, project)
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
    c.JSON(http.StatusOK, gin.H{"message": "project deleted"})
}