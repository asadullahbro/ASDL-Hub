package services

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/asdl/hub/internal/models"
)

// RFC 6238 appendix B: secret "12345678901234567890", time 59s, 6 digits.
func TestTOTP_MatchesRFC6238(t *testing.T) {
	key := []byte("12345678901234567890")
	if got := hotp(key, uint64(totpStep(time.Unix(59, 0)))); got != "287082" {
		t.Errorf("code at t=59 is %s, want 287082", got)
	}
	if got := hotp(key, uint64(totpStep(time.Unix(1111111109, 0)))); got != "081804" {
		t.Errorf("code at t=1111111109 is %s, want 081804", got)
	}
}

func TestCheckTOTP_WindowAndReplay(t *testing.T) {
	key := []byte("12345678901234567890")
	now := time.Unix(1_700_000_000, 0)
	cur := totpStep(now)
	code := func(step int64) string { return hotp(key, uint64(step)) }

	for _, step := range []int64{cur - 1, cur, cur + 1} {
		if _, ok := checkTOTP(key, code(step), now, 0); !ok {
			t.Errorf("step %+d from now should be accepted", step-cur)
		}
	}
	for _, step := range []int64{cur - 2, cur + 2} {
		if _, ok := checkTOTP(key, code(step), now, 0); ok {
			t.Errorf("step %+d from now should be refused", step-cur)
		}
	}
	// The same code can't be used twice.
	step, ok := checkTOTP(key, code(cur), now, 0)
	if !ok {
		t.Fatal("first use refused")
	}
	if _, ok := checkTOTP(key, code(cur), now, step); ok {
		t.Error("a code already used was accepted again")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := checkTOTP(key, bad, now, 0); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

// setUp2FA turns two-factor on for alice and returns her secret and recovery codes.
func setUp2FA(t *testing.T, svc *AuthService) ([]byte, []string) {
	t.Helper()
	recoveryCost = bcrypt.MinCost
	b32, uri, err := svc.BeginTwoFactor("u1")
	if err != nil || !strings.HasPrefix(uri, "otpauth://totp/ASDL%20Hub:alice?secret="+b32) {
		t.Fatalf("begin: %v %q", err, uri)
	}
	key, err := recoveryEncoding.DecodeString(b32)
	if err != nil {
		t.Fatal(err)
	}
	codes, err := svc.EnableTwoFactor("u1", hotp(key, uint64(totpStep(time.Now()))))
	if err != nil || len(codes) != recoveryCount {
		t.Fatalf("enable: %v, %d codes", err, len(codes))
	}
	return key, codes
}

func TestTwoFactorLogin_NeedsACodeAndTheMFATokenIsNoSession(t *testing.T) {
	svc := newAuthTestService(t)
	key, _ := setUp2FA(t, svc)

	user, token, err := svc.Login("alice", "correct-horse")
	if err != ErrTwoFactorRequired || token != "" || user == nil {
		t.Fatalf("login with 2FA on: user=%v token=%q err=%v", user, token, err)
	}

	mfa, err := svc.MFAToken(user)
	if err != nil {
		t.Fatal(err)
	}
	// The password-step token must not work as a session.
	if _, err := svc.ValidateToken(mfa); err == nil {
		t.Fatal("an mfa token was accepted as a session token")
	}

	u, err := svc.MFAUser(mfa)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteTwoFactorLogin(u, "000000"); err == nil {
		t.Error("a wrong code signed in")
	}
	// The setup consumed this time step; the next one is the next valid code.
	next := hotp(key, uint64(totpStep(time.Now())+1))
	session, err := svc.CompleteTwoFactorLogin(u, next)
	if err != nil {
		t.Fatalf("right code refused: %v", err)
	}
	if _, err := svc.ValidateToken(session); err != nil {
		t.Errorf("the session token from 2FA login is not valid: %v", err)
	}
	// And that code is spent.
	u, _ = svc.MFAUser(mfa)
	if _, err := svc.CompleteTwoFactorLogin(u, next); err == nil {
		t.Error("the same code signed in twice")
	}
}

func TestMFAUser_RejectsOtherTokens(t *testing.T) {
	svc := newAuthTestService(t)
	setUp2FA(t, svc)
	_, session, _ := svc.sessionToken0(t)
	if _, err := svc.MFAUser(session); err == nil {
		t.Error("a normal session token was accepted as an mfa token")
	}
	expired, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": "u1", "type": "mfa", "exp": time.Now().Add(-time.Minute).Unix(),
	}).SignedString([]byte("test-secret"))
	if _, err := svc.MFAUser(expired); err == nil {
		t.Error("an expired mfa token was accepted")
	}
}

// sessionToken0 makes a normal session token for alice.
func (s *AuthService) sessionToken0(t *testing.T) (*models.User, string, error) {
	t.Helper()
	var u models.User
	s.db.First(&u, "id = ?", "u1")
	tok, err := s.sessionToken(&u)
	return &u, tok, err
}

func TestRecoveryCode_WorksOnceAndOnlyOnce(t *testing.T) {
	svc := newAuthTestService(t)
	_, codes := setUp2FA(t, svc)

	var u models.User
	svc.db.First(&u, "id = ?", "u1")
	// Typed in capitals or without the dash is still the same code.
	if _, err := svc.CompleteTwoFactorLogin(&u, strings.ToUpper(strings.ReplaceAll(codes[0], "-", ""))); err != nil {
		t.Fatalf("recovery code refused: %v", err)
	}
	svc.db.First(&u, "id = ?", "u1")
	if _, err := svc.CompleteTwoFactorLogin(&u, codes[0]); err == nil {
		t.Error("a recovery code worked twice")
	}
	if _, err := svc.CompleteTwoFactorLogin(&u, codes[1]); err != nil {
		t.Errorf("another recovery code refused: %v", err)
	}
	if _, err := svc.CompleteTwoFactorLogin(&u, "aaaaa-bbbbb"); err == nil {
		t.Error("a made-up recovery code worked")
	}
}

func TestTwoFactor_SecretIsEncryptedAtRest(t *testing.T) {
	svc := newAuthTestService(t)
	key, _ := setUp2FA(t, svc)
	var u models.User
	svc.db.First(&u, "id = ?", "u1")
	if strings.Contains(u.TOTPSecret, recoveryEncoding.EncodeToString(key)) || u.TOTPSecret == "" {
		t.Error("the secret is stored in the clear (or missing)")
	}
	// Another Hub secret can't open it.
	other := NewAuthService(svc.db, "different-secret")
	if _, err := other.openSecret(u.TOTPSecret); err == nil {
		t.Error("the stored secret opened with the wrong key")
	}
	if got, err := svc.openSecret(u.TOTPSecret); err != nil || string(got) != string(key) {
		t.Errorf("round trip failed: %v", err)
	}
}

func TestDisableTwoFactor_NeedsPasswordAndCode(t *testing.T) {
	svc := newAuthTestService(t)
	key, _ := setUp2FA(t, svc)
	code := hotp(key, uint64(totpStep(time.Now())+1))

	if err := svc.DisableTwoFactor("u1", "wrong", code); err == nil {
		t.Error("turned off with the wrong password")
	}
	if err := svc.DisableTwoFactor("u1", "correct-horse", "000000"); err == nil {
		t.Error("turned off with a wrong code")
	}
	if err := svc.DisableTwoFactor("u1", "correct-horse", code); err != nil {
		t.Fatalf("turn off: %v", err)
	}
	// Back to a plain password login.
	if _, token, err := svc.Login("alice", "correct-horse"); err != nil || token == "" {
		t.Errorf("login after turning off: %v", err)
	}
}

func TestEnableTwoFactor_RefusesAWrongCode(t *testing.T) {
	svc := newAuthTestService(t)
	if _, _, err := svc.BeginTwoFactor("u1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EnableTwoFactor("u1", "000000"); err == nil {
		t.Error("enabled with a wrong code")
	}
	// Setup alone changes nothing about signing in.
	if _, token, err := svc.Login("alice", "correct-horse"); err != nil || token == "" {
		t.Errorf("unconfirmed setup changed login: %v", err)
	}
}

func TestResetTwoFactor_ClearsEverything(t *testing.T) {
	svc := newAuthTestService(t)
	setUp2FA(t, svc)
	if err := svc.ResetTwoFactor("u1"); err != nil {
		t.Fatal(err)
	}
	var u models.User
	svc.db.First(&u, "id = ?", "u1")
	if u.TOTPEnabled || u.TOTPSecret != "" || u.RecoveryCodes != "" {
		t.Errorf("not cleared: %+v", u)
	}
}
