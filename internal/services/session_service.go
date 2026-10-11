package services

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/asdl/hub/internal/models"
)

// Every sign-in is a row in sessions, and its token carries the row's ID
// ("sid"). A token whose row is gone no longer works, so signing out, or
// signing a lost laptop out from Settings, really ends it.
//
// A session is also tied to the browser and OS that signed in. A token copied
// to a different browser is refused and its session ended, since the owner's
// browser never changes family by itself. The address is recorded but not
// enforced: home, phone and VPN addresses change all day.

const (
	sessionLife           = 24 * time.Hour
	rememberedSessionLife = 30 * 24 * time.Hour
	// How often last-used time and address are written, at most.
	sessionTouchEvery = 5 * time.Minute
)

// Client is who is asking: the address and User-Agent of a request.
type Client struct {
	IP        string
	UserAgent string
}

// SessionOptions describe a sign-in.
type SessionOptions struct {
	Client   Client
	Remember bool   // 30 days instead of 24 hours
	Method   string // password, passkey or cli
}

var errSignedOut = errors.New("signed out")

// deviceName is the browser and OS a User-Agent names, without versions, so
// browser updates don't change it.
func deviceName(ua string) string {
	if strings.HasPrefix(ua, "asdl-hub-cli") {
		return "asdl-hub command line"
	}
	browser := "Unknown browser"
	for _, b := range []struct{ marker, name string }{
		{"Edg", "Edge"}, // Edg/, EdgA/, EdgiOS/
		{"OPR/", "Opera"},
		{"SamsungBrowser/", "Samsung Internet"},
		{"Firefox/", "Firefox"},
		{"FxiOS/", "Firefox"},
		{"CriOS/", "Chrome"},
		{"Chrome/", "Chrome"},
		{"Safari/", "Safari"},
	} {
		if strings.Contains(ua, b.marker) {
			browser = b.name
			break
		}
	}
	os := "unknown OS"
	for _, o := range []struct{ marker, name string }{
		{"iPhone", "iOS"},
		{"iPad", "iPadOS"},
		{"Android", "Android"},
		{"CrOS", "ChromeOS"},
		{"Windows", "Windows"},
		{"Macintosh", "macOS"},
		{"Linux", "Linux"},
	} {
		if strings.Contains(ua, o.marker) {
			os = o.name
			break
		}
	}
	return browser + " on " + os
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// issueSession records a sign-in and returns its token.
func (s *AuthService) issueSession(user *models.User, opts SessionOptions) (string, error) {
	now := time.Now()
	life := sessionLife
	if opts.Remember {
		life = rememberedSessionLife
	}
	sid := uuid.New().String()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"role":    user.Role,
		"sid":     sid,
		"exp":     now.Add(life).Unix(),
	}).SignedString([]byte(s.jwtSecret))
	if err != nil {
		return "", err
	}
	ua := opts.Client.UserAgent
	if len(ua) > 255 {
		ua = ua[:255]
	}
	sess := models.Session{
		ID:         sid,
		UserID:     user.ID,
		Token:      hashToken(token),
		IP:         opts.Client.IP,
		UserAgent:  ua,
		Device:     deviceName(opts.Client.UserAgent),
		Method:     opts.Method,
		Remember:   opts.Remember,
		LastIP:     opts.Client.IP,
		LastUsedAt: now,
		ExpiresAt:  now.Add(life),
		CreatedAt:  now,
	}
	if err := s.db.Create(&sess).Error; err != nil {
		return "", err
	}
	// Tidy up: sessions that have run out.
	s.db.Where("expires_at < ?", now).Delete(&models.Session{})
	return token, nil
}

// checkSession is the part of ValidateToken for tokens that name a session.
func (s *AuthService) checkSession(sid, userID string, client Client) error {
	var sess models.Session
	if err := s.db.First(&sess, "id = ?", sid).Error; err != nil {
		return errSignedOut
	}
	now := time.Now()
	if sess.UserID != userID || now.After(sess.ExpiresAt) {
		return errSignedOut
	}
	if device := deviceName(client.UserAgent); device != sess.Device {
		// The token left the browser it was given to. End the session so it
		// doesn't work anywhere; the owner just signs in again.
		log.Printf("🔒 Session %s for user %s was used from %q at %s but belongs to %q; signing it out",
			sid, userID, device, client.IP, sess.Device)
		s.db.Delete(&models.Session{}, "id = ?", sid)
		return errSignedOut
	}
	if now.Sub(sess.LastUsedAt) > sessionTouchEvery || sess.LastIP != client.IP {
		s.db.Model(&models.Session{}).Where("id = ?", sid).
			Updates(map[string]interface{}{"last_used_at": now, "last_ip": client.IP})
	}
	return nil
}

// SessionID is the session a token names, or "" for tokens that aren't
// sessions (API tokens). The token must already have been validated.
func (s *AuthService) SessionID(tokenString string) string {
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(tokenString, claims); err != nil {
		return ""
	}
	sid, _ := claims["sid"].(string)
	return sid
}

// ListSessions is a user's signed-in devices, most recently used first.
func (s *AuthService) ListSessions(userID string) ([]models.Session, error) {
	var out []models.Session
	err := s.db.Where("user_id = ? AND expires_at > ?", userID, time.Now()).
		Order("last_used_at DESC").Find(&out).Error
	return out, err
}

// RevokeSession signs one of a user's devices out.
func (s *AuthService) RevokeSession(userID, sid string) error {
	res := s.db.Where("id = ? AND user_id = ?", sid, userID).Delete(&models.Session{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("no such session")
	}
	return nil
}

// RevokeOtherSessions signs a user out everywhere except keepSID ("" keeps
// none).
func (s *AuthService) RevokeOtherSessions(userID, keepSID string) (int64, error) {
	res := s.db.Where("user_id = ? AND id <> ?", userID, keepSID).Delete(&models.Session{})
	return res.RowsAffected, res.Error
}
