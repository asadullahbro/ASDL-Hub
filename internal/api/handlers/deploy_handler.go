package handlers

import (
	"database/sql"
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

// DeployHandler handles OIDC-authenticated deployment requests from GitHub Actions.
type DeployHandler struct {
	db       *gorm.DB
	verifier *githuboidc.OIDCVerifier
}

// NewDeployHandler creates a DeployHandler. hubAudience is the Hub's public URL
// (e.g. "https://hub.example.com") — must match the `hub:` field in the workflow.
func NewDeployHandler(db *gorm.DB, hubAudience string) *DeployHandler {
	return &DeployHandler{
		db:       db,
		verifier: githuboidc.NewOIDCVerifier(hubAudience),
	}
}

// Deploy handles POST /api/v1/deploy
//
// Called by GitHub Actions. It:
//  1. Verifies the OIDC JWT (issuer, audience, signature, expiry)
//  2. Looks up the repo+environment -> project+node mapping
//  3. Records the deployment for audit
//  4. Dispatches a job to the target node via the agent
func (h *DeployHandler) Deploy(c *gin.Context) {
	var req models.DeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	// 1. Verify the OIDC token.
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
		"environment", claims.Environment,
		"workflow", claims.Workflow,
		"sha", claims.SHA,
		"run_id", claims.RunID,
	)

	// 2. Look up the authorization rule.
	rule, err := h.findRule(claims.Repository, req.Environment)
	if err == sql.ErrNoRows {
		slog.Warn("no deployment rule found",
			"repository", claims.Repository,
			"environment", req.Environment,
		)
		c.JSON(http.StatusForbidden, gin.H{
			"error": fmt.Sprintf(
				"repository %q is not authorized to deploy to environment %q",
				claims.Repository, req.Environment,
			),
		})
		return
	}
	if err != nil {
		slog.Error("failed to query deployment rule", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	if !rule.Enabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "deployment rule is disabled"})
		return
	}

	// Sanity: environment in the OIDC token must match what the workflow requested.
	// GitHub sets the environment claim only when the job targets a named environment.
	if claims.Environment != "" && claims.Environment != req.Environment {
		c.JSON(http.StatusForbidden, gin.H{
			"error": fmt.Sprintf(
				"OIDC environment claim %q does not match requested environment %q",
				claims.Environment, req.Environment,
			),
		})
		return
	}

	// 3. Record the deployment.
	deployment, err := h.recordDeployment(rule, claims, req.Image)
	if err != nil {
		slog.Error("failed to record deployment", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// 4. Dispatch to the agent on the target node.
	if err := h.dispatchToNode(rule, deployment, req.Image, claims.SHA); err != nil {
		h.markDeploymentStatus(deployment.ID, "failed", err.Error())
		slog.Error("failed to dispatch deployment",
			"deployment_id", deployment.ID,
			"node_id", rule.NodeID,
			"error", err,
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to dispatch deployment to node"})
		return
	}

	h.markDeploymentStatus(deployment.ID, "dispatched", "")

	c.JSON(http.StatusAccepted, models.DeployResponse{
		DeploymentID: deployment.ID,
		Status:       "dispatched",
		Message:      fmt.Sprintf("deployment %s dispatched to node %s", deployment.ID, rule.NodeID),
	})
}

func (h *DeployHandler) ListRules(c *gin.Context) {
	var rules []models.RepoDeploymentRule
	if err := h.db.Order("created_at desc").Find(&rules).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch rules"})
		return
	}
	c.JSON(http.StatusOK, rules)
}

// CreateRule handles POST /api/v1/deploy/rules
func (h *DeployHandler) CreateRule(c *gin.Context) {
	var r models.RepoDeploymentRule
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	owner, _, err := splitRepo(r.Repository)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repository must be in owner/repo format"})
		return
	}
	r.ID = uuid.New().String()
	r.RepoOwner = owner
	r.Enabled = true
	r.CreatedAt = time.Now()
	r.UpdatedAt = time.Now()

	if err := h.db.Create(&r).Error; err != nil {
		slog.Error("failed to create deployment rule", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create rule"})
		return
	}
	c.JSON(http.StatusCreated, r)
}

// DeleteRule handles DELETE /api/v1/deploy/rules/:id
func (h *DeployHandler) DeleteRule(c *gin.Context) {
	id := c.Param("id")
	if err := h.db.Delete(&models.RepoDeploymentRule{}, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete rule"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
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

// --- internal helpers ---

func (h *DeployHandler) findRule(repo, env string) (*models.RepoDeploymentRule, error) {
	var rule models.RepoDeploymentRule
	err := h.db.First(&rule, "repository = ? AND environment = ?", repo, env).Error
	return &rule, err
}

func (h *DeployHandler) recordDeployment(rule *models.RepoDeploymentRule, claims *githuboidc.OIDCClaims, image string) (*models.OIDCDeployment, error) {
	d := &models.OIDCDeployment{
		ID:          uuid.New().String(),
		RuleID:      rule.ID,
		Repository:  claims.Repository,
		Environment: rule.Environment,
		SHA:         claims.SHA,
		Ref:         claims.Ref,
		Workflow:    claims.Workflow,
		RunID:       claims.RunID,
		Image:       image,
		Status:      "pending",
		CreatedAt:   time.Now(),
	}
	err := h.db.Create(d).Error
	return d, err
}

func (h *DeployHandler) markDeploymentStatus(id, status, errMsg string) {
	updates := map[string]interface{}{"status": status}
	if errMsg != "" {
		updates["error"] = errMsg
	}
	h.db.Model(&models.OIDCDeployment{}).Where("id = ?", id).Updates(updates)
}

// dispatchToNode enqueues a deploy job for the target node.
func (h *DeployHandler) dispatchToNode(rule *models.RepoDeploymentRule, deployment *models.OIDCDeployment, image, sha string) error {
	var project models.Project
	if err := h.db.First(&project, "id = ?", rule.ProjectID).Error; err != nil {
		return fmt.Errorf("project not found: %w", err)
	}

	payload := &models.JobPayload{
		Image:         image,
		ContainerName: project.Name,
		Operation:     "deploy",
		Repository:    deployment.Repository,
		LastDeployed:  time.Now(),
	}

	job := &models.Job{
		ID:         uuid.New().String(),
		NodeID:     rule.NodeID,
		Type:       models.JobTypeDeploy,
		Status:     models.JobStatusPending,
		Command:    fmt.Sprintf("docker pull %s && docker run -d --name %s %s", image, project.Name, image),
		Payload:    payload,
		MaxRetries: 1,
		CreatedAt:  time.Now(),
	}

	if err := h.db.Create(job).Error; err != nil {
		return fmt.Errorf("failed to create deploy job: %w", err)
	}

	dep := &models.Deployment{
		ID:            uuid.New().String(),
		JobID:         job.ID,
		NodeID:        rule.NodeID,
		Repository:    deployment.Repository,
		Branch:        deployment.Ref,
		Commit:        deployment.SHA,
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
		"deployment_id", deployment.ID,
		"node_id", rule.NodeID,
		"image", image,
	)

	return nil
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

// extractRepoFromToken is best-effort for logging only — does NOT verify the token.
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
