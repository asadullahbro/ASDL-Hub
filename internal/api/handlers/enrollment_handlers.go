package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/services"
)

type EnrollmentHandlers struct {
	enrollment *services.EnrollmentService
	wg         *services.WireGuardService
}

func NewEnrollmentHandlers(e *services.EnrollmentService, wg *services.WireGuardService) *EnrollmentHandlers {
	return &EnrollmentHandlers{enrollment: e, wg: wg}
}

func (h *EnrollmentHandlers) CreateToken(c *gin.Context) {
	user := currentUser(c)
	var req struct {
		Label string `json:"label" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	token, err := h.enrollment.CreateToken(req.Label, user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, token)
}

func (h *EnrollmentHandlers) ListTokens(c *gin.Context) {
	tokens, err := h.enrollment.ListTokens()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tokens)
}

func (h *EnrollmentHandlers) RevokeToken(c *gin.Context) {
	id := c.Param("id")
	if err := h.enrollment.RevokeToken(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": true})
}

func (h *EnrollmentHandlers) Enroll(c *gin.Context) {
	var req services.EnrollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.enrollment.Enroll(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, resp)
}

func (h *EnrollmentHandlers) WireGuardStatus(c *gin.Context) {
	status, err := h.wg.Status()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"output": status})
}

func (h *EnrollmentHandlers) RemovePeer(c *gin.Context) {
	nodeID := c.Param("id")

	var peer models.WireGuardPeer
	if err := h.wg.DB().First(&peer, "node_id = ?", nodeID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "peer not found"})
		return
	}

	if err := h.wg.RemovePeer(peer.PublicKey); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.wg.DB().Delete(&peer)
	c.JSON(http.StatusOK, gin.H{"removed": true})
}
