package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/asdl/hub/internal/api/middleware"
	"github.com/asdl/hub/internal/services"
)

func currentSessionID(c *gin.Context) string {
	sid, _ := c.Get("session_id")
	s, _ := sid.(string)
	return s
}

// Logout handles POST /auth/logout: ends the session the request came with.
func (h *AuthHandlers) Logout(c *gin.Context) {
	u, ok := signedInUser(c)
	if !ok {
		return
	}
	if sid := currentSessionID(c); sid != "" {
		_ = h.authService.RevokeSession(u.ID, sid)
	}
	c.JSON(http.StatusOK, gin.H{"message": "signed out"})
}

// ListSessions handles GET /auth/sessions: where you are signed in.
func (h *AuthHandlers) ListSessions(c *gin.Context) {
	u, ok := signedInUser(c)
	if !ok {
		return
	}
	sessions, err := h.authService.ListSessions(u.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	current := currentSessionID(c)
	out := make([]gin.H, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, gin.H{
			"id": s.ID, "device": s.Device, "method": s.Method, "remember": s.Remember,
			"ip": s.IP, "last_ip": s.LastIP, "created_at": s.CreatedAt, "last_used_at": s.LastUsedAt,
			"expires_at": s.ExpiresAt, "current": s.ID == current,
		})
	}
	c.JSON(http.StatusOK, out)
}

// RevokeSession handles DELETE /auth/sessions/:id: signs that device out.
func (h *AuthHandlers) RevokeSession(c *gin.Context) {
	u, ok := signedInUser(c)
	if !ok {
		return
	}
	if err := h.authService.RevokeSession(u.ID, c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "signed out"})
}

// RevokeOtherSessions handles POST /auth/sessions/revoke-others: signs out
// everywhere but here.
func (h *AuthHandlers) RevokeOtherSessions(c *gin.Context) {
	u, ok := signedInUser(c)
	if !ok {
		return
	}
	n, err := h.authService.RevokeOtherSessions(u.ID, currentSessionID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"signed_out": n})
}

// passkeyLimitKey stands in for the username in the login limiter: passkey
// sign-in has none, and one shared key would let anyone lock everyone out.
func passkeyLimitKey(c *gin.Context) string { return "passkey@" + c.ClientIP() }

// PasskeyLoginBegin handles POST /auth/passkey/login/begin (no sign-in).
func (h *AuthHandlers) PasskeyLoginBegin(c *gin.Context) {
	if h.refuseIfLocked(c, passkeyLimitKey(c)) {
		return
	}
	options, ceremony, err := h.authService.BeginPasskeyLogin(c.GetHeader("Origin"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ceremony": ceremony, "options": options})
}

// PasskeyLoginFinish handles POST /auth/passkey/login/finish (no sign-in).
func (h *AuthHandlers) PasskeyLoginFinish(c *gin.Context) {
	var req struct {
		Ceremony   string          `json:"ceremony" binding:"required"`
		Credential json.RawMessage `json:"credential" binding:"required"`
		Remember   bool            `json:"remember"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.refuseIfLocked(c, passkeyLimitKey(c)) {
		return
	}
	user, token, err := h.authService.FinishPasskeyLogin(req.Ceremony, req.Credential,
		services.SessionOptions{Client: middleware.ClientOf(c), Remember: req.Remember})
	if err != nil {
		h.limiter.Failed(c.ClientIP(), passkeyLimitKey(c))
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id": user.ID, "username": user.Username, "email": user.Email, "role": user.Role,
		},
	})
}

// ListPasskeys handles GET /auth/passkeys.
func (h *AuthHandlers) ListPasskeys(c *gin.Context) {
	u, ok := signedInUser(c)
	if !ok {
		return
	}
	keys, err := h.authService.ListPasskeys(u.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, keys)
}

// PasskeyRegisterBegin handles POST /auth/passkeys/register/begin.
func (h *AuthHandlers) PasskeyRegisterBegin(c *gin.Context) {
	u, ok := signedInUser(c)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password" binding:"required"`
		Code     string `json:"code"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.refuseIfLocked(c, u.Username) {
		return
	}
	options, ceremony, err := h.authService.BeginPasskeyRegistration(u.ID, req.Password, req.Code, c.GetHeader("Origin"))
	if err != nil {
		h.limiter.Failed(c.ClientIP(), u.Username)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ceremony": ceremony, "options": options})
}

// PasskeyRegisterFinish handles POST /auth/passkeys/register/finish.
func (h *AuthHandlers) PasskeyRegisterFinish(c *gin.Context) {
	u, ok := signedInUser(c)
	if !ok {
		return
	}
	var req struct {
		Ceremony   string          `json:"ceremony" binding:"required"`
		Name       string          `json:"name"`
		Credential json.RawMessage `json:"credential" binding:"required"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	pk, err := h.authService.FinishPasskeyRegistration(u.ID, req.Ceremony, req.Name, req.Credential)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, pk)
}

// DeletePasskey handles DELETE /auth/passkeys/:id.
func (h *AuthHandlers) DeletePasskey(c *gin.Context) {
	u, ok := signedInUser(c)
	if !ok {
		return
	}
	if err := h.authService.DeletePasskey(u.ID, c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "passkey removed"})
}
