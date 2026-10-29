package services

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

func mustHash(t *testing.T, password string) string {
	t.Helper()
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	return string(hashed)
}

// testOpts is a sign-in from a desktop Firefox.
var testOpts = SessionOptions{Client: Client{
	IP:        "198.51.100.20",
	UserAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0",
}}

// cliClient is the command line asking.
var cliClient = Client{IP: "198.51.100.8", UserAgent: "asdl-hub-cli"}

func newAuthTestService(t *testing.T) *AuthService {
	db := testutil.NewDB(t, &models.User{}, &models.PermanentToken{}, &models.Session{}, &models.Passkey{})
	user := &models.User{
		ID:       "u1",
		Username: "alice",
		Email:    "alice@example.com",
		Password: mustHash(t, "correct-horse"),
		Role:     models.RoleAdmin,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return NewAuthService(db, "test-secret")
}

func TestLogin_Success(t *testing.T) {
	svc := newAuthTestService(t)

	user, token, err := svc.Login("alice", "correct-horse", testOpts)
	if err != nil {
		t.Fatalf("expected login to succeed, got error: %v", err)
	}
	if user.Username != "alice" {
		t.Fatalf("expected user alice, got %s", user.Username)
	}
	if token == "" {
		t.Fatal("expected a non-empty token")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	svc := newAuthTestService(t)

	if _, _, err := svc.Login("alice", "wrong-password", testOpts); err == nil {
		t.Fatal("expected login with wrong password to fail")
	}
}

func TestLogin_UnknownUser(t *testing.T) {
	svc := newAuthTestService(t)

	if _, _, err := svc.Login("nobody", "whatever", testOpts); err == nil {
		t.Fatal("expected login with unknown username to fail")
	}
}

func TestValidateToken_RoundTrip(t *testing.T) {
	svc := newAuthTestService(t)

	_, token, err := svc.Login("alice", "correct-horse", testOpts)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	user, err := svc.ValidateToken(token, testOpts.Client)
	if err != nil {
		t.Fatalf("expected token to validate, got error: %v", err)
	}
	if user.ID != "u1" {
		t.Fatalf("expected user u1, got %s", user.ID)
	}
}

func TestValidateToken_RejectsGarbage(t *testing.T) {
	svc := newAuthTestService(t)

	if _, err := svc.ValidateToken("not-a-real-token", testOpts.Client); err == nil {
		t.Fatal("expected garbage token to be rejected")
	}
}

func TestValidateToken_RejectsExpiredToken(t *testing.T) {
	svc := newAuthTestService(t)

	expired := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": "u1",
		"role":    models.RoleAdmin,
		"exp":     time.Now().Add(-1 * time.Hour).Unix(),
	})
	tokenString, err := expired.SignedString([]byte(svc.GetJWTSecret()))
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}

	if _, err := svc.ValidateToken(tokenString, testOpts.Client); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("an expired token: got %v, want %v", err, ErrTokenExpired)
	}
}

func TestValidateToken_RejectsTokenSignedWithWrongSecret(t *testing.T) {
	svc := newAuthTestService(t)

	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": "u1",
		"role":    models.RoleAdmin,
		"exp":     time.Now().Add(1 * time.Hour).Unix(),
	})
	tokenString, err := forged.SignedString([]byte("some-other-secret"))
	if err != nil {
		t.Fatalf("sign forged token: %v", err)
	}

	if _, err := svc.ValidateToken(tokenString, testOpts.Client); err == nil {
		t.Fatal("expected token signed with the wrong secret to be rejected")
	}
}

func newPermanentToken(t *testing.T, secret string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"token_id": "tok1",
		"type":     "permanent",
		"exp":      time.Now().Add(10 * 365 * 24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign permanent token: %v", err)
	}
	return tokenString
}

func TestValidateToken_PermanentTokenResolvesToCreator(t *testing.T) {
	svc := newAuthTestService(t)
	tokenString := newPermanentToken(t, svc.GetJWTSecret())

	pt := &models.PermanentToken{
		ID:        "pt1",
		Name:      "ci",
		Token:     hashToken(tokenString),
		TokenHint: "...hint",
		CreatedBy: "u1",
	}
	if err := svc.db.Create(pt).Error; err != nil {
		t.Fatalf("seed permanent token: %v", err)
	}

	user, err := svc.ValidateToken(tokenString, testOpts.Client)
	if err != nil {
		t.Fatalf("expected permanent token to validate, got error: %v", err)
	}
	if user.ID != "u1" {
		t.Fatalf("expected permanent token to resolve to creator u1, got %s", user.ID)
	}
}

func TestValidateToken_RevokedPermanentTokenRejected(t *testing.T) {
	svc := newAuthTestService(t)
	tokenString := newPermanentToken(t, svc.GetJWTSecret())

	pt := &models.PermanentToken{ID: "pt2", Name: "ci", Token: hashToken(tokenString), CreatedBy: "u1"}
	if err := svc.db.Create(pt).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := svc.db.Delete(pt).Error; err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if _, err := svc.ValidateToken(tokenString, testOpts.Client); !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("a revoked permanent token: got %v, want %v", err, ErrTokenRevoked)
	}
}

func TestValidateToken_RejectsOtherSigningMethods(t *testing.T) {
	svc := newAuthTestService(t)
	for _, method := range []jwt.SigningMethod{jwt.SigningMethodHS384, jwt.SigningMethodHS512} {
		tok, err := jwt.NewWithClaims(method, jwt.MapClaims{"user_id": "u1", "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte("test-secret"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ValidateToken(tok, testOpts.Client); err == nil {
			t.Errorf("a token signed with %s was accepted", method.Alg())
		}
	}
	// The one the Hub issues still works.
	_, tok, _ := svc.Login("alice", "correct-horse", testOpts)
	if _, err := svc.ValidateToken(tok, testOpts.Client); err != nil {
		t.Errorf("the Hub's own token was refused: %v", err)
	}
}

func TestPermanentTokens_StoredOnlyAsAHash(t *testing.T) {
	svc := newAuthTestService(t)
	settings := NewSettingsService(svc.db, svc, svc.GetJWTSecret())

	token, pt, err := settings.GeneratePermanentToken("ci", "u1", ScopeAdmin)
	if err != nil {
		t.Fatal(err)
	}
	var stored models.PermanentToken
	svc.db.First(&stored, "id = ?", pt.ID)
	if stored.Token == token || strings.Contains(stored.Token, ".") {
		t.Fatalf("the token itself is in the database: %q", stored.Token)
	}
	if _, err := svc.ValidateToken(token, testOpts.Client); err != nil {
		t.Fatalf("a new permanent token was refused: %v", err)
	}
}

func TestPermanentTokens_OldPlainOnesAreHashedAndKeepWorking(t *testing.T) {
	svc := newAuthTestService(t)
	settings := NewSettingsService(svc.db, svc, svc.GetJWTSecret())
	tokenString := newPermanentToken(t, svc.GetJWTSecret())
	// As versions before 0.15.1 stored them; one revoked, which must be hashed too.
	svc.db.Create(&models.PermanentToken{ID: "old", Name: "ci", Token: tokenString, CreatedBy: "u1"})
	revoked := &models.PermanentToken{ID: "gone", Name: "x", Token: "a.b.c", CreatedBy: "u1"}
	svc.db.Create(revoked)
	svc.db.Delete(revoked)

	settings.HashStoredTokens()
	settings.HashStoredTokens() // running again on every start changes nothing

	var rows []models.PermanentToken
	svc.db.Unscoped().Find(&rows)
	for _, r := range rows {
		if strings.Contains(r.Token, ".") {
			t.Errorf("token %s is still stored as-is", r.ID)
		}
	}
	if _, err := svc.ValidateToken(tokenString, testOpts.Client); err != nil {
		t.Fatalf("an existing permanent token stopped working: %v", err)
	}
}

func TestCapRole(t *testing.T) {
	cases := []struct{ role, scope, want string }{
		{models.RoleAdmin, ScopeRead, models.RoleViewer},
		{models.RoleAdmin, ScopeOperate, models.RoleOperator},
		{models.RoleAdmin, ScopeAdmin, models.RoleAdmin},
		{models.RoleOperator, ScopeRead, models.RoleViewer},
		{models.RoleOperator, ScopeAdmin, models.RoleOperator}, // a scope never raises a role
		{models.RoleViewer, ScopeOperate, models.RoleViewer},
		{models.RoleAdmin, "", models.RoleAdmin}, // tokens from before scopes
	}
	for _, c := range cases {
		if got := CapRole(c.role, c.scope); got != c.want {
			t.Errorf("CapRole(%s, %q) = %s, want %s", c.role, c.scope, got, c.want)
		}
	}
}

func TestPermanentToken_ScopeLowersTheOwnersRole(t *testing.T) {
	svc := newAuthTestService(t)
	settings := NewSettingsService(svc.db, svc, svc.GetJWTSecret())
	want := map[string]string{ScopeRead: models.RoleViewer, ScopeOperate: models.RoleOperator, ScopeAdmin: models.RoleAdmin}
	for scope, role := range want {
		tok, pt, err := settings.GeneratePermanentToken("t-"+scope, "u1", scope)
		if err != nil {
			t.Fatal(err)
		}
		if pt.Scope != scope {
			t.Errorf("stored scope = %q, want %q", pt.Scope, scope)
		}
		user, err := svc.ValidateToken(tok, testOpts.Client)
		if err != nil {
			t.Fatal(err)
		}
		if user.Role != role {
			t.Errorf("scope %s: role %s, want %s", scope, user.Role, role)
		}
	}
	var owner models.User
	svc.db.First(&owner, "id = ?", "u1")
	if owner.Role != models.RoleAdmin {
		t.Errorf("the owner's own role changed to %s", owner.Role)
	}
}

func TestPermanentToken_TokenFromBeforeScopesKeepsFullAccess(t *testing.T) {
	svc := newAuthTestService(t)
	tok := newPermanentToken(t, svc.GetJWTSecret())
	svc.db.Create(&models.PermanentToken{ID: "old", Name: "old", Token: hashToken(tok), CreatedBy: "u1"})
	user, err := svc.ValidateToken(tok, testOpts.Client)
	if err != nil || user.Role != models.RoleAdmin {
		t.Fatalf("old token: role %v, err %v; want admin", user, err)
	}
}

func TestGeneratePermanentToken_RejectsUnknownScope(t *testing.T) {
	svc := newAuthTestService(t)
	settings := NewSettingsService(svc.db, svc, svc.GetJWTSecret())
	for _, scope := range []string{"", "root", "Read"} {
		if _, _, err := settings.GeneratePermanentToken("x", "u1", scope); err == nil {
			t.Errorf("scope %q was accepted", scope)
		}
	}
	var n int64
	svc.db.Model(&models.PermanentToken{}).Count(&n)
	if n != 0 {
		t.Errorf("%d token(s) stored for refused scopes", n)
	}
}
