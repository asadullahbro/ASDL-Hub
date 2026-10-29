package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/services"
	"github.com/asdl/hub/internal/testutil"
)

// A refused token gets its reason and signed_out, which the dashboard uses
// to go to the sign-in page instead of polling on.
func TestAuth_SaysWhyATokenWasRefused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewDB(t, &models.User{}, &models.PermanentToken{}, &models.Session{}, &models.Passkey{})
	auth := services.NewAuthService(db, "test-secret")
	settings := services.NewSettingsService(db, auth, "test-secret")
	db.Create(&models.User{ID: "u1", Username: "alice", Role: models.RoleAdmin})
	token, pt, err := settings.GeneratePermanentToken("ci", "u1", services.ScopeAdmin)
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.GET("/me", Auth(auth), func(c *gin.Context) { c.Status(http.StatusOK) })
	call := func(tok string) (int, map[string]interface{}) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/me", nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		r.ServeHTTP(w, req)
		var body map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body
	}

	if code, _ := call(token); code != http.StatusOK {
		t.Fatalf("a good token got %d", code)
	}
	_ = settings.RevokePermanentToken(pt.ID)
	for _, c := range []struct{ token, want string }{
		{token, "token revoked"},
		{"garbage", "invalid token"},
		{"", "missing authorization"},
	} {
		code, body := call(c.token)
		if code != http.StatusUnauthorized || body["error"] != c.want || body["signed_out"] != true {
			t.Errorf("token %.10q: got %d %v, want 401 %q with signed_out", c.token, code, body, c.want)
		}
	}
}

// The dashboard's cookie login works for reads, but a write must come from the
// Hub's own pages: the cookie goes along with requests from any site.
func TestAuth_CookieLoginRefusesCrossSiteWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewDB(t, &models.User{}, &models.PermanentToken{}, &models.Session{}, &models.Passkey{})
	auth := services.NewAuthService(db, "test-secret")
	settings := services.NewSettingsService(db, auth, "test-secret")
	db.Create(&models.User{ID: "u1", Username: "alice", Role: models.RoleAdmin})
	token, _, err := settings.GeneratePermanentToken("ci", "u1", services.ScopeAdmin)
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.Use(Auth(auth))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.POST("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	call := func(method string, cookie bool, hdr map[string]string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, "http://hub.example/x", nil)
		if cookie {
			req.AddCookie(&http.Cookie{Name: SessionCookie, Value: token})
		} else {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		r.ServeHTTP(w, req)
		return w.Code
	}

	if got := call("GET", true, nil); got != http.StatusOK {
		t.Errorf("cookie GET = %d, want 200", got)
	}
	if got := call("POST", true, map[string]string{"Origin": "https://evil.example"}); got != http.StatusForbidden {
		t.Errorf("cookie POST from another site = %d, want 403", got)
	}
	if got := call("POST", true, nil); got != http.StatusForbidden {
		t.Errorf("cookie POST with no Origin or X-Requested-With = %d, want 403", got)
	}
	if got := call("POST", true, map[string]string{"Origin": "http://hub.example"}); got != http.StatusOK {
		t.Errorf("cookie POST from the Hub's own page = %d, want 200", got)
	}
	if got := call("POST", true, map[string]string{"X-Requested-With": "asdl"}); got != http.StatusOK {
		t.Errorf("cookie POST with X-Requested-With = %d, want 200", got)
	}
	if got := call("POST", false, map[string]string{"Origin": "https://evil.example"}); got != http.StatusOK {
		t.Errorf("Bearer POST = %d, want 200 (no cookie, nothing to ride on)", got)
	}
}

// A read-only token owned by an admin reaches what a viewer reaches and is
// refused on operator and admin routes, through the real role guards.
func TestAuth_ScopedTokenIsHeldToItsScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewDB(t, &models.User{}, &models.PermanentToken{}, &models.Session{}, &models.Passkey{})
	auth := services.NewAuthService(db, "test-secret")
	settings := services.NewSettingsService(db, auth, "test-secret")
	db.Create(&models.User{ID: "u1", Username: "alice", Role: models.RoleAdmin})

	r := gin.New()
	r.Use(Auth(auth))
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	r.GET("/apps", ok)
	r.POST("/apps/redeploy", RequireRole(models.RoleAdmin, models.RoleOperator), ok)
	r.GET("/settings/tokens", RequireRole(models.RoleAdmin), ok)

	status := func(scope, method, path string) int {
		tok, _, err := settings.GeneratePermanentToken("t", "u1", scope)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		r.ServeHTTP(w, req)
		return w.Code
	}
	cases := []struct {
		scope, method, path string
		want                int
	}{
		{services.ScopeRead, "GET", "/apps", 200},
		{services.ScopeRead, "POST", "/apps/redeploy", 403},
		{services.ScopeRead, "GET", "/settings/tokens", 403},
		{services.ScopeOperate, "POST", "/apps/redeploy", 200},
		{services.ScopeOperate, "GET", "/settings/tokens", 403},
		{services.ScopeAdmin, "GET", "/settings/tokens", 200},
	}
	for _, c := range cases {
		if got := status(c.scope, c.method, c.path); got != c.want {
			t.Errorf("%s token %s %s = %d, want %d", c.scope, c.method, c.path, got, c.want)
		}
	}
}
