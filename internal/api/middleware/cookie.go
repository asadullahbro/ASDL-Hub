package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// SessionCookie holds the dashboard's login token. It is HttpOnly, so page
// scripts can't read it, and SameSite=Lax, so other sites can't send it with a
// form post; Auth also checks the Origin of cookie-authenticated writes.
const SessionCookie = "asdl_session"

const (
	sessionCookieLife  = 24 * time.Hour      // matches a normal session
	rememberCookieLife = 30 * 24 * time.Hour // matches "remember me"
)

func secureRequest(c *gin.Context) bool {
	return c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
}

func writeSessionCookie(c *gin.Context, token string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secureRequest(c),
		SameSite: http.SameSiteLaxMode,
	})
}

// SetSessionCookie signs the browser in: token lasts as long as its session.
func SetSessionCookie(c *gin.Context, token string, remember bool) {
	life := sessionCookieLife
	if remember {
		life = rememberCookieLife
	}
	writeSessionCookie(c, token, int(life.Seconds()))
}

// ClearSessionCookie signs the browser out.
func ClearSessionCookie(c *gin.Context) { writeSessionCookie(c, "", -1) }

// TokenFrom is the login token a request carries: a Bearer header (the CLI,
// scripts), else the session cookie (the dashboard, including its WebSockets).
func TokenFrom(c *gin.Context) (token string, viaCookie bool) {
	if h := c.GetHeader("Authorization"); len(h) > 7 && h[:7] == "Bearer " {
		return h[7:], false
	}
	if v, err := c.Cookie(SessionCookie); err == nil && v != "" {
		return v, true
	}
	return "", false
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}
