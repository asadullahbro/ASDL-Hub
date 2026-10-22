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

func rollbackServer(t *testing.T, previous interface{}) (posted *bool) {
	t.Helper()
	posted = new(bool)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/projects":
			json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]interface{}{{"id": "p1", "name": "bot", "node_id": "n1"}}})
		case r.URL.Path == "/api/v1/projects/p1/rollback" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]interface{}{"current_image": "ghcr.io/o/bot:v2", "previous": previous})
		case r.URL.Path == "/api/v1/projects/p1/rollback" && r.Method == http.MethodPost:
			*posted = true
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

func TestRollback_ShowsBothVersionsAndRollsBack(t *testing.T) {
	posted := rollbackServer(t, map[string]interface{}{
		"image": "ghcr.io/o/bot:v1", "deployed_at": time.Now().Add(-5 * time.Hour), "commit_msg": "fix the epic command",
	})
	c, err := newClient()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := (&env{out: &out}).rollback(c, "bot"); err != nil {
		t.Fatal(err)
	}
	if !*posted {
		t.Fatal("should have asked the Hub to roll back")
	}
	for _, want := range []string{"ghcr.io/o/bot:v2", "ghcr.io/o/bot:v1", "5h ago", "fix the epic command"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestRollback_WithNothingEarlierChangesNothing(t *testing.T) {
	posted := rollbackServer(t, nil)
	c, _ := newClient()
	err := (&env{out: &bytes.Buffer{}}).rollback(c, "bot")
	if err == nil || !strings.Contains(err.Error(), "nothing to roll back to") {
		t.Fatalf("want a 'nothing to roll back to' error, got %v", err)
	}
	if *posted {
		t.Fatal("nothing should have been rolled back")
	}
}
