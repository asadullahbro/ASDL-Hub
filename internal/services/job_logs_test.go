package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/asdl/hub/internal/models"
)

func TestJobLogHubAppendSkipsWhatItAlreadyHas(t *testing.T) {
	h := NewJobLogHub()
	h.Append("j", 0, "hello ")
	h.Append("j", 0, "hello ")     // a retried request
	h.Append("j", 3, "lo world\n") // overlaps the first chunk
	snap, done, _, _, cancel := h.Subscribe("j")
	defer cancel()
	if done || snap != "hello world\n" {
		t.Fatalf("snapshot %q done=%v", snap, done)
	}
	if h.Total("j") != len("hello world\n") {
		t.Fatalf("total %d", h.Total("j"))
	}
}

func TestJobLogHubSendsLiveChunksThenDone(t *testing.T) {
	h := NewJobLogHub()
	h.Append("j", 0, "a")
	_, _, _, ch, cancel := h.Subscribe("j")
	defer cancel()
	h.Append("j", 1, "b")
	h.Finish("j", "completed")
	if ev := <-ch; ev.Chunk != "b" {
		t.Fatalf("got %+v", ev)
	}
	if ev := <-ch; !ev.Done || ev.Status != "completed" {
		t.Fatalf("got %+v", ev)
	}
	// Output after the end is ignored; a late subscriber sees it is over
	h.Append("j", 2, "late")
	snap, done, status, _, _ := h.Subscribe("j")
	if !done || status != "completed" || snap != "ab" {
		t.Fatalf("snap=%q done=%v status=%q", snap, done, status)
	}
}

func TestJobLogHubKeepsOnlyTheTail(t *testing.T) {
	h := NewJobLogHub()
	big := make([]byte, maxLiveLog+10)
	for i := range big {
		big[i] = 'x'
	}
	h.Append("j", 0, string(big)+"END")
	snap, _, _, _, cancel := h.Subscribe("j")
	defer cancel()
	if len(snap) != maxLiveLog || snap[len(snap)-3:] != "END" {
		t.Fatalf("len %d", len(snap))
	}
}

func appendLogs(svc *JobService, jobID, fromIP, body string) int {
	c, w := newTestContext(http.MethodPost, "/jobs/"+jobID+"/logs", body)
	c.Params = gin.Params{{Key: "id", Value: jobID}}
	c.Set("vpn_ip", fromIP)
	svc.AppendLogs(c)
	return w.Code
}

func TestAppendLogsOnlyFromTheJobsOwnNode(t *testing.T) {
	svc, _ := setupDeploy(t, "old") // job j1 runs on node "new" (10.0.0.3)

	if code := appendLogs(svc, "j1", "10.0.0.2", `{"offset":0,"chunk":"x"}`); code != http.StatusForbidden {
		t.Fatalf("another node: %d, want 403", code)
	}
	if code := appendLogs(svc, "j1", "10.0.0.3", `{"offset":0,"chunk":"pulling\n"}`); code != http.StatusOK {
		t.Fatalf("own node: %d, want 200", code)
	}
	if snap, _, _, _, cancel := svc.logs.Subscribe("j1"); snap != "pulling\n" {
		t.Fatalf("snapshot %q", snap)
	} else {
		cancel()
	}

	completeJob(t, svc, models.JobStatusCompleted)
	if code := appendLogs(svc, "j1", "10.0.0.3", `{"offset":8,"chunk":"late"}`); code != http.StatusConflict {
		t.Fatalf("after completion: %d, want 409", code)
	}
}

func TestStreamSendsSnapshotThenChunksThenDone(t *testing.T) {
	svc, _ := setupDeploy(t, "old") // j1 is running
	svc.logs.Append("j1", 0, "start\n")

	r := gin.New()
	r.GET("/jobs/:id/stream", svc.Stream)
	srv := httptest.NewServer(r)
	defer srv.Close()

	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/jobs/j1/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	read := func() map[string]string {
		t.Helper()
		ws.SetReadDeadline(time.Now().Add(3 * time.Second))
		var m map[string]string
		if err := ws.ReadJSON(&m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	if m := read(); m["type"] != "snapshot" || m["data"] != "start\n" {
		t.Fatalf("first message %v", m)
	}
	svc.logs.Append("j1", 6, "pulling\n")
	if m := read(); m["type"] != "chunk" || m["data"] != "pulling\n" {
		t.Fatalf("second message %v", m)
	}
	svc.logs.Finish("j1", models.JobStatusCompleted)
	if m := read(); m["type"] != "done" || m["status"] != "completed" {
		t.Fatalf("third message %v", m)
	}
}
