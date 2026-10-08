package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/asdl/hub/internal/models"
)

// refuseIfLocked answers 429 when too many wrong codes or passwords have been
// tried; it reports whether it did.
func (h *AuthHandlers) refuseIfLocked(c *gin.Context, username string) bool {
	if blocked, wait := h.limiter.Blocked(c.ClientIP(), username); blocked {
		secs := int(wait.Seconds()) + 1
		c.Header("Retry-After", strconv.Itoa(secs))
		c.JSON(http.StatusTooManyRequests, gin.H{"error": fmt.Sprintf("too many failed attempts; try again in %d minutes", secs/60+1)})
		return true
	}
	return false
}

// LoginTwoFactor handles POST /auth/2fa/login: the second step of signing in.
func (h *AuthHandlers) LoginTwoFactor(c *gin.Context) {
	var req struct {
		MFAToken string `json:"mfa_token" binding:"required"`
		Code     string `json:"code" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	user, err := h.authService.MFAUser(req.MFAToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	if h.refuseIfLocked(c, user.Username) {
		return
	}
	token, err := h.authService.CompleteTwoFactorLogin(user, req.Code)
	if err != nil {
		h.limiter.Failed(c.ClientIP(), user.Username)
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	h.limiter.Succeeded(user.Username)
	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id": user.ID, "username": user.Username, "email": user.Email, "role": user.Role,
		},
	})
}

func signedInUser(c *gin.Context) (*models.User, bool) {
	v, ok := c.Get("user")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return nil, false
	}
	return v.(*models.User), true
}

// SetupTwoFactor handles POST /auth/2fa/setup: a new secret to scan.
func (h *AuthHandlers) SetupTwoFactor(c *gin.Context) {
	u, ok := signedInUser(c)
	if !ok {
		return
	}
	secret, uri, err := h.authService.BeginTwoFactor(u.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"secret": secret, "uri": uri})
}

// EnableTwoFactor handles POST /auth/2fa/enable: confirms a code, turns it on,
// and returns the recovery codes once.
func (h *AuthHandlers) EnableTwoFactor(c *gin.Context) {
	u, ok := signedInUser(c)
	if !ok {
		return
	}
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.refuseIfLocked(c, u.Username) {
		return
	}
	codes, err := h.authService.EnableTwoFactor(u.ID, req.Code)
	if err != nil {
		h.limiter.Failed(c.ClientIP(), u.Username)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"recovery_codes": codes})
}

// DisableTwoFactor handles POST /auth/2fa/disable.
func (h *AuthHandlers) DisableTwoFactor(c *gin.Context) {
	u, ok := signedInUser(c)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password" binding:"required"`
		Code     string `json:"code" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.refuseIfLocked(c, u.Username) {
		return
	}
	if err := h.authService.DisableTwoFactor(u.ID, req.Password, req.Code); err != nil {
		h.limiter.Failed(c.ClientIP(), u.Username)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "two-factor is off"})
}

// ResetTwoFactor handles DELETE /settings/users/:id/2fa (admins): turns it off
// for someone who lost their phone and recovery codes.
func (h *AuthHandlers) ResetTwoFactor(c *gin.Context) {
	if err := h.authService.ResetTwoFactor(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "two-factor is off for that user"})
}
