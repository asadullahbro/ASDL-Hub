package services

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

type snapEnv struct {
	t       *testing.T
	db      *gorm.DB
	svc     *SnapshotService
	srv     *httptest.Server
	caller  *string // the mesh address the next request appears to come from
	project models.Project
}

// newSnapEnv runs the real snapshot routes on a local server, with project p1 on node "good" and a Redis plugin
// that keeps its data and sends snapshots to the Hub.
func newSnapEnv(t *testing.T) *snapEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	PluginDir = t.TempDir()
	db := testutil.NewDB(t, &models.Node{}, &models.Job{}, &models.Migration{}, &models.Project{}, &models.ProjectPlugin{},
		&models.Deployment{}, &models.OIDCDeployment{}, &models.Setting{}, &models.GitHubToken{}, &models.PluginSnapshot{})
	db.Create(&models.Node{ID: "good", Hostname: "good", VPNIP: "10.0.0.3", Online: true, HealthScore: 90})
	db.Create(&models.Node{ID: "other", Hostname: "other", VPNIP: "10.0.0.4", Online: true, HealthScore: 60})
	db.Create(&models.Project{ID: "p1", Name: "chat", NodeID: "good", Image: "ghcr.io/o/chat:v1", Status: "running", AutoPort: true, Ports: []string{"20000:3000"}})
	db.Create(&models.ProjectPlugin{ID: "pp1", ProjectID: "p1", PluginID: "redis", Vars: models.SecretEnvVars{
		{Key: "password", Value: "pw"}, {Key: PersistVarKey, Value: "on"}, {Key: SnapshotVarKey, Value: "on"}, {Key: SnapshotMinutesKey, Value: "5"}}})

	e := &snapEnv{t: t, db: db}
	addr := "10.0.0.3"
	e.caller = &addr
	e.svc = NewSnapshotService(db, func(*models.Project) error { return nil })
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("vpn_ip", *e.caller); c.Next() })
	r.POST("/api/v1/snapshots/:project/:plugin", e.svc.Upload)
	r.GET("/api/v1/snapshots/:project/:plugin/meta", e.svc.Meta)
	r.GET("/api/v1/snapshots/:project/:plugin/latest", e.svc.Latest)
	r.GET("/projects/:id/snapshots", e.svc.List)
	r.POST("/projects/:id/snapshots", e.svc.TakeNow)
	r.DELETE("/projects/:id/snapshots", e.svc.DeleteAll)
	r.DELETE("/projects/:id/snapshots/:snapshot", e.svc.Delete)
	r.POST("/projects/:id/snapshots/:snapshot/restore", e.svc.Restore)
	e.srv = httptest.NewServer(r)
	t.Cleanup(e.srv.Close)
	SetSnapshotHub("hub-secret", e.srv.URL, 1<<20, 3)
	t.Cleanup(func() { SetSnapshotHub("", "", 0, 0) })
	db.First(&e.project, "id = ?", "p1")
	return e
}

func (e *snapEnv) token() string { return SnapshotToken("hub-secret", "p1", "redis") }

func (e *snapEnv) do(method, path, token string, body []byte, headers ...string) (int, string, http.Header) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b), res.Header
}

const snapPath = "/api/v1/snapshots/p1/redis"

func (e *snapEnv) plugin() ResolvedPlugin {
	e.t.Helper()
	for _, pl := range resolvePlugins(e.db, &e.project) {
		fillSnapshot(&e.project, &pl, "")
		return pl
	}
	e.t.Fatal("no plugin")
	return ResolvedPlugin{}
}

func TestSnapshotUpload_NeedsTheRightTokenAndTheHostingNode(t *testing.T) {
	e := newSnapEnv(t)
	if code, _, _ := e.do("POST", snapPath, "", []byte("x")); code != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", code)
	}
	if code, _, _ := e.do("POST", snapPath, SnapshotToken("hub-secret", "p1", "searxng"), []byte("x")); code != http.StatusUnauthorized {
		t.Errorf("another plugin's token: %d, want 401", code)
	}
	if code, _, _ := e.do("POST", snapPath, SnapshotToken("wrong", "p1", "redis"), []byte("x")); code != http.StatusUnauthorized {
		t.Errorf("token signed with another key: %d, want 401", code)
	}
	*e.caller = "10.0.0.4" // a node the project does not run on
	if code, _, _ := e.do("POST", snapPath, e.token(), []byte("data")); code != http.StatusConflict {
		t.Errorf("upload from a node that doesn't host the project: %d, want 409", code)
	}
	*e.caller = "10.0.0.3"
	if code, _, _ := e.do("POST", snapPath, e.token(), nil); code != http.StatusBadRequest {
		t.Errorf("empty upload: %d, want 400", code)
	}
	if code, _, _ := e.do("POST", snapPath, e.token(), bytes.Repeat([]byte("a"), 2<<20)); code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized upload: %d, want 413", code)
	}
	code, body, _ := e.do("POST", snapPath, e.token(), []byte("hello"), "X-Snapshot-Trigger", "move")
	if code != http.StatusCreated || !strings.Contains(body, `"generation":1`) {
		t.Fatalf("good upload: %d %s", code, body)
	}
	var n int64
	e.db.Model(&models.PluginSnapshot{}).Count(&n)
	if n != 1 {
		t.Errorf("%d snapshots stored, want exactly the one good upload", n)
	}
}

func TestSnapshots_GenerationsGrowAndOnlyTheNewestAreKept(t *testing.T) {
	e := newSnapEnv(t)
	for i := 1; i <= 5; i++ {
		code, body, _ := e.do("POST", snapPath, e.token(), []byte(strings.Repeat("v", i)))
		if code != http.StatusCreated || !strings.Contains(body, `"generation":`+string(rune('0'+i))) {
			t.Fatalf("upload %d: %d %s", i, code, body)
		}
	}
	var gens []int64
	e.db.Model(&models.PluginSnapshot{}).Order("generation").Pluck("generation", &gens)
	if len(gens) != 3 || gens[0] != 3 || gens[2] != 5 {
		t.Errorf("kept generations %v, want [3 4 5]", gens)
	}
	code, body, h := e.do("GET", snapPath+"/latest", e.token(), nil)
	if code != 200 || body != "vvvvv" || h.Get("X-Generation") != "5" {
		t.Errorf("latest: %d %q gen %s", code, body, h.Get("X-Generation"))
	}
	code, body, _ = e.do("GET", snapPath+"/meta", e.token(), nil)
	if code != 200 || !strings.Contains(body, `"generation":5`) || !strings.Contains(body, `"sha256"`) {
		t.Errorf("meta: %d %s", code, body)
	}
}

func TestSnapshots_NothingStoredYet(t *testing.T) {
	e := newSnapEnv(t)
	if code, body, _ := e.do("GET", snapPath+"/meta", e.token(), nil); code != 200 || !strings.Contains(body, `"generation":0`) {
		t.Errorf("meta with nothing stored: %d %s", code, body)
	}
	if code, _, _ := e.do("GET", snapPath+"/latest", e.token(), nil); code != http.StatusNotFound {
		t.Errorf("latest with nothing stored: %d, want 404", code)
	}
}

func TestSnapshots_OffForAPluginMeansNoSnapshots(t *testing.T) {
	e := newSnapEnv(t)
	e.db.Model(&models.ProjectPlugin{}).Where("id = ?", "pp1").Update("vars", models.SecretEnvVars{
		{Key: "password", Value: "pw"}, {Key: PersistVarKey, Value: "on"}, {Key: SnapshotVarKey, Value: "off"}})
	if code, _, _ := e.do("POST", snapPath, e.token(), []byte("x")); code != http.StatusNotFound {
		t.Errorf("upload with snapshots off: %d, want 404", code)
	}
	SetSnapshotHub("", "", 0, 0)
	if code, _, _ := e.do("GET", snapPath+"/meta", e.token(), nil); code != http.StatusServiceUnavailable {
		t.Errorf("snapshots disabled on the Hub: %d, want 503", code)
	}
}

func TestSnapshotMeta_WaitsForTheFinalSnapshotOfAMove(t *testing.T) {
	e := newSnapEnv(t)
	e.db.Create(&models.Job{ID: "j-final", NodeID: "good", Type: models.JobTypeCommand, Status: models.JobStatusRunning})
	go func() {
		time.Sleep(1500 * time.Millisecond)
		e.do("POST", snapPath, e.token(), []byte("final"), "X-Snapshot-Trigger", "move", "X-Job-Id", "j-final")
	}()
	start := time.Now()
	code, body, _ := e.do("GET", snapPath+"/meta?after_job=j-final&wait=30", e.token(), nil)
	if code != 200 || !strings.Contains(body, `"generation":1`) {
		t.Fatalf("meta after the final snapshot: %d %s", code, body)
	}
	if d := time.Since(start); d < time.Second || d > 10*time.Second {
		t.Errorf("waited %v; want it to wait for the upload, not time out", d)
	}

	// A job that failed (or never existed) must not hold a deploy up.
	e.db.Create(&models.Job{ID: "j-failed", NodeID: "good", Type: models.JobTypeCommand, Status: models.JobStatusFailed})
	start = time.Now()
	e.do("GET", snapPath+"/meta?after_job=j-failed&wait=30", e.token(), nil)
	e.do("GET", snapPath+"/meta?after_job=never-existed&wait=30", e.token(), nil)
	if time.Since(start) > 3*time.Second {
		t.Error("a finished or missing job must not make the deploy wait")
	}
}

// ---- the scripts, run for real against the real routes ----

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(path string) string { b, _ := os.ReadFile(path); return string(b) }

func TestSnapshotScripts_BackUpThenRestoreOnAnotherNodeAndSkipWhenCurrent(t *testing.T) {
	e := newSnapEnv(t)
	pl := e.plugin()
	dump := filepath.Join(pl.Snapshot.DataDir, "dump.rdb")
	writeFile(t, dump, "calls=41")

	// 1. The node takes a snapshot: Redis is told to save first, and the Hub ends up with generation 1.
	script, env := BuildSnapshotCommand(pl, models.SnapshotScheduled, "job-1")
	calls, out, err := runWithFakeDockerFS(t, script, env, nil, false)
	if err != nil {
		t.Fatalf("snapshot script failed: %v\n%s", err, out)
	}
	if !strings.Contains(calls, "exec\nchat-redis\nredis-cli") || !strings.Contains(calls, "SAVE") {
		t.Errorf("Redis must be told to save before the folder is copied:\n%s", calls)
	}
	if got := readFile(filepath.Join(pl.Snapshot.StateDir, "generation")); strings.TrimSpace(got) != "1" {
		t.Errorf("the node's generation marker = %q, want 1", got)
	}
	var snap models.PluginSnapshot
	e.db.Omit("data").First(&snap)
	if snap.Generation != 1 || snap.JobID != "job-1" || snap.Trigger != models.SnapshotScheduled || snap.SizeBytes == 0 {
		t.Errorf("stored snapshot = %+v", snap)
	}

	// 2. The project fails over to a different node: its data folder is empty. The deploy restores it first.
	PluginDir = t.TempDir()
	e.project.NodeID = "other"
	pl2 := e.plugin()
	if pl2.Snapshot.DataDir == pl.Snapshot.DataDir {
		t.Fatal("test setup: the new node must use a different folder")
	}
	deploy := func(p ResolvedPlugin) string {
		t.Helper()
		sh, denv := BuildDeployCommand(DeploySpec{Project: &e.project, Image: "img", Plugins: []ResolvedPlugin{p}})
		_, o, err := runWithFakeDockerFS(t, sh, denv, nil, false)
		if err != nil {
			t.Fatalf("deploy script failed: %v\n%s", err, o)
		}
		return o
	}
	out = deploy(pl2)
	if got := readFile(filepath.Join(pl2.Snapshot.DataDir, "dump.rdb")); got != "calls=41" {
		t.Fatalf("restored dump.rdb = %q, want the data from the old node\n%s", got, out)
	}
	if got := strings.TrimSpace(readFile(filepath.Join(pl2.Snapshot.StateDir, "generation"))); got != "1" {
		t.Errorf("generation marker after restore = %q, want 1", got)
	}

	// 3. Redeploying on the same node must NOT overwrite newer local data with the older snapshot.
	writeFile(t, filepath.Join(pl2.Snapshot.DataDir, "dump.rdb"), "calls=57")
	out = deploy(pl2)
	if got := readFile(filepath.Join(pl2.Snapshot.DataDir, "dump.rdb")); got != "calls=57" {
		t.Errorf("a current node's data was overwritten: %q\n%s", got, out)
	}

	// 4. A newer snapshot on the Hub (another node took over and uploaded) does replace this node's older copy.
	e.svc.store("p1", "redis", "other", models.SnapshotScheduled, "", mustGzipOf(t, "calls=99"), "")
	e.db.Model(&models.PluginSnapshot{}).Where("generation = 2").Update("sha256", sha256Hex(mustGzipOf(t, "calls=99")))
	out = deploy(pl2)
	if got := readFile(filepath.Join(pl2.Snapshot.DataDir, "dump.rdb")); got != "calls=99" {
		t.Errorf("the newer snapshot was not restored: %q\n%s", got, out)
	}
}

func TestSnapshotRestore_RefusesACorruptDownloadAndKeepsLocalData(t *testing.T) {
	e := newSnapEnv(t)
	pl := e.plugin()
	e.svc.store("p1", "redis", "good", models.SnapshotScheduled, "", mustGzipOf(t, "calls=1"), "0000000000000000000000000000000000000000000000000000000000000000")
	writeFile(t, filepath.Join(pl.Snapshot.DataDir, "dump.rdb"), "local data")
	sh, env := BuildDeployCommand(DeploySpec{Project: &e.project, Image: "img", Plugins: []ResolvedPlugin{pl}})
	_, out, err := runWithFakeDockerFS(t, sh, env, nil, false)
	if err != nil {
		t.Fatalf("a bad snapshot must never fail the deploy: %v\n%s", err, out)
	}
	if got := readFile(filepath.Join(pl.Snapshot.DataDir, "dump.rdb")); got != "local data" {
		t.Errorf("local data was replaced by a snapshot that failed its checksum: %q", got)
	}
	if !strings.Contains(out, "checksum") {
		t.Errorf("the log should say why nothing was restored:\n%s", out)
	}
}

func TestSnapshotRestore_AnUnreachableHubNeverFailsTheDeploy(t *testing.T) {
	e := newSnapEnv(t)
	SetSnapshotHub("hub-secret", "http://127.0.0.1:1", 1<<20, 3) // nothing listens here
	pl := e.plugin()
	writeFile(t, filepath.Join(pl.Snapshot.DataDir, "dump.rdb"), "local data")
	sh, env := BuildDeployCommand(DeploySpec{Project: &e.project, Image: "img", Plugins: []ResolvedPlugin{pl}})
	_, out, err := runWithFakeDockerFS(t, sh, env, nil, false)
	if err != nil {
		t.Fatalf("deploy failed because the Hub was down: %v\n%s", err, out)
	}
	if !strings.Contains(out, "could not be reached") || readFile(filepath.Join(pl.Snapshot.DataDir, "dump.rdb")) != "local data" {
		t.Errorf("want local data kept and a clear message:\n%s", out)
	}
}

func TestSnapshots_AreOffInTheDeployScriptUnlessTurnedOn(t *testing.T) {
	e := newSnapEnv(t)
	e.db.Model(&models.ProjectPlugin{}).Where("id = ?", "pp1").Update("vars", models.SecretEnvVars{
		{Key: "password", Value: "pw"}, {Key: PersistVarKey, Value: "on"}})
	plugins := resolvePlugins(e.db, &e.project)
	sh, env := BuildDeployCommand(DeploySpec{Project: &e.project, Image: "img", Plugins: plugins})
	if strings.Contains(sh, "snapshots") || strings.Contains(strings.Join(env, "\n"), "SNAPTOKEN") {
		t.Errorf("persistence alone must not talk to the Hub:\n%s", sh)
	}
}

// ---- moves, scheduling, dashboard routes ----

func TestDispatch_PlannedMoveTakesAFinalSnapshotAndTheNewNodeWaitsForIt(t *testing.T) {
	e := newSnapEnv(t)
	d := NewDeployer(e.db)
	var target models.Node
	e.db.First(&target, "id = ?", "other")

	job, _, err := d.Dispatch(&e.project, &target, "img:v2", DeployMeta{Trigger: TriggerMigration})
	if err != nil {
		t.Fatal(err)
	}
	var snapJob models.Job
	if err := e.db.First(&snapJob, "node_id = ? AND type = ?", "good", models.JobTypeCommand).Error; err != nil {
		t.Fatalf("expected a final snapshot job on the old node: %v", err)
	}
	if !strings.Contains(snapJob.Command, "X-Snapshot-Trigger: move") || !strings.Contains(snapJob.Command, "X-Job-Id: "+snapJob.ID) {
		t.Errorf("the final snapshot must be marked as a move and carry its job id:\n%s", snapJob.Command)
	}
	if !strings.Contains(job.Command, "after_job="+snapJob.ID) {
		t.Errorf("the new node's deploy must wait for that snapshot:\n%s", job.Command)
	}

	// A failover (the old node is gone) has nothing to wait for; it just uses the newest stored snapshot.
	e.db.Where("1 = 1").Delete(&models.Job{})
	job, _, _ = d.Dispatch(&e.project, &target, "img:v2", DeployMeta{Trigger: TriggerFailover})
	var n int64
	e.db.Model(&models.Job{}).Where("type = ?", models.JobTypeCommand).Count(&n)
	if n != 0 || strings.Contains(job.Command, "after_job=") {
		t.Errorf("failover must not queue or wait for a final snapshot (%d command jobs)", n)
	}

	// An old node that is offline can't take one either.
	e.db.Model(&models.Node{}).Where("id = ?", "good").Update("online", false)
	e.db.Where("1 = 1").Delete(&models.Job{})
	job, _, _ = d.Dispatch(&e.project, &target, "img:v2", DeployMeta{Trigger: TriggerMigration})
	e.db.Model(&models.Job{}).Where("type = ?", models.JobTypeCommand).Count(&n)
	if n != 0 || strings.Contains(job.Command, "after_job=") {
		t.Error("a move from an offline node must not wait for a snapshot that can't happen")
	}
}

func TestSnapshotScheduler_AsksForSnapshotsOnTimeAndNeverTwice(t *testing.T) {
	e := newSnapEnv(t)
	count := func() int64 {
		var n int64
		e.db.Model(&models.Job{}).Where("type = ?", models.JobTypeCommand).Count(&n)
		return n
	}
	e.svc.tick(time.Now())
	if count() != 1 {
		t.Fatalf("first tick queued %d snapshot jobs, want 1 (nothing stored yet)", count())
	}
	e.svc.tick(time.Now())
	if count() != 1 {
		t.Errorf("a snapshot job is already waiting; got %d", count())
	}
	e.db.Model(&models.Job{}).Where("1 = 1").Update("status", models.JobStatusCompleted)
	e.svc.store("p1", "redis", "good", models.SnapshotScheduled, "", []byte("d"), "x")
	e.svc.tick(time.Now())
	if count() != 1 {
		t.Errorf("the newest snapshot is fresh, nothing should be queued; got %d", count())
	}
	e.svc.tick(time.Now().Add(6 * time.Minute))
	if count() != 2 {
		t.Errorf("after the 5 minute interval a new snapshot is due; got %d jobs", count())
	}

	// Offline node, or snapshots off: nothing is queued.
	e.db.Where("1 = 1").Delete(&models.Job{})
	e.db.Model(&models.Node{}).Where("id = ?", "good").Update("online", false)
	e.svc.tick(time.Now().Add(time.Hour))
	e.db.Model(&models.Node{}).Where("id = ?", "good").Update("online", true)
	e.db.Model(&models.ProjectPlugin{}).Where("id = ?", "pp1").Update("vars", models.SecretEnvVars{{Key: "password", Value: "pw"}})
	e.svc.tick(time.Now().Add(time.Hour))
	if count() != 0 {
		t.Errorf("queued %d jobs for an offline node or a plugin with snapshots off", count())
	}
}

func TestSnapshotRoutes_ListTakeNowRestoreDelete(t *testing.T) {
	e := newSnapEnv(t)
	e.svc.store("p1", "redis", "good", models.SnapshotScheduled, "", []byte("one"), "a")
	e.svc.store("p1", "redis", "good", models.SnapshotScheduled, "", []byte("two"), "b")

	code, body, _ := e.do("GET", "/projects/p1/snapshots", "", nil)
	if code != 200 || !strings.Contains(body, `"snapshots":true`) || !strings.Contains(body, `"healthy":true`) || strings.Contains(body, `"data"`) {
		t.Errorf("list: %d %s (must show health and never the snapshot bytes)", code, body)
	}
	code, body, _ = e.do("POST", "/projects/p1/snapshots", "", []byte(`{"plugin_id":"redis"}`))
	if code != http.StatusAccepted || !strings.Contains(body, "job_id") {
		t.Errorf("take now: %d %s", code, body)
	}
	if code, _, _ = e.do("POST", "/projects/p1/snapshots", "", []byte(`{"plugin_id":"searxng"}`)); code != http.StatusBadRequest {
		t.Errorf("take now for a plugin without snapshots: %d, want 400", code)
	}

	var first models.PluginSnapshot
	e.db.Omit("data").Where("generation = 1").First(&first)
	code, body, _ = e.do("POST", "/projects/p1/snapshots/"+first.ID+"/restore", "", nil)
	if code != http.StatusAccepted || !strings.Contains(body, `"generation":3`) || !strings.Contains(body, `"redeploying":true`) {
		t.Errorf("restore: %d %s (the old snapshot becomes the newest generation and the project redeploys)", code, body)
	}
	var newest models.PluginSnapshot
	e.db.Where("generation = 3").First(&newest)
	if string(newest.Data) != "one" || newest.Trigger != models.SnapshotRestore {
		t.Errorf("restored copy = %q trigger %s", newest.Data, newest.Trigger)
	}

	if code, _, _ = e.do("DELETE", "/projects/p1/snapshots/"+newest.ID, "", nil); code != 200 {
		t.Errorf("delete: %d", code)
	}
	code, body, _ = e.do("DELETE", "/projects/p1/snapshots?plugin_id=redis", "", nil)
	var left int64
	e.db.Model(&models.PluginSnapshot{}).Count(&left)
	if code != 200 || left != 0 {
		t.Errorf("delete all: %d %s, %d left", code, body, left)
	}
}

func TestSnapshotList_FlagsASilentSchedule(t *testing.T) {
	e := newSnapEnv(t)
	e.svc.store("p1", "redis", "good", models.SnapshotScheduled, "", []byte("one"), "a")
	e.db.Model(&models.PluginSnapshot{}).Where("1 = 1").Update("created_at", time.Now().Add(-time.Hour))
	_, body, _ := e.do("GET", "/projects/p1/snapshots", "", nil)
	if !strings.Contains(body, `"healthy":false`) {
		t.Errorf("an hour old snapshot on a 5 minute schedule must be flagged: %s", body)
	}
}

func mustGzipOf(t *testing.T, content string) []byte {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dump.rdb"), content)
	out := filepath.Join(t.TempDir(), "x.tgz")
	if b, err := exec.Command("tar", "-C", dir, "-czf", out, ".").CombinedOutput(); err != nil {
		t.Fatalf("tar: %v %s", err, b)
	}
	b, _ := os.ReadFile(out)
	return b
}

func sha256Hex(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// runWithFakeDockerFS is runWithFakeDocker plus an emulation of the throwaway container the snapshot scripts use to
// pack and unpack a data folder: "docker run --rm -v HOST:/data --entrypoint tar|sh IMAGE ..." runs the same tar
// or shell on the host folder, which is all the real container does.
func runWithFakeDockerFS(t *testing.T, script string, env []string, busy []string, crashed bool) (calls, output string, err error) {
	t.Helper()
	dir := t.TempDir()
	logFile := filepath.Join(dir, "calls")
	fake := `#!/bin/sh
{ echo "CALL"; for a in "$@"; do echo "$a"; done; } >> "` + logFile + `"
case "$1" in
  inspect) echo true ;;
  image) echo "3000/tcp " ;;
  run)
    if [ "$2" = "--rm" ]; then
      shift 2
      host=""; ent=""
      while [ $# -gt 0 ]; do
        case "$1" in
          -i) shift ;;
          -v) host="${2%%:*}"; shift 2 ;;
          --entrypoint) ent="$2"; shift 2 ;;
          *) break ;;
        esac
      done
      shift
      case "$ent" in
        tar) exec tar -czf - -C "$host" . ;;
        sh) body=$(printf '%s' "$2" | sed "s#/data#$host#g"); exec sh -c "$body" ;;
      esac
    fi ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(fake), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sleep"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append([]string{"PATH=" + dir + ":" + os.Getenv("PATH")}, env...)
	out, err := cmd.CombinedOutput()
	logged, _ := os.ReadFile(logFile)
	return string(logged), string(out), err
}
