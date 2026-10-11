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
	token, pt, err := settings.GeneratePermanentToken("ci", "u1")
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
