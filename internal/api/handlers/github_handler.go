package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	githubclient "github.com/asdl/hub/internal/github"
	"github.com/asdl/hub/internal/models"
)

type GitHubHandlers struct {
	db  *gorm.DB
	app *githubclient.AppClient
}

func NewGitHubHandlers(db *gorm.DB) *GitHubHandlers {
	return &GitHubHandlers{
		db:  db,
		app: githubclient.NewAppClient(db),
	}
}

// --- App configuration (admin only) ---

// ConfigureApp stores GitHub App credentials in the settings table.
// POST /api/v1/github/app
func (h *GitHubHandlers) ConfigureApp(c *gin.Context) {
	var req struct {
		AppID         string `json:"app_id" binding:"required"`
		PrivateKey    string `json:"private_key" binding:"required"` // raw PEM
		WebhookSecret string `json:"webhook_secret" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	settings := []models.Setting{
		{Key: "github_app_id", Value: req.AppID, UpdatedAt: time.Now()},
		{Key: "github_app_private_key", Value: req.PrivateKey, UpdatedAt: time.Now()},
		{Key: "github_webhook_secret", Value: req.WebhookSecret, UpdatedAt: time.Now()},
	}

	for _, s := range settings {
		if err := h.db.Save(&s).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save %s: %v", s.Key, err)})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"configured": true})
}

// GetAppConfig returns whether the GitHub App is configured (never returns credentials).
// GET /api/v1/github/app
func (h *GitHubHandlers) GetAppConfig(c *gin.Context) {
	var appID models.Setting
	configured := h.db.First(&appID, "key = ?", "github_app_id").Error == nil && appID.Value != ""
	c.JSON(http.StatusOK, gin.H{
		"configured": configured,
		"app_id":     appID.Value, // safe — not a secret
	})
}

// --- Installations ---

// RegisterInstallation is called after the user installs the GitHub App.
// POST /api/v1/github/installations
func (h *GitHubHandlers) RegisterInstallation(c *gin.Context) {
	var req struct {
		InstallationID int64  `json:"installation_id" binding:"required"`
		AccountLogin   string `json:"account_login" binding:"required"`
		AccountType    string `json:"account_type" binding:"required"` // "User" or "Organization"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify we can actually get a token for this installation
	// before storing it — catches bad installation IDs early
	if _, err := h.app.InstallationToken(req.InstallationID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("could not verify installation: %v", err)})
		return
	}

	installation := models.GitHubInstallation{
		ID:             uuid.New().String(),
		InstallationID: req.InstallationID,
		AccountLogin:   req.AccountLogin,
		AccountType:    req.AccountType,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Upsert — if installation already exists, update it
	var existing models.GitHubInstallation
	if h.db.First(&existing, "installation_id = ?", req.InstallationID).Error == nil {
		h.db.Model(&existing).Updates(map[string]interface{}{
			"account_login": req.AccountLogin,
			"account_type":  req.AccountType,
			"updated_at":    time.Now(),
		})
		c.JSON(http.StatusOK, existing)
		return
	}

	if err := h.db.Create(&installation).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, installation)
}

// ListInstallations returns all registered GitHub App installations.
// GET /api/v1/github/installations
func (h *GitHubHandlers) ListInstallations(c *gin.Context) {
	var installations []models.GitHubInstallation
	if err := h.db.Find(&installations).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, installations)
}

// --- Repositories ---

// ListRepos returns repositories accessible to an installation from GitHub.
// GET /api/v1/github/installations/:installation_id/repos
func (h *GitHubHandlers) ListRepos(c *gin.Context) {
	var installation models.GitHubInstallation
	if err := h.db.First(&installation, "installation_id = ?", c.Param("installation_id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "installation not found"})
		return
	}

	repos, err := h.app.ListRepositories(installation.InstallationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"repositories": repos})
}

// LinkRepo connects a GitHub repository to an ASDL Hub project.
// POST /api/v1/github/repos
func (h *GitHubHandlers) LinkRepo(c *gin.Context) {
	var req struct {
		InstallationID int64  `json:"installation_id" binding:"required"`
		RepoID         int64  `json:"repo_id" binding:"required"`
		Owner          string `json:"owner" binding:"required"`
		Name           string `json:"name" binding:"required"`
		DefaultBranch  string `json:"default_branch"`
		ProjectID      string `json:"project_id"` // optional — can link later
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify installation exists
	var installation models.GitHubInstallation
	if err := h.db.First(&installation, "installation_id = ?", req.InstallationID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "installation not found"})
		return
	}

	// Verify project exists if provided
	if req.ProjectID != "" {
		var project models.Project
		if err := h.db.First(&project, "id = ?", req.ProjectID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
	}

	// Verify repo is accessible to this installation
	repo, err := h.app.GetRepository(req.InstallationID, req.Owner, req.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("could not verify repository access: %v", err)})
		return
	}

	defaultBranch := req.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = repo.DefaultBranch
	}

	ghRepo := models.GitHubRepository{
		ID:             uuid.New().String(),
		InstallationID: req.InstallationID,
		RepoID:         repo.ID,
		Owner:          repo.Owner,
		Name:           repo.Name,
		FullName:       repo.FullName,
		DefaultBranch:  defaultBranch,
		ProjectID:      req.ProjectID,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Upsert by RepoID
	var existing models.GitHubRepository
	if h.db.First(&existing, "repo_id = ?", repo.ID).Error == nil {
		h.db.Model(&existing).Updates(map[string]interface{}{
			"installation_id": req.InstallationID,
			"project_id":      req.ProjectID,
			"default_branch":  defaultBranch,
			"updated_at":      time.Now(),
		})
		c.JSON(http.StatusOK, existing)
		return
	}

	if err := h.db.Create(&ghRepo).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, ghRepo)
}

// GetLinkedRepos returns all GitHub repositories linked to a project.
// GET /api/v1/github/repos?project_id=...
func (h *GitHubHandlers) GetLinkedRepos(c *gin.Context) {
	projectID := c.Query("project_id")

	var repos []models.GitHubRepository
	query := h.db
	if projectID != "" {
		query = query.Where("project_id = ?", projectID)
	}
	if err := query.Find(&repos).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, repos)
}

// --- Webhook ---

// Webhook receives GitHub webhook events.
// POST /api/v1/github/webhook  (public — no JWT auth)
func (h *GitHubHandlers) Webhook(c *gin.Context) {
	// Read body first — needed for signature verification
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	// Verify signature
	if err := h.verifyWebhookSignature(body, c.GetHeader("X-Hub-Signature-256")); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook signature"})
		return
	}

	eventType := c.GetHeader("X-GitHub-Event")
	deliveryID := c.GetHeader("X-GitHub-Delivery")

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}

	switch eventType {
	case "push":
		h.handlePushEvent(payload, deliveryID)
	case "installation":
		h.handleInstallationEvent(payload)
	case "ping":
		// GitHub sends ping on webhook creation — just acknowledge
	default:
		// Ignore unsupported events
	}

	c.JSON(http.StatusOK, gin.H{"received": true})
}

func (h *GitHubHandlers) verifyWebhookSignature(body []byte, signature string) error {
	secret, err := h.app.WebhookSecret()
	if err != nil {
		return err
	}

	expected := "sha256=" + computeHMAC(body, secret)
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func computeHMAC(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *GitHubHandlers) handlePushEvent(payload map[string]interface{}, deliveryID string) {
	// Extract repo ID from payload
	repo, ok := payload["repository"].(map[string]interface{})
	if !ok {
		return
	}
	repoID, ok := repo["id"].(float64)
	if !ok {
		return
	}

	// Find linked ASDL repo
	var ghRepo models.GitHubRepository
	if err := h.db.First(&ghRepo, "repo_id = ?", int64(repoID)).Error; err != nil {
		return // repo not linked — ignore
	}

	if ghRepo.ProjectID == "" {
		return // linked repo but no project yet — ignore
	}

	// Idempotency — skip if we already processed this delivery
	var existing models.GitHubWebhookEvent
	if h.db.First(&existing, "delivery_id = ?", deliveryID).Error == nil {
		return
	}

	// Record the event
	event := models.GitHubWebhookEvent{
		ID:         uuid.New().String(),
		DeliveryID: deliveryID,
		Event:      "push",
		RepoID:     int64(repoID),
		ProjectID:  ghRepo.ProjectID,
		Processed:  false,
		CreatedAt:  time.Now(),
	}
	h.db.Create(&event)

	// TODO: trigger deployment — wired up in next step
}

func (h *GitHubHandlers) handleInstallationEvent(payload map[string]interface{}) {
	action, _ := payload["action"].(string)
	if action != "deleted" {
		return
	}

	installation, ok := payload["installation"].(map[string]interface{})
	if !ok {
		return
	}
	installationID, ok := installation["id"].(float64)
	if !ok {
		return
	}

	// Soft delete the installation and its repos
	h.db.Where("installation_id = ?", int64(installationID)).Delete(&models.GitHubInstallation{})
	h.db.Where("installation_id = ?", int64(installationID)).Delete(&models.GitHubRepository{})
}
