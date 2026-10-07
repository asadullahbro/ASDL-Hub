package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDoctor_FindsProblems(t *testing.T) {
	now := time.Now()
	routes := map[string]interface{}{
		"/api/v1/auth/me":        map[string]string{"username": "admin", "role": "admin"},
		"/api/v1/system/version": map[string]interface{}{"current": "v1.0.0", "latest": "v1.1.0", "update_available": true},
		"/api/v1/nodes": []map[string]interface{}{
			{"id": "n1", "hostname": "good", "online": true, "agent_version": "v2026.01.01-aaa", "last_heartbeat": now, "disk_total": 100, "disk_used": 10},
			{"id": "n2", "hostname": "gone", "online": false, "last_heartbeat": now.Add(-48 * time.Hour)},
		},
		"/api/v1/projects": map[string]interface{}{"data": []map[string]interface{}{
			{"id": "p1", "name": "api", "node_id": "n2", "status": "running", "health_status": "unhealthy", "domain": "doctor-test.invalid"},
			{"id": "p2", "name": "web", "node_id": "n1", "status": "failed", "health_status": "unhealthy"},
		}},
		"/api/v1/nodes/n1/connection": map[string]interface{}{"wg_handshake": now.Add(-10 * time.Minute)},
		"/api/v1/jobs": map[string]interface{}{"data": []map[string]interface{}{
			{"id": "j1234567-0000", "node_id": "n1", "type": "deploy", "status": "failed", "created_at": now.Add(-time.Hour)},
		}},
		"/api/v1/notifications": []map[string]interface{}{
			{"id": "c1", "name": "Ops", "type": "discord", "enabled": true, "last_error": "discord.com answered 404: Unknown Webhook"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, ok := routes[r.URL.Path]
		if !ok {
			v = []interface{}{}
		}
		json.NewEncoder(w).Encode(v)
	}))
	defer srv.Close()
	t.Setenv("ASDL_HUB_URL", srv.URL)
	t.Setenv("ASDL_HUB_TOKEN", "t")
	old := latestRelease
	latestRelease = func(string) string { return "v2026.02.02-bbb" }
	defer func() { latestRelease = old }()

	var out bytes.Buffer
	if err := (&env{out: &out}).doctor(); err != errSilent {
		t.Fatalf("doctor returned %v, want problems", err)
	}
	got := out.String()
	for _, want := range []string{
		"v1.1.0 is available", "asdl-hub update install",
		"gone is offline (last seen 2d ago) and still has apps: api",
		"WireGuard handshake 10m ago", "agent v2026.01.01-aaa, v2026.02.02-bbb is out",
		"api is unhealthy", "web has failed",
		"doctor-test.invalid (api): the domain doesn't resolve",
		"1 failed job(s)", "asdl-hub job j1234567",
		"Ops (discord): last send failed: discord.com answered 404",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report lacks %q:\n%s", want, got)
		}
	}
}

func TestTerminalFix(t *testing.T) {
	linux := node{Hostname: "laptop", OS: "linux"}
	mac := node{Hostname: "mbp", OS: "darwin"}
	cases := []struct {
		n       node
		problem string
		want    string
	}{
		{linux, "no SSH server listening (connection refused)", "sudo apt install -y openssh-server"},
		{mac, "no SSH server listening (connection refused)", "Remote Login"},
		{linux, "SSH port doesn't answer (timed out)", "ufw allow"},
		{linux, "no SSH key for this node", "re-run the installer"},
	}
	for _, c := range cases {
		if got := terminalFix(c.n, 22, c.problem); !strings.Contains(got, c.want) || !strings.Contains(got, c.n.Hostname) {
			t.Errorf("%s / %q: %q, want it to mention %q", c.n.OS, c.problem, got, c.want)
		}
	}
}
