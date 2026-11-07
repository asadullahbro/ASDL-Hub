package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Three apps to one node: all three moves reach the Hub before the CLI waits on any, so the node can run them
// side by side; one failing is reported and makes the command fail.
func TestMoveMany_StartsAllThenWaits(t *testing.T) {
	var mu sync.Mutex
	var posted []string
	polled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/api/v1/projects":
			json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]interface{}{
				{"id": "p1", "name": "ai-chat", "node_id": "n-tower"},
				{"id": "p2", "name": "gamer-ak-bot", "node_id": "n-tower"},
				{"id": "p3", "name": "profile-cards", "node_id": "n-tower"},
			}})
		case r.URL.Path == "/api/v1/nodes":
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": "n-tower", "hostname": "tower", "online": true},
				{"id": "n-laptop", "hostname": "laptop", "online": true, "parallel_jobs": 3, "capabilities": []string{"parallel_jobs"}},
			})
		case r.URL.Path == "/api/v1/migrations" && r.Method == http.MethodPost:
			if polled {
				t.Error("a move was started after the CLI began waiting")
			}
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			posted = append(posted, body["project_id"]+"->"+body["target_node_id"])
			json.NewEncoder(w).Encode(map[string]interface{}{"start_job_id": "job-" + body["project_id"]})
		case strings.HasPrefix(r.URL.Path, "/api/v1/jobs/"):
			polled = true
			status := "completed"
			if strings.HasSuffix(r.URL.Path, "job-p2") {
				status = "failed"
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"status": status})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ASDL_HUB_URL", srv.URL)
	t.Setenv("ASDL_HUB_TOKEN", "t")

	c, _ := newClient()
	apps, to := splitMoveArgs(c, []string{"ai-chat", "gamer-ak-bot", "profile-cards", "laptop"})
	if to != "laptop" || len(apps) != 3 {
		t.Fatalf("split into %v → %q", apps, to)
	}
	var out bytes.Buffer
	err := (&env{out: &out}).moveMany(c, apps, to)
	if err == nil || !strings.Contains(err.Error(), "1 of 3") {
		t.Errorf("err = %v, want 1 of 3 failed", err)
	}
	if len(posted) != 3 {
		t.Errorf("posted %v, want 3 moves", posted)
	}
	o := out.String()
	for _, want := range []string{"Moving 3 app(s) to laptop, up to 3 at a time", "✓ ai-chat moved to laptop", "✗ gamer-ak-bot did not move", "✓ profile-cards moved to laptop"} {
		if !strings.Contains(o, want) {
			t.Errorf("output lacks %q:\n%s", want, o)
		}
	}
}

func TestSplitMoveArgs_WithoutANode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{{"id": "n1", "hostname": "laptop"}})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ASDL_HUB_URL", srv.URL)
	t.Setenv("ASDL_HUB_TOKEN", "t")
	c, _ := newClient()
	if apps, to := splitMoveArgs(c, []string{"ai-chat", "modscore"}); to != "" || len(apps) != 2 {
		t.Errorf("no node named: got %v → %q", apps, to)
	}
}
