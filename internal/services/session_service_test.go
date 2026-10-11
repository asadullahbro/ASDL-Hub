package services

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/asdl/hub/internal/models"
)

func TestDeviceName(t *testing.T) {
	for ua, want := range map[string]string{
		"Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0":                                                                 "Firefox on Linux",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36":                        "Chrome on Windows",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0":          "Edge on Windows",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15":                  "Safari on macOS",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/141.0 Mobile/15E148 Safari/604.1": "Chrome on iOS",
		"Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36":                  "Chrome on Android",
		"asdl-hub-cli": "asdl-hub command line",
		"curl/8.5.0":   "Unknown browser on unknown OS",
	} {
		if got := deviceName(ua); got != want {
			t.Errorf("deviceName(%q) = %q, want %q", ua, got, want)
		}
	}
	// Updating the browser keeps the same device.
	a := deviceName("Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0")
	b := deviceName("Mozilla/5.0 (X11; Linux x86_64; rv:141.0) Gecko/20100101 Firefox/141.0")
	if a != b {
		t.Errorf("a browser update changed the device: %q vs %q", a, b)
	}
}

func expiryOf(t *testing.T, token string) time.Time {
	t.Helper()
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(token, claims); err != nil {
		t.Fatal(err)
	}
	exp, _ := claims.GetExpirationTime()
	return exp.Time
}

func TestLogin_RememberLastsThirtyDays(t *testing.T) {
	svc := newAuthTestService(t)
	_, short, _ := svc.Login("alice", "correct-horse", testOpts)
	opts := testOpts
	opts.Remember = true
	_, long, _ := svc.Login("alice", "correct-horse", opts)

	if d := time.Until(expiryOf(t, short)); d < 23*time.Hour || d > 25*time.Hour {
		t.Errorf("normal sign-in lasts %v, want a day", d)
	}
	if d := time.Until(expiryOf(t, long)); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Errorf("remembered sign-in lasts %v, want 30 days", d)
	}
	sessions, _ := svc.ListSessions("u1")
	if len(sessions) != 2 {
		t.Fatalf("%d sessions recorded, want 2", len(sessions))
	}
	for _, s := range sessions {
		if s.Device != "Firefox on Linux" || s.IP != "198.51.100.20" || s.Method != "password" {
			t.Errorf("session recorded as %+v", s)
		}
	}
}

func TestSession_SignOutEndsTheToken(t *testing.T) {
	svc := newAuthTestService(t)
	_, token, _ := svc.Login("alice", "correct-horse", testOpts)
	_, other, _ := svc.Login("alice", "correct-horse", testOpts)

	if err := svc.RevokeSession("u1", svc.SessionID(token)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateToken(token, testOpts.Client); err == nil {
		t.Error("a signed-out token still works")
	}
	if _, err := svc.ValidateToken(other, testOpts.Client); err != nil {
		t.Errorf("signing one device out ended another: %v", err)
	}
	// Someone else can't sign alice's devices out.
	if err := svc.RevokeSession("u2", svc.SessionID(other)); err == nil {
		t.Error("another user revoked alice's session")
	}

	if n, _ := svc.RevokeOtherSessions("u1", svc.SessionID(other)); n != 0 {
		t.Errorf("revoke-others removed %d, want 0 (only the current one is left)", n)
	}
	_, third, _ := svc.Login("alice", "correct-horse", testOpts)
	svc.RevokeOtherSessions("u1", svc.SessionID(other))
	if _, err := svc.ValidateToken(third, testOpts.Client); err == nil {
		t.Error("revoke-others left another session working")
	}
	if _, err := svc.ValidateToken(other, testOpts.Client); err != nil {
		t.Errorf("revoke-others ended the current session: %v", err)
	}
}

func TestSession_TiedToTheBrowser(t *testing.T) {
	svc := newAuthTestService(t)
	_, token, _ := svc.Login("alice", "correct-horse", testOpts)

	// A new address is fine (phones and laptops move), and is recorded.
	moved := testOpts.Client
	moved.IP = "203.0.113.50"
	if _, err := svc.ValidateToken(token, moved); err != nil {
		t.Fatalf("a new address signed the user out: %v", err)
	}
	var s models.Session
	svc.db.First(&s, "id = ?", svc.SessionID(token))
	if s.LastIP != "203.0.113.50" || s.IP != "198.51.100.20" {
		t.Errorf("addresses recorded as sign-in %q, last %q", s.IP, s.LastIP)
	}

	// The token copied to another browser is refused, and ended for good.
	thief := Client{IP: "203.0.113.66", UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"}
	if _, err := svc.ValidateToken(token, thief); !errors.Is(err, ErrOtherDevice) {
		t.Fatalf("a token from a different browser: got %v, want %v", err, ErrOtherDevice)
	}
	if _, err := svc.ValidateToken(token, testOpts.Client); !errors.Is(err, ErrSignedOut) {
		t.Errorf("the session after being used from another browser: got %v, want %v", err, ErrSignedOut)
	}
}

func TestPasswordChange_SignsOutEverywhereElse(t *testing.T) {
	svc := newAuthTestService(t)
	settings := NewSettingsService(svc.db, svc, "test-secret")
	_, here, _ := svc.Login("alice", "correct-horse", testOpts)
	_, elsewhere, _ := svc.Login("alice", "correct-horse", testOpts)

	if err := settings.ChangePassword("u1", "new-password-123", svc.SessionID(here)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateToken(elsewhere, testOpts.Client); err == nil {
		t.Error("another device stayed signed in after a password change")
	}
	if _, err := svc.ValidateToken(here, testOpts.Client); err != nil {
		t.Errorf("the device that changed the password was signed out: %v", err)
	}
}

func TestValidateToken_OldTokensWithoutASession(t *testing.T) {
	svc := newAuthTestService(t)
	sign := func(claims jwt.MapClaims) string {
		tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret"))
		return tok
	}
	// A day-long token from before sessions keeps working until it runs out.
	legacy := sign(jwt.MapClaims{"user_id": "u1", "role": "admin", "exp": time.Now().Add(20 * time.Hour).Unix()})
	if _, err := svc.ValidateToken(legacy, testOpts.Client); err != nil {
		t.Errorf("a day-long token from before the upgrade was refused: %v", err)
	}
	// A long-lived one with no session to revoke is not accepted.
	long := sign(jwt.MapClaims{"user_id": "u1", "role": "admin", "exp": time.Now().Add(30 * 24 * time.Hour).Unix()})
	if _, err := svc.ValidateToken(long, testOpts.Client); err == nil {
		t.Error("a 30-day token without a session was accepted")
	}
	// A session ID that doesn't exist is refused.
	forged := sign(jwt.MapClaims{"user_id": "u1", "sid": "nope", "exp": time.Now().Add(time.Hour).Unix()})
	if _, err := svc.ValidateToken(forged, testOpts.Client); err == nil {
		t.Error("a token naming no session was accepted")
	}
	// The command line's local token is still fine.
	local := sign(jwt.MapClaims{"user_id": "u1", "role": "admin", "type": "local", "exp": time.Now().Add(7 * 24 * time.Hour).Unix()})
	if _, err := svc.ValidateToken(local, cliClient); err != nil {
		t.Errorf("the local command-line token was refused: %v", err)
	}
}
