package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// The Hub's real defaults: loopback counts as the mesh (nginx connects from
// there) and only the local nginx may vouch for a client address.
var (
	defaultMesh    = []string{"10.100.0.0/24", "127.0.0.0/8", "::1/128"}
	defaultProxies = []string{"127.0.0.1", "::1"}
)

// A mesh-only route that reports the address it identified the caller as.
func meshRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := TrustOnlyProxies(r, defaultProxies); err != nil {
		t.Fatal(err)
	}
	r.Use(VPNOnly(defaultMesh))
	r.POST("/mesh", func(c *gin.Context) {
		c.String(http.StatusOK, "%s", c.GetString("vpn_ip"))
	})
	return r
}

// peer is the connection's address; headers are what the caller sent along.
func call(r *gin.Engine, peer string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mesh", nil)
	req.RemoteAddr = peer
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMeshRoutesRejectForgedAddresses(t *testing.T) {
	cases := []struct {
		name    string
		peer    string
		headers map[string]string
	}{
		{"forged X-Forwarded-For straight from the internet", "203.0.113.9:4000",
			map[string]string{"X-Forwarded-For": "10.100.0.5"}},
		{"forged X-Real-IP straight from the internet", "203.0.113.9:4000",
			map[string]string{"X-Real-IP": "10.100.0.5"}},
		// What nginx sends when an internet caller forges the header: its own
		// $proxy_add_x_forwarded_for puts the real address on the right.
		{"forged address on the left, real one added by nginx", "127.0.0.1:5000",
			map[string]string{"X-Forwarded-For": "10.100.0.5, 203.0.113.9", "X-Real-IP": "203.0.113.9"}},
		{"forged loopback on the left, real one added by nginx", "127.0.0.1:5000",
			map[string]string{"X-Forwarded-For": "127.0.0.1, 203.0.113.9", "X-Real-IP": "203.0.113.9"}},
		{"plain internet caller through nginx", "127.0.0.1:5000",
			map[string]string{"X-Forwarded-For": "203.0.113.9", "X-Real-IP": "203.0.113.9"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if w := call(meshRouter(t), tc.peer, tc.headers); w.Code != http.StatusForbidden {
				t.Errorf("status = %d (%q), want 403", w.Code, w.Body.String())
			}
		})
	}
}

func TestMeshRoutesIdentifyTheRealCaller(t *testing.T) {
	cases := []struct {
		name    string
		peer    string
		headers map[string]string
		want    string
	}{
		{"a node connecting over WireGuard", "10.100.0.5:4000", nil, "10.100.0.5"},
		// A node must not be able to pass as another node by adding a header.
		{"a node claiming to be another node", "10.100.0.5:4000",
			map[string]string{"X-Forwarded-For": "10.100.0.9", "X-Real-IP": "10.100.0.9"}, "10.100.0.5"},
		{"a mesh request that nginx proxied", "127.0.0.1:5000",
			map[string]string{"X-Forwarded-For": "10.100.0.5", "X-Real-IP": "10.100.0.5"}, "10.100.0.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := call(meshRouter(t), tc.peer, tc.headers)
			if w.Code != http.StatusOK || w.Body.String() != tc.want {
				t.Errorf("got %d %q, want 200 %q", w.Code, w.Body.String(), tc.want)
			}
		})
	}
}

func TestTrustOnlyProxiesRejectsBadAddresses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := TrustOnlyProxies(gin.New(), []string{"not-an-address"}); err == nil {
		t.Error("expected an error for an invalid proxy address")
	}
}
