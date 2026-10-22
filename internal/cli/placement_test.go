package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func moveServer(t *testing.T, options []map[string]interface{}) (posted *map[string]string) {
	t.Helper()
	posted = &map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/projects":
			json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]interface{}{{"id": "p1", "name": "mailcow", "node_id": "n-old"}}})
		case r.URL.Path == "/api/v1/nodes":
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": "n-old", "hostname": "laptop", "online": true},
				{"id": "n-tower", "hostname": "tower", "online": true},
				{"id": "n-pc", "hostname": "pc", "online": true},
			})
		case r.URL.Path == "/api/v1/projects/p1/placement":
			json.NewEncoder(w).Encode(map[string]interface{}{"options": options})
		case r.URL.Path == "/api/v1/migrations" && r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(posted)
			json.NewEncoder(w).Encode(map[string]interface{}{})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ASDL_HUB_URL", srv.URL)
	t.Setenv("ASDL_HUB_TOKEN", "t")
	return posted
}

func TestMove_WithoutANodeUsesTheBestMatch(t *testing.T) {
	up := 99.9
	free := 40
	posted := moveServer(t, []map[string]interface{}{
		{"node_id": "n-tower", "hostname": "tower", "score": 91.2, "uptime_7d": up, "free_memory_percent": free, "free_disk_percent": 20, "headroom": true, "master": true},
		{"node_id": "n-pc", "hostname": "pc", "score": 70, "uptime_7d": nil, "headroom": false, "outages_24h": 2},
	})
	c, err := newClient()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := (&env{out: &out}).move(c, "mailcow", ""); err != nil {
		t.Fatal(err)
	}
	if (*posted)["target_node_id"] != "n-tower" || (*posted)["project_id"] != "p1" {
		t.Fatalf("should have moved mailcow to the best node, posted %v", *posted)
	}
	for _, want := range []string{"tower *", "99.90%", "master", "low on memory or disk", "2 outage(s) today"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestMove_WithoutANodeAndNoneAvailableMovesNothing(t *testing.T) {
	posted := moveServer(t, []map[string]interface{}{})
	c, _ := newClient()
	err := (&env{out: &bytes.Buffer{}}).move(c, "mailcow", "")
	if err == nil || !strings.Contains(err.Error(), "no other node") {
		t.Fatalf("want a 'no other node' error, got %v", err)
	}
	if len(*posted) != 0 {
		t.Fatalf("nothing should have been moved: %v", *posted)
	}
}

func TestMove_ANamedNodeSkipsTheSuggestion(t *testing.T) {
	posted := moveServer(t, nil)
	c, _ := newClient()
	var out bytes.Buffer
	if err := (&env{out: &out}).move(c, "mailcow", "pc"); err != nil {
		t.Fatal(err)
	}
	if (*posted)["target_node_id"] != "n-pc" || strings.Contains(out.String(), "SCORE") {
		t.Fatalf("an explicit node must be used as given, posted %v, output:\n%s", *posted, out.String())
	}
}
