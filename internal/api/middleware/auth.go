// internal/middleware/auth.go
package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/services"
)

// ClientOf is the address and browser a request comes from.
func ClientOf(c *gin.Context) services.Client {
	return services.Client{IP: c.ClientIP(), UserAgent: c.Request.UserAgent()}
}

func Auth(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := ""

		// Try Authorization header first
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenString = strings.TrimPrefix(authHeader, "Bearer ")
		}

		// Fall back to query param for WebSocket connections
		if tokenString == "" {
			tokenString = c.Query("token")
		}

		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization", "signed_out": true})
			c.Abort()
			return
		}

		user, err := authService.ValidateToken(tokenString, ClientOf(c))
		if err != nil {
			// Say why (revoked, expired, ended...), and mark the answer so
			// the dashboard knows to send the user to sign in rather than
			// keep asking with a token that won't work again.
			msg := services.ErrTokenInvalid.Error()
			for _, known := range services.TokenErrors {
				if errors.Is(err, known) {
					msg = known.Error()
				}
			}
			c.JSON(http.StatusUnauthorized, gin.H{"error": msg, "signed_out": true})
			c.Abort()
			return
		}

		c.Set("user", user)
		c.Set("user_id", user.ID)
		c.Set("session_id", authService.SessionID(tokenString))
		c.Next()
	}
}

func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, exists := c.Get("user")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}

		u := user.(*models.User)
		for _, role := range roles {
			if u.Role == role {
				c.Next()
				return
			}
		}

		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		c.Abort()
	}
}
