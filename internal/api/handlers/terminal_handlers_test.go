package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/services"
	"github.com/asdl/hub/internal/testutil"
)

const terminalTestSecret = "terminal-test-secret"

// A real signed token for a user, the way Login makes one
func tokenFor(t *testing.T, userID string) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(terminalTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

// Calls the terminal handler the way the browser does (token in the query)
// and returns the HTTP status. These are plain requests, not WebSocket
// upgrades, so a request that passes the role check ends in the upgrader's
// 400 instead of opening a shell: a node is never reached.
func terminalStatus(t *testing.T, token string) int {
	t.Helper()
	db := testutil.NewDB(t, &models.User{}, &models.PermanentToken{})
	for _, u := range []models.User{
		{ID: "u-admin", Username: "admin", Email: "admin@example.com", Password: "x", Role: models.RoleAdmin},
		{ID: "u-operator", Username: "operator", Email: "operator@example.com", Password: "x", Role: models.RoleOperator},
		{ID: "u-viewer", Username: "viewer", Email: "viewer@example.com", Password: "x", Role: models.RoleViewer},
	} {
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
	}

	gin.SetMode(gin.TestMode)
	h := NewTerminalHandlers(nil, services.NewAuthService(db, terminalTestSecret))
	router := gin.New()
	router.GET("/nodes/:id/terminal", h.Terminal)

	url := "/nodes/node-1/terminal"
	if token != "" {
		url += "?token=" + token
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec.Code
}

func TestTerminalNeedsAdminRole(t *testing.T) {
	cases := []struct {
		name  string
		token func(t *testing.T) string
		want  func(code int) bool
		desc  string
	}{
		{"no token", func(t *testing.T) string { return "" }, func(c int) bool { return c == http.StatusUnauthorized }, "401"},
		{"garbage token", func(t *testing.T) string { return "not-a-token" }, func(c int) bool { return c == http.StatusUnauthorized }, "401"},
		{"token for a user that does not exist", func(t *testing.T) string { return tokenFor(t, "ghost") }, func(c int) bool { return c == http.StatusUnauthorized }, "401"},
		{"viewer", func(t *testing.T) string { return tokenFor(t, "u-viewer") }, func(c int) bool { return c == http.StatusForbidden }, "403"},
		{"operator", func(t *testing.T) string { return tokenFor(t, "u-operator") }, func(c int) bool { return c == http.StatusForbidden }, "403"},
		// Passes the role check; the plain request then fails the WebSocket upgrade
		{"admin", func(t *testing.T) string { return tokenFor(t, "u-admin") }, func(c int) bool { return c != http.StatusUnauthorized && c != http.StatusForbidden }, "not 401/403"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code := terminalStatus(t, tc.token(t)); !tc.want(code) {
				t.Errorf("status = %d, want %s", code, tc.desc)
			}
		})
	}
}

func TestCanOpenTerminal(t *testing.T) {
	cases := map[string]struct {
		user *models.User
		want bool
	}{
		"admin":    {&models.User{Role: models.RoleAdmin}, true},
		"operator": {&models.User{Role: models.RoleOperator}, false},
		"viewer":   {&models.User{Role: models.RoleViewer}, false},
		"no role":  {&models.User{}, false},
		"nil user": {nil, false},
	}
	for name, tc := range cases {
		if got := canOpenTerminal(tc.user); got != tc.want {
			t.Errorf("%s: canOpenTerminal = %v, want %v", name, got, tc.want)
		}
	}
}
