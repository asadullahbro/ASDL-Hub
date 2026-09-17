package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func newVPNTestRouter(allowed []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(VPNOnly(allowed))
	r.GET("/mesh", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return r
}

func doRequest(r *gin.Engine, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/mesh", nil)
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestVPNOnly_AllowsIPInsideCIDR(t *testing.T) {
	r := newVPNTestRouter([]string{"10.100.0.0/24"})
	w := doRequest(r, "10.100.0.5:5555")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for in-range IP, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVPNOnly_BlocksIPOutsideCIDR(t *testing.T) {
	r := newVPNTestRouter([]string{"10.100.0.0/24"})
	w := doRequest(r, "203.0.113.9:5555")
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for out-of-range IP, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVPNOnly_AllowsLoopback(t *testing.T) {
	r := newVPNTestRouter([]string{"127.0.0.0/8", "::1/128"})
	w := doRequest(r, "127.0.0.1:5555")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for loopback, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVPNOnly_BlocksEverythingWhenNoNetworksConfigured(t *testing.T) {
	r := newVPNTestRouter(nil)
	w := doRequest(r, "10.100.0.5:5555")
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with no allowed networks, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVPNOnly_SkipsMalformedCIDRRatherThanAllowingAll(t *testing.T) {
	r := newVPNTestRouter([]string{"not-a-cidr", "10.100.0.0/24"})
	// Still enforces the valid entry...
	if w := doRequest(r, "10.100.0.5:5555"); w.Code != http.StatusOK {
		t.Fatalf("expected 200 for in-range IP, got %d", w.Code)
	}
	// ...and the malformed one grants no access.
	if w := doRequest(r, "8.8.8.8:5555"); w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for out-of-range IP, got %d", w.Code)
	}
}
