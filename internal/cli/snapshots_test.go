package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type snapCalls struct{ method, path, body string }

func snapshotServer(t *testing.T, plugins []map[string]interface{}) *[]snapCalls {
	t.Helper()
	calls := &[]snapCalls{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/api/v1/projects" {
			*calls = append(*calls, snapCalls{r.Method, r.URL.RequestURI(), string(b)})
		}
		switch {
		case r.URL.Path == "/api/v1/projects":
			json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]interface{}{{"id": "p1", "name": "chat", "node_id": "n1"}}})
		case r.URL.Path == "/api/v1/projects/p1/snapshots" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]interface{}{"enabled": true, "keep": 3, "max_mb": 32, "plugins": plugins})
		case r.URL.Path == "/api/v1/projects/p1/snapshots" && r.Method == http.MethodPost:
			json.NewEncoder(w).Encode(map[string]interface{}{"job_id": "0123456789abcdef"})
		case strings.HasSuffix(r.URL.Path, "/restore"):
			json.NewEncoder(w).Encode(map[string]interface{}{"generation": 9, "redeploying": true})
		default:
			json.NewEncoder(w).Encode(map[string]interface{}{})
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ASDL_HUB_URL", srv.URL)
	t.Setenv("ASDL_HUB_TOKEN", "t")
	return calls
}

func redisPlugin(persist, snaps, healthy bool) map[string]interface{} {
	return map[string]interface{}{"plugin_id": "redis", "persist": persist, "snapshots": snaps, "interval_minutes": 5, "healthy": healthy,
		"items": []map[string]interface{}{
			{"id": "s2", "generation": 2, "trigger": "scheduled", "size_bytes": 2048, "created_at": time.Now().Add(-3 * time.Minute)},
			{"id": "s1", "generation": 1, "trigger": "move", "size_bytes": 900, "created_at": time.Now().Add(-2 * time.Hour)},
		}}
}

func TestSnapshots_ListShowsStateAndEachSnapshot(t *testing.T) {
	snapshotServer(t, []map[string]interface{}{redisPlugin(true, true, true)})
	c, _ := newClient()
	var out bytes.Buffer
	if err := (&env{out: &out}).snapshots(c, "chat"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"redis", "on, every 5 min", "2.0 KB", "scheduled", "move", "3m ago", "newest 3 per plugin"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestSnapshots_ListFlagsABehindScheduleAndAnEmptyApp(t *testing.T) {
	snapshotServer(t, []map[string]interface{}{redisPlugin(true, true, false)})
	c, _ := newClient()
	var out bytes.Buffer
	(&env{out: &out}).snapshots(c, "chat")
	if !strings.Contains(out.String(), "BEHIND") {
		t.Errorf("a schedule that stopped must be flagged:\n%s", out.String())
	}
	snapshotServer(t, nil)
	c, _ = newClient()
	out.Reset()
	(&env{out: &out}).snapshots(c, "chat")
	if !strings.Contains(out.String(), "no plugin that can keep its data") {
		t.Errorf("unexpected output:\n%s", out.String())
	}
}

func TestSnapshotOn_SetsTheThreeSettingsAndOffKeepsTheData(t *testing.T) {
	calls := snapshotServer(t, []map[string]interface{}{redisPlugin(false, false, false)})
	c, _ := newClient()
	var out bytes.Buffer
	e := &env{out: &out}
	if err := e.snapshotCommand(c, []string{"on", "chat", "--minutes", "10"}); err != nil {
		t.Fatal(err)
	}
	var put snapCalls
	for _, c := range *calls {
		if c.method == "PUT" {
			put = c
		}
	}
	if put.path != "/api/v1/projects/p1/plugins/redis" ||
		!strings.Contains(put.body, `"key":"persist","value":"on"`) || !strings.Contains(put.body, `"key":"snapshot","value":"on"`) || !strings.Contains(put.body, `"key":"snapshot_minutes","value":"10"`) {
		t.Errorf("PUT = %+v", put)
	}
	if !strings.Contains(out.String(), "empty data folder") {
		t.Errorf("turning persistence on for the first time must warn that the data restarts empty:\n%s", out.String())
	}

	*calls = nil
	out.Reset()
	if err := e.snapshotCommand(c, []string{"off", "chat"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range *calls {
		if c.method == "DELETE" {
			t.Errorf("turning snapshots off must keep the stored snapshots unless --delete: %+v", c)
		}
	}
	if !strings.Contains(out.String(), "2 stored snapshot(s) were kept") {
		t.Errorf("should say the snapshots were kept:\n%s", out.String())
	}

	*calls = nil
	if err := e.snapshotCommand(c, []string{"off", "chat", "--delete"}); err != nil {
		t.Fatal(err)
	}
	deleted := false
	for _, c := range *calls {
		if c.method == "DELETE" && c.path == "/api/v1/projects/p1/snapshots?plugin_id=redis" {
			deleted = true
		}
	}
	if !deleted {
		t.Errorf("--delete must remove the stored snapshots: %+v", *calls)
	}
}

func TestSnapshotNowRestoreDelete(t *testing.T) {
	calls := snapshotServer(t, []map[string]interface{}{redisPlugin(true, true, true)})
	c, _ := newClient()
	var out bytes.Buffer
	e := &env{out: &out}
	if err := e.snapshotCommand(c, []string{"now", "chat"}); err != nil || !strings.Contains(out.String(), "queued") {
		t.Fatalf("now: %v %s", err, out.String())
	}
	out.Reset()
	if err := e.snapshotCommand(c, []string{"restore", "chat", "1"}); err != nil || !strings.Contains(out.String(), "generation 9") {
		t.Fatalf("restore: %v %s", err, out.String())
	}
	restored := false
	for _, c := range *calls {
		if c.method == "POST" && c.path == "/api/v1/projects/p1/snapshots/s1/restore" {
			restored = true
		}
	}
	if !restored {
		t.Errorf("generation 1 is snapshot s1; calls: %+v", *calls)
	}
	if err := e.snapshotCommand(c, []string{"restore", "chat", "42"}); err == nil || !strings.Contains(err.Error(), "no snapshot with generation 42") {
		t.Errorf("an unknown generation must be an error, got %v", err)
	}
	if err := e.snapshotCommand(c, []string{"restore", "chat", "abc"}); err == nil {
		t.Error("a non-number generation must be an error")
	}
	if err := e.snapshotCommand(c, []string{"delete", "chat", "2"}); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotCommand_NeedsAChoiceWhenSeveralPluginsKeepData(t *testing.T) {
	other := redisPlugin(true, true, true)
	other["plugin_id"] = "db"
	snapshotServer(t, []map[string]interface{}{redisPlugin(true, true, true), other})
	c, _ := newClient()
	err := (&env{out: &bytes.Buffer{}}).snapshotCommand(c, []string{"now", "chat"})
	if err == nil || !strings.Contains(err.Error(), "name one: redis db") {
		t.Errorf("want a request to name the plugin, got %v", err)
	}
}
