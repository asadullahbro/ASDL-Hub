package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	githuboidc "github.com/asdl/hub/internal/github"
	"github.com/asdl/hub/internal/models"
)

type DeployHandler struct {
	db       *gorm.DB
	verifier *githuboidc.OIDCVerifier
}

func NewDeployHandler(db *gorm.DB, hubAudience string) *DeployHandler {
	return &DeployHandler{
		db:       db,
		verifier: githuboidc.NewOIDCVerifier(hubAudience),
	}
}

// Deploy handles POST /api/v1/deploy
func (h *DeployHandler) Deploy(c *gin.Context) {
	var req models.DeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	// 1. Verify OIDC token
	claims, err := h.verifier.Verify(req.OIDCToken)
	if err != nil {
		slog.Warn("OIDC verification failed",
			"error", err,
			"repository", extractRepoFromToken(req.OIDCToken),
		)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "token verification failed"})
		return
	}

	slog.Info("OIDC token verified",
		"repository", claims.Repository,
		"sha", claims.SHA,
		"workflow", claims.Workflow,
	)

	// 2. Resolve environment
	environment := claims.Environment
	if environment == "" {
		environment = req.Environment
	}
	if environment == "" {
		environment = "production"
	}

	// 3. Authorize — look up allowed repo
	var allowed models.AllowedRepo
	err = h.db.First(&allowed,
		"repository = ? AND environment = ? AND enabled = ?",
		claims.Repository, environment, true,
	).Error
	if err == gorm.ErrRecordNotFound {
		slog.Warn("repository not authorized",
			"repository", claims.Repository,
			"environment", environment,
		)
		c.JSON(http.StatusForbidden, gin.H{
			"error": fmt.Sprintf(
				"repository %q is not authorized for environment %q",
				claims.Repository, environment,
			),
		})
		return
	}
	if err != nil {
		slog.Error("failed to query allowed repos", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// 4. Load the project
	var project models.Project
	if err := h.db.First(&project, "id = ?", allowed.ProjectID).Error; err != nil {
		slog.Error("project not found", "project_id", allowed.ProjectID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "project not found"})
		return
	}

	// 5. Health-aware node selection for this deployment
	node, err := h.bestNode()
	if err != nil {
		slog.Error("no available node", "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no available nodes"})
		return
	}

	// 6. Record the OIDC deployment
	deployment := &models.OIDCDeployment{
		ID:          uuid.New().String(),
		Repository:  claims.Repository,
		Environment: environment,
		ProjectID:   project.ID,
		NodeID:      node.ID,
		SHA:         claims.SHA,
		Ref:         claims.Ref,
		Workflow:    claims.Workflow,
		RunID:       claims.RunID,
		Image:       req.Image,
		Status:      "pending",
		CreatedAt:   time.Now(),
	}
	if err := h.db.Create(deployment).Error; err != nil {
		slog.Error("failed to record deployment", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// 7. Dispatch job to selected node
	if err := h.dispatch(&project, deployment, node, req.Image, claims.SHA); err != nil {
		h.markStatus(deployment.ID, "failed", err.Error())
		slog.Error("dispatch failed", "error", err, "node_id", node.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to dispatch deployment"})
		return
	}

	h.markStatus(deployment.ID, "dispatched", "")

	c.JSON(http.StatusAccepted, models.DeployResponse{
		DeploymentID: deployment.ID,
		ProjectID:    project.ID,
		NodeID:       node.ID,
		Status:       "dispatched",
	})
}

// ListDeployments handles GET /api/v1/deploy/history
func (h *DeployHandler) ListDeployments(c *gin.Context) {
	var deployments []models.OIDCDeployment
	if err := h.db.Order("created_at desc").Limit(100).Find(&deployments).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch deployments"})
		return
	}
	c.JSON(http.StatusOK, deployments)
}

// ListAllowed handles GET /api/v1/deploy/allowed
func (h *DeployHandler) ListAllowed(c *gin.Context) {
	var allowed []models.AllowedRepo
	if err := h.db.Order("created_at desc").Find(&allowed).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch allowed repos"})
		return
	}
	c.JSON(http.StatusOK, allowed)
}

// AddAllowed handles POST /api/v1/deploy/allowed
func (h *DeployHandler) AddAllowed(c *gin.Context) {
	var input struct {
		Repository  string `json:"repository" binding:"required"`
		Environment string `json:"environment"`
		ProjectID   string `json:"project_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !strings.Contains(input.Repository, "/") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repository must be in owner/repo format"})
		return
	}

	if input.Environment == "" {
		input.Environment = "production"
	}

	// Verify project exists
	var project models.Project
	if err := h.db.First(&project, "id = ?", input.ProjectID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project not found"})
		return
	}

	allowed := models.AllowedRepo{
		ID:          uuid.New().String(),
		Repository:  input.Repository,
		Environment: input.Environment,
		ProjectID:   input.ProjectID,
		Enabled:     true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := h.db.Create(&allowed).Error; err != nil {
		slog.Error("failed to create allowed repo", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create entry"})
		return
	}
	c.JSON(http.StatusCreated, allowed)
}

// RemoveAllowed handles DELETE /api/v1/deploy/allowed/:id
func (h *DeployHandler) RemoveAllowed(c *gin.Context) {
	id := c.Param("id")
	if err := h.db.Delete(&models.AllowedRepo{}, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete entry"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// bestNode returns the online node with the highest health score.
// Falls back to master node if configured.
func (h *DeployHandler) bestNode() (*models.Node, error) {
	var masterSetting models.Setting
	if h.db.First(&masterSetting, "key = ?", "master_node_id").Error == nil && masterSetting.Value != "" {
		var master models.Node
		if h.db.First(&master, "id = ? AND online = ?", masterSetting.Value, true).Error == nil {
			return &master, nil
		}
	}

	var node models.Node
	if err := h.db.Where("online = ?", true).
		Order("health_score desc").
		First(&node).Error; err != nil {
		return nil, fmt.Errorf("no online nodes available")
	}
	return &node, nil
}

func (h *DeployHandler) dispatch(project *models.Project, deployment *models.OIDCDeployment, node *models.Node, image, sha string) error {
	containerName := strings.ToLower(project.Name)

	job := &models.Job{
		ID:     uuid.New().String(),
		NodeID: node.ID,
		Type:   models.JobTypeDeploy,
		Status: models.JobStatusPending,
		Command: fmt.Sprintf(
			"docker pull %s && docker stop %s 2>/dev/null || true && docker rm %s 2>/dev/null || true && docker run -d --name %s %s",
			image,
			containerName, containerName,
			containerName, image,
		),
		MaxRetries: 1,
		CreatedAt:  time.Now(),
	}
	if err := h.db.Create(job).Error; err != nil {
		return fmt.Errorf("failed to create deploy job: %w", err)
	}

	dep := &models.Deployment{
		ID:            uuid.New().String(),
		JobID:         job.ID,
		NodeID:        node.ID,
		Repository:    deployment.Repository,
		Branch:        deployment.Ref,
		Commit:        sha,
		ImageName:     image,
		ContainerName: containerName,
		Type:          models.DeploymentTypeDocker,
		Status:        models.DeploymentStatusPending,
		CreatedAt:     time.Now(),
	}
	if err := h.db.Create(dep).Error; err != nil {
		return fmt.Errorf("failed to create deployment record: %w", err)
	}

	slog.Info("deploy job created",
		"job_id", job.ID,
		"project_id", project.ID,
		"node_id", node.ID,
		"image", image,
	)
	return nil
}

func (h *DeployHandler) markStatus(id, status, errMsg string) {
	updates := map[string]interface{}{"status": status}
	if errMsg != "" {
		updates["error"] = errMsg
	}
	if err := h.db.Model(&models.OIDCDeployment{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		slog.Warn("failed to update deployment status", "id", id, "status", status, "error", err)
	}
}

func splitJWT(token string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			parts = append(parts, token[start:i])
			start = i + 1
		}
	}
	return append(parts, token[start:])
}

func extractRepoFromToken(rawToken string) string {
	parts := splitJWT(rawToken)
	if len(parts) != 3 {
		return "<unparseable>"
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "<undecodable>"
	}
	var claims struct {
		Repository string `json:"repository"`
	}
	_ = json.Unmarshal(payload, &claims)
	return claims.Repository
}