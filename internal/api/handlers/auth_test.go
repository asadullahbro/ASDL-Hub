package handlers

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/services"
	"github.com/asdl/hub/internal/testutil"
)

func loginRouter(t *testing.T) *gin.Engine {
	t.Helper()
	r, _ := loginRouterWithService(t)
	return r
}

func loginRouterWithService(t *testing.T) (*gin.Engine, *services.AuthService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := testutil.NewDB(t, &models.User{}, &models.PermanentToken{})
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct-horse"), bcrypt.MinCost)
	db.Create(&models.User{ID: "u1", Username: "alice", Email: "a@example.com", Password: string(hash), Role: models.RoleAdmin})
	svc := services.NewAuthService(db, "secret")
	h := NewAuthHandlers(svc)
	r := gin.New()
	r.POST("/login", h.Login)
	r.POST("/2fa/login", h.LoginTwoFactor)
	return r, svc
}

func login(r *gin.Engine, from, user, pass string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"`+user+`","password":"`+pass+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = from + ":5000"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestLogin_StopsGuessingAndKeepsRealUsersOut_OfTheWay(t *testing.T) {
	r := loginRouter(t)
	attacker := "203.0.113.9"

	for i := 0; i < 8; i++ {
		if w := login(r, attacker, "alice", "guess"); w.Code != http.StatusUnauthorized {
			t.Fatalf("guess %d: status %d, want 401", i, w.Code)
		}
	}
	// Past the limit even the right password is refused from that address.
	w := login(r, attacker, "alice", "correct-horse")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("after 8 failures: status %d, Retry-After %q; want 429 with a wait", w.Code, w.Header().Get("Retry-After"))
	}
	// Someone else is not affected, and a normal login works.
	if w := login(r, "198.51.100.4", "alice", "correct-horse"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "token") {
		t.Fatalf("a different address: status %d: %s", w.Code, w.Body.String())
	}
}

func TestLogin_UnknownUserAndWrongPasswordLookTheSame(t *testing.T) {
	r := loginRouter(t)
	a := login(r, "203.0.113.1", "alice", "wrong")
	b := login(r, "203.0.113.2", "nobody", "wrong")
	if a.Code != b.Code || a.Body.String() != b.Body.String() {
		t.Errorf("responses differ: %d %q vs %d %q", a.Code, a.Body.String(), b.Code, b.Body.String())
	}
}

// An independent RFC 6238 implementation, so the test doesn't just agree with
// the code under test.
func appCode(t *testing.T, b32 string, at time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(b32)
	if err != nil {
		t.Fatal(err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(at.Unix()/30))
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	o := sum[19] & 0xf
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[o:o+4])&0x7fffffff)%1000000)
}

func post(r *gin.Engine, path, from, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = from + ":5000"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestLogin_WithTwoFactor(t *testing.T) {
	r, svc := loginRouterWithService(t)
	secret, _, err := svc.BeginTwoFactor("u1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EnableTwoFactor("u1", appCode(t, secret, time.Now())); err != nil {
		t.Fatal(err)
	}

	// The password alone gets no session, only a token for the second step.
	w := login(r, "198.51.100.4", "alice", "correct-horse")
	var step1 struct {
		Token       string `json:"token"`
		MFARequired bool   `json:"mfa_required"`
		MFAToken    string `json:"mfa_token"`
	}
	json.Unmarshal(w.Body.Bytes(), &step1)
	if w.Code != http.StatusOK || !step1.MFARequired || step1.MFAToken == "" || step1.Token != "" {
		t.Fatalf("step 1: %d %s", w.Code, w.Body.String())
	}

	// A wrong code is refused; the app's next code signs in.
	body := func(code string) string { return `{"mfa_token":"` + step1.MFAToken + `","code":"` + code + `"}` }
	if w := post(r, "/2fa/login", "198.51.100.4", body("000000")); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code: %d", w.Code)
	}
	w = post(r, "/2fa/login", "198.51.100.4", body(appCode(t, secret, time.Now().Add(30*time.Second))))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("right code: %d %s", w.Code, w.Body.String())
	}

	// Guessing codes is limited like guessing passwords.
	for i := 0; i < 8; i++ {
		post(r, "/2fa/login", "203.0.113.50", body("000000"))
	}
	if w := post(r, "/2fa/login", "203.0.113.50", body(appCode(t, secret, time.Now().Add(-30*time.Second)))); w.Code != http.StatusTooManyRequests {
		t.Errorf("after many wrong codes: %d, want 429", w.Code)
	}
}
