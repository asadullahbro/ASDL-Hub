package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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

	environment := claims.Environment
	if environment == "" {
		environment = req.Environment
	}
	if environment == "" {
		environment = "production"
	}

	project, created, err := h.findOrCreateProject(claims, req.Image)
	if err != nil {
		slog.Error("failed to find or create project", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve project"})
		return
	}
	if created {
		slog.Info("created new project",
			"project_id", project.ID,
			"node_id", project.NodeID,
			"repository", claims.Repository,
		)
	}

	deployment := &models.OIDCDeployment{
		ID:          uuid.New().String(),
		Repository:  claims.Repository,
		Environment: environment,
		ProjectID:   project.ID,
		NodeID:      project.NodeID,
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

	if err := h.dispatch(project, deployment, req.Image, claims.SHA); err != nil {
		h.markStatus(deployment.ID, "failed", err.Error())
		slog.Error("dispatch failed", "error", err, "node_id", project.NodeID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to dispatch deployment"})
		return
	}

	h.markStatus(deployment.ID, "dispatched", "")

	c.JSON(http.StatusAccepted, models.DeployResponse{
		DeploymentID: deployment.ID,
		ProjectID:    project.ID,
		NodeID:       project.NodeID,
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

// findOrCreateProject finds an existing project by repository, or creates one
// on the healthiest available node. Safe against concurrent first-deploys via
// unique index on Project.Repository — loser of the race fetches the winner's row.
func (h *DeployHandler) findOrCreateProject(claims *githuboidc.OIDCClaims, image string) (*models.Project, bool, error) {
	var project models.Project

	err := h.db.First(&project, "repository = ?", claims.Repository).Error
	if err == nil {
		h.db.Model(&project).Updates(map[string]interface{}{
			"image":         image,
			"last_deployed": time.Now(),
			"updated_at":    time.Now(),
		})
		return &project, false, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, false, fmt.Errorf("project lookup failed: %w", err)
	}

	node, err := h.bestNode()
	if err != nil {
		return nil, false, fmt.Errorf("no available node: %w", err)
	}

	_, repoName, _ := splitRepo(claims.Repository)

	project = models.Project{
		ID:           uuid.New().String(),
		Name:         repoName,
		Repository:   claims.Repository,
		NodeID:       node.ID,
		Image:        image,
		Status:       "deploying",
		HealthStatus: "unknown",
		LastDeployed: time.Now(),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := h.db.Create(&project).Error; err != nil {
		// Lost the race — fetch the row the winner created
		if err2 := h.db.First(&project, "repository = ?", claims.Repository).Error; err2 != nil {
			return nil, false, fmt.Errorf("failed to create or fetch project: %w", err2)
		}
		return &project, false, nil
	}

	return &project, true, nil
}

// bestNode returns the online node with the highest health score.
// Falls back to master node setting if configured, then any online node.
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

func (h *DeployHandler) dispatch(project *models.Project, deployment *models.OIDCDeployment, image, sha string) error {
	// Fetch GitHub token for private repo access
	var setting models.Setting
	gitToken := ""
	if h.db.First(&setting, "key = ?", "github_token").Error == nil {
		gitToken = setting.Value
	}

	repoURL := fmt.Sprintf("https://github.com/%s.git", deployment.Repository)
	if gitToken != "" {
		repoURL = fmt.Sprintf("https://%s@github.com/%s.git", gitToken, deployment.Repository)
	}

	command := fmt.Sprintf(
		"cd /tmp && rm -rf %s && git clone %s %s && cd %s && docker build -t %s . && docker stop %s 2>/dev/null || true && docker rm %s 2>/dev/null || true && docker run -d --name %s %s",
		project.Name, repoURL, project.Name,
		project.Name,
		project.Name,
		project.Name, project.Name,
		project.Name, project.Name,
	)

	job := &models.Job{
		ID:         uuid.New().String(),
		NodeID:     project.NodeID,
		Type:       models.JobTypeDeploy,
		Status:     models.JobStatusPending,
		Command:    command,
		MaxRetries: 1,
		CreatedAt:  time.Now(),
	}
	if err := h.db.Create(job).Error; err != nil {
		return fmt.Errorf("failed to create deploy job: %w", err)
	}

	dep := &models.Deployment{
		ID:            uuid.New().String(),
		JobID:         job.ID,
		NodeID:        project.NodeID,
		Repository:    deployment.Repository,
		Branch:        deployment.Ref,
		Commit:        sha,
		ImageName:     image,
		ContainerName: project.Name,
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
		"node_id", project.NodeID,
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

func splitRepo(repo string) (owner, name string, err error) {
	for i, c := range repo {
		if c == '/' {
			return repo[:i], repo[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid repo format: %q", repo)
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
