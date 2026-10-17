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

func TestDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		45 * time.Second:              "45s",
		158 * time.Second:             "2m 38s",
		4*time.Hour + 5*time.Minute:   "4h 5m",
		51*time.Hour + 20*time.Minute: "2d 3h",
	} {
		if got := duration(d); got != want {
			t.Errorf("duration(%v) = %q, want %q", d, got, want)
		}
	}
}

func nodesServer(t *testing.T, uptime interface{}) {
	t.Helper()
	now := time.Now()
	routes := map[string]interface{}{
		"/api/v1/nodes": []map[string]interface{}{
			{"id": "n1", "hostname": "laptop", "online": true, "last_heartbeat": now},
			{"id": "n2", "hostname": "pc", "online": false, "last_heartbeat": now.Add(-48 * time.Hour)},
			{"id": "n3", "hostname": "fresh", "online": true, "last_heartbeat": now},
		},
	}
	if uptime != nil {
		routes["/api/v1/nodes/uptime"] = uptime
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(v)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ASDL_HUB_URL", srv.URL)
	t.Setenv("ASDL_HUB_TOKEN", "t")
}

func TestNodes_ShowsUptimeAndLastOutage(t *testing.T) {
	now := time.Now()
	up99, up0 := 99.37, 0.0
	nodesServer(t, map[string]interface{}{"nodes": []map[string]interface{}{
		{"node_id": "n1", "uptime_7d": up99, "last_outage": map[string]interface{}{"started_at": now.Add(-5 * time.Hour), "ended_at": now.Add(-3 * time.Hour), "seconds": 7200}},
		{"node_id": "n2", "uptime_7d": up0, "last_outage": map[string]interface{}{"started_at": now.Add(-48 * time.Hour), "ended_at": nil, "seconds": 0}},
		{"node_id": "n3", "uptime_7d": nil, "last_outage": nil},
	}})
	c, err := newClient()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := (&env{out: &out}).nodes(c); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"UPTIME 7D", "LAST OUTAGE", "99.37%", "2h 0m, 3h ago", "0.00%", "ongoing, 2d 0h"} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
}

func TestNodes_StillWorksAgainstAnOlderHub(t *testing.T) {
	nodesServer(t, nil) // no /nodes/uptime: 404
	c, err := newClient()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := (&env{out: &out}).nodes(c); err != nil {
		t.Fatalf("an older Hub without the endpoint must not break the command: %v", err)
	}
	if !strings.Contains(out.String(), "laptop") || !strings.Contains(out.String(), "-") {
		t.Fatalf("nodes should still list, with dashes for uptime:\n%s", out.String())
	}
}
