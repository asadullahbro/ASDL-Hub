package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/services"
)

type SettingsHandlers struct {
	settings *services.SettingsService
}

func NewSettingsHandlers(s *services.SettingsService) *SettingsHandlers {
	return &SettingsHandlers{settings: s}
}

func currentUser(c *gin.Context) *models.User {
	u, _ := c.Get("user")
	user, _ := u.(*models.User)
	return user
}

// --- Sudo ---

func (h *SettingsHandlers) VerifyPassword(c *gin.Context) {
	user := currentUser(c)
	var req struct {
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.settings.VerifyAdminPassword(user.ID, req.Password); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"verified": true})
}

// --- Permanent tokens ---

func (h *SettingsHandlers) ListTokens(c *gin.Context) {
	tokens, err := h.settings.ListPermanentTokens()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tokens)
}

func (h *SettingsHandlers) GenerateToken(c *gin.Context) {
	user := currentUser(c)
	var req struct {
		Name     string `json:"name" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.settings.VerifyAdminPassword(user.ID, req.Password); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	tokenString, pt, err := h.settings.GeneratePermanentToken(req.Name, user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Return the raw token ONCE — never stored in plaintext after this
	c.JSON(http.StatusCreated, gin.H{
		"token": tokenString,
		"meta":  pt,
	})
}

func (h *SettingsHandlers) RevokeToken(c *gin.Context) {
	id := c.Param("id")
	if err := h.settings.RevokePermanentToken(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": true})
}

// --- GitHub token ---

func (h *SettingsHandlers) GetGitHubToken(c *gin.Context) {
	masked := h.settings.GetGitHubTokenMasked()
	c.JSON(http.StatusOK, gin.H{"token": masked, "set": masked != ""})
}

func (h *SettingsHandlers) SetGitHubToken(c *gin.Context) {
	user := currentUser(c)
	var req struct {
		Token    string `json:"token" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.settings.VerifyAdminPassword(user.ID, req.Password); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	if err := h.settings.SetGitHubToken(req.Token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"set": true})
}

// --- Master node ---

func (h *SettingsHandlers) GetMasterNode(c *gin.Context) {
	node, err := h.settings.GetMasterNode()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if node == nil {
		c.JSON(http.StatusOK, gin.H{"master_node": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"master_node": node})
}

func (h *SettingsHandlers) SetMasterNode(c *gin.Context) {
	var req struct {
		NodeID string `json:"node_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.settings.SetMasterNode(req.NodeID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"set": true})
}

func (h *SettingsHandlers) ClearMasterNode(c *gin.Context) {
	if err := h.settings.ClearMasterNode(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"cleared": true})
}

// --- Users ---

func (h *SettingsHandlers) ListUsers(c *gin.Context) {
	users, err := h.settings.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, users)
}

func (h *SettingsHandlers) CreateUser(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Email    string `json:"email" binding:"required"`
		Password string `json:"password" binding:"required"`
		Role     string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	user, err := h.settings.CreateUser(req.Username, req.Email, req.Password, req.Role)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, user)
}

func (h *SettingsHandlers) ChangePassword(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.settings.ChangePassword(id, req.Password); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": true})
}

func (h *SettingsHandlers) ChangeRole(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Role string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.settings.ChangeRole(id, req.Role); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": true})
}

func (h *SettingsHandlers) DeleteUser(c *gin.Context) {
	requester := currentUser(c)
	id := c.Param("id")
	if err := h.settings.DeleteUser(id, requester.ID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}