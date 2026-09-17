package services

import (
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

func newAuthTestService(t *testing.T) *AuthService {
	db := testutil.NewDB(t, &models.User{}, &models.PermanentToken{})
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

	user, token, err := svc.Login("alice", "correct-horse")
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

	if _, _, err := svc.Login("alice", "wrong-password"); err == nil {
		t.Fatal("expected login with wrong password to fail")
	}
}

func TestLogin_UnknownUser(t *testing.T) {
	svc := newAuthTestService(t)

	if _, _, err := svc.Login("nobody", "whatever"); err == nil {
		t.Fatal("expected login with unknown username to fail")
	}
}

func TestValidateToken_RoundTrip(t *testing.T) {
	svc := newAuthTestService(t)

	_, token, err := svc.Login("alice", "correct-horse")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	user, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("expected token to validate, got error: %v", err)
	}
	if user.ID != "u1" {
		t.Fatalf("expected user u1, got %s", user.ID)
	}
}

func TestValidateToken_RejectsGarbage(t *testing.T) {
	svc := newAuthTestService(t)

	if _, err := svc.ValidateToken("not-a-real-token"); err == nil {
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

	if _, err := svc.ValidateToken(tokenString); err == nil {
		t.Fatal("expected expired token to be rejected")
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

	if _, err := svc.ValidateToken(tokenString); err == nil {
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
		Token:     tokenString,
		TokenHint: "...hint",
		CreatedBy: "u1",
	}
	if err := svc.db.Create(pt).Error; err != nil {
		t.Fatalf("seed permanent token: %v", err)
	}

	user, err := svc.ValidateToken(tokenString)
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

	pt := &models.PermanentToken{ID: "pt2", Name: "ci", Token: tokenString, CreatedBy: "u1"}
	if err := svc.db.Create(pt).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := svc.db.Delete(pt).Error; err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if _, err := svc.ValidateToken(tokenString); err == nil {
		t.Fatal("expected a revoked permanent token to be rejected")
	}
}
