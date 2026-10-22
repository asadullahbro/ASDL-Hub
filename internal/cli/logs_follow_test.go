package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestLogPrinter_StripsHeaderAndFooterAndJoinsSplitLines(t *testing.T) {
	var out bytes.Buffer
	p := newLogPrinter(&out)
	p.write("Job 123 started\nNode: tower\n" + jobBanner + "\n\n2026-10-22T10:00:00Z first li")
	p.write("ne\n2026-10-22T10:00:01Z second line\n")
	p.write("\n" + jobBanner + "\nCompleted at: now\n")
	p.flush()
	want := "2026-10-22T10:00:00Z first line\n2026-10-22T10:00:01Z second line\n\n"
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

func TestLogPrinter_WithoutAHeaderPrintsWhatItGets(t *testing.T) {
	var out bytes.Buffer
	p := newLogPrinter(&out)
	for _, c := range []string{"a\n", "b\n", "c\n", "d\n"} {
		p.write(c)
	}
	p.flush()
	if out.String() != "a\nb\nc\nd\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestLogPrinter_FlushPrintsAnUnfinishedLastLine(t *testing.T) {
	var out bytes.Buffer
	p := newLogPrinter(&out)
	p.write("only one line, no newline")
	p.flush()
	if out.String() != "only one line, no newline\n" {
		t.Fatalf("got %q", out.String())
	}
}

func followServer(t *testing.T, status string) (query *string) {
	t.Helper()
	query = new(string)
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/projects":
			json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]interface{}{{"id": "p1", "name": "bot", "node_id": "n1"}}})
		case r.URL.Path == "/api/v1/nodes/n1/containers/bot/logs" && r.Method == http.MethodPost:
			*query = r.URL.RawQuery
			json.NewEncoder(w).Encode(map[string]string{"job_id": "j1"})
		case r.URL.Path == "/api/v1/jobs/j1/stream":
			if r.Header.Get("Authorization") != "Bearer t" {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			ws, err := up.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer ws.Close()
			ws.WriteJSON(map[string]string{"type": "snapshot", "data": jobBanner + "\n\nold line\n"})
			ws.WriteJSON(map[string]string{"type": "ping"})
			ws.WriteJSON(map[string]string{"type": "chunk", "data": "new li"})
			ws.WriteJSON(map[string]string{"type": "chunk", "data": "ne\n"})
			ws.WriteJSON(map[string]string{"type": "done", "status": status})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ASDL_HUB_URL", srv.URL)
	t.Setenv("ASDL_HUB_TOKEN", "t")
	return query
}

func TestLogs_FollowStreamsUntilTheJobEnds(t *testing.T) {
	query := followServer(t, "completed")
	c, err := newClient()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := (&env{out: &out}).logs(c, "bot", 20, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(*query, "follow=1") || !strings.Contains(*query, "lines=20") {
		t.Fatalf("should ask the Hub to follow, query %q", *query)
	}
	if out.String() != "old line\nnew line\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestLogs_FollowReportsAJobThatFailed(t *testing.T) {
	followServer(t, "failed")
	c, _ := newClient()
	err := (&env{out: &bytes.Buffer{}}).logs(c, "bot", 20, true)
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("want an error naming the failed job, got %v", err)
	}
}
