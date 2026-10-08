package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/asdl/hub/internal/services"
)

// CLIStart handles POST /auth/cli/start (no sign-in): the command line asks
// for a login request. The user then approves its code in the dashboard.
func (h *AuthHandlers) CLIStart(c *gin.Context) {
	var req struct {
		Machine string `json:"machine"`
	}
	_ = c.ShouldBindJSON(&req)
	code, secret, err := h.CLI.Start(req.Machine, c.ClientIP())
	if err != nil {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":        services.FormatCLICode(code),
		"poll_secret": secret,
		"expires_in":  300,
		"path":        "/authorize?code=" + services.FormatCLICode(code),
	})
}

// CLIPoll handles POST /auth/cli/poll (no sign-in): 202 while waiting, 200
// with the token once approved, 410 when denied or expired.
func (h *AuthHandlers) CLIPoll(c *gin.Context) {
	var req struct {
		Code   string `json:"code" binding:"required"`
		Secret string `json:"poll_secret" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.refuseIfLocked(c, "cli:"+req.Code) {
		return
	}
	status, token, user := h.CLI.Poll(req.Code, req.Secret)
	switch status {
	case "approved":
		c.JSON(http.StatusOK, gin.H{
			"status": status,
			"token":  token,
			"user":   gin.H{"id": user.ID, "username": user.Username, "email": user.Email, "role": user.Role},
		})
	case "pending":
		c.JSON(http.StatusAccepted, gin.H{"status": status})
	case "denied", "expired":
		c.JSON(http.StatusGone, gin.H{"status": status})
	default:
		h.limiter.Failed(c.ClientIP(), "cli:"+req.Code)
		c.JSON(http.StatusNotFound, gin.H{"status": "unknown"})
	}
}

// CLIRequest handles GET /auth/cli/request?code=: what the authorise page
// shows about the request, for a signed-in user.
func (h *AuthHandlers) CLIRequest(c *gin.Context) {
	u, ok := signedInUser(c)
	if !ok {
		return
	}
	if h.refuseIfLocked(c, u.Username) {
		return
	}
	info, found := h.CLI.Info(c.Query("code"))
	if !found {
		h.limiter.Failed(c.ClientIP(), u.Username)
		c.JSON(http.StatusNotFound, gin.H{"error": "that login request has expired or doesn't exist; run asdl-hub login again"})
		return
	}
	c.JSON(http.StatusOK, info)
}

func (h *AuthHandlers) cliAnswer(c *gin.Context, approve bool) {
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
	var err error
	if approve {
		err = h.CLI.Approve(req.Code, u)
	} else {
		err = h.CLI.Deny(req.Code)
	}
	if err != nil {
		h.limiter.Failed(c.ClientIP(), u.Username)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "done"})
}

// CLIApprove handles POST /auth/cli/approve.
func (h *AuthHandlers) CLIApprove(c *gin.Context) { h.cliAnswer(c, true) }

// CLIDeny handles POST /auth/cli/deny.
func (h *AuthHandlers) CLIDeny(c *gin.Context) { h.cliAnswer(c, false) }
