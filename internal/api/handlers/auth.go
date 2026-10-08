package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/asdl/hub/internal/api/middleware"
	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/services"
)

type AuthHandlers struct {
	authService *services.AuthService
	limiter     *middleware.LoginLimiter
}

func NewAuthHandlers(authService *services.AuthService) *AuthHandlers {
	return &AuthHandlers{authService: authService, limiter: middleware.NewLoginLimiter()}
}

func (h *AuthHandlers) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Too many wrong passwords from this address, or for this account: refuse
	// without even checking the password.
	ip := c.ClientIP()
	if blocked, wait := h.limiter.Blocked(ip, req.Username); blocked {
		secs := int(wait.Seconds()) + 1
		c.Header("Retry-After", strconv.Itoa(secs))
		c.JSON(http.StatusTooManyRequests, gin.H{"error": fmt.Sprintf("too many failed logins; try again in %d minutes", secs/60+1)})
		return
	}

	user, token, err := h.authService.Login(req.Username, req.Password)
	if errors.Is(err, services.ErrTwoFactorRequired) {
		// Right password; a code is still needed. This counts neither as a
		// failure nor as a success for the limiter.
		mfa, err := h.authService.MFAToken(user)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start two-factor sign-in"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"mfa_required": true, "mfa_token": mfa})
		return
	}
	if err != nil {
		h.limiter.Failed(ip, req.Username)
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	h.limiter.Succeeded(req.Username)

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"email":    user.Email,
			"role":     user.Role,
		},
	})
}

func (h *AuthHandlers) Me(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	u := user.(*models.User)
	c.JSON(http.StatusOK, gin.H{
		"id":           u.ID,
		"username":     u.Username,
		"email":        u.Email,
		"role":         u.Role,
		"created_at":   u.CreatedAt,
		"totp_enabled": u.TOTPEnabled,
	})
}

func (h *AuthHandlers) GeneratePermanentToken(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	u := user.(*models.User)
	if u.Role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "only admins can generate permanent tokens"})
		return
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": u.ID,
		"role":    u.Role,
		"type":    "permanent",
		"exp":     time.Now().Add(365 * 24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString([]byte(h.authService.GetJWTSecret()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":   tokenString,
		"expires": time.Now().Add(365 * 24 * time.Hour),
	})
}
