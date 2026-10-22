package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func statusPageServer(t *testing.T, public *bool) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/settings/status-page" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPut {
			var body struct {
				Public bool `json:"public"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			*public = body.Public
		}
		json.NewEncoder(w).Encode(map[string]bool{"public": *public})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ASDL_HUB_URL", srv.URL)
	t.Setenv("ASDL_HUB_TOKEN", "t")
}

func TestStatusPage_ShowsAndSwitches(t *testing.T) {
	public := false
	statusPageServer(t, &public)
	c, err := newClient()
	if err != nil {
		t.Fatal(err)
	}
	run := func(arg string) string {
		var out bytes.Buffer
		if err := (&env{out: &out}).statusPage(c, arg); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if got := run(""); !strings.Contains(got, "private") || !strings.Contains(got, "/status/") {
		t.Fatalf("should say it is private and where the page is: %s", got)
	}
	if got := run("on"); !public || !strings.Contains(got, "public") || !strings.Contains(got, "only apps that have a domain") {
		t.Fatalf("on: public=%v output %s", public, got)
	}
	if got := run("off"); public || !strings.Contains(got, "private") {
		t.Fatalf("off: public=%v output %s", public, got)
	}
	if err := (&env{out: &bytes.Buffer{}}).statusPage(c, "maybe"); err == nil {
		t.Fatal("an unknown setting must be refused")
	}
}
