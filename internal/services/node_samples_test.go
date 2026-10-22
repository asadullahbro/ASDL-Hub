package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/asdl/hub/internal/models"
)

func countSamples(svc *NodeService, nodeID string) int64 {
	var n int64
	svc.db.Model(&models.NodeSample{}).Where("node_id = ?", nodeID).Count(&n)
	return n
}

func TestSampleDue_AtMostOneAMinutePerNode(t *testing.T) {
	now := time.Now()
	if !sampleDue("sd-a", now) {
		t.Fatal("the first sample of a node is due")
	}
	if sampleDue("sd-a", now.Add(30*time.Second)) {
		t.Fatal("a second one within a minute is not")
	}
	if !sampleDue("sd-b", now.Add(30*time.Second)) {
		t.Fatal("another node has its own clock")
	}
	if !sampleDue("sd-a", now.Add(61*time.Second)) {
		t.Fatal("a minute later it is due again")
	}
}

func TestHeartbeat_StoresASampleButNotOnEveryBeat(t *testing.T) {
	svc, db := newNodeTestService(t)
	db.Create(&models.Node{ID: "hb-n1", Hostname: "tower", VPNIP: "10.100.0.8", Online: true, MemoryTotal: 100, DiskTotal: 200})
	beat := func() {
		c, w := newTestContext(http.MethodPost, "/nodes/self/heartbeat", `{"cpu_percent":40,"memory_used":25,"disk_used":50,"load_avg_1":1.5,"ping_latency":12,"wifi_signal":70}`)
		c.Params = gin.Params{{Key: "id", Value: "self"}}
		c.Set("vpn_ip", "10.100.0.8")
		svc.Heartbeat(c)
		if w.Code != http.StatusOK {
			t.Fatalf("heartbeat got %d: %s", w.Code, w.Body.String())
		}
	}
	beat()
	beat()
	beat()
	if n := countSamples(svc, "hb-n1"); n != 1 {
		t.Fatalf("three beats in a row make one sample, got %d", n)
	}
	var s models.NodeSample
	db.First(&s, "node_id = ?", "hb-n1")
	if s.CPUPercent != 40 || s.MemoryPercent != 25 || s.DiskPercent != 25 || s.Load1 != 1.5 || s.PingMs != 12 || s.WiFiSignal != 70 {
		t.Fatalf("sample has the wrong readings: %+v", s)
	}
}

func TestTrimSamples_DropsOnlyOldOnes(t *testing.T) {
	svc, db := newNodeTestService(t)
	now := time.Now()
	db.Create(&models.NodeSample{NodeID: "n1", At: now.Add(-15 * 24 * time.Hour)})
	db.Create(&models.NodeSample{NodeID: "n1", At: now.Add(-13 * 24 * time.Hour)})
	db.Create(&models.NodeSample{NodeID: "n1", At: now})
	if got := trimSamples(db, now); got != 1 {
		t.Fatalf("removed %d, want 1", got)
	}
	if n := countSamples(svc, "n1"); n != 2 {
		t.Fatalf("kept %d, want 2", n)
	}
}

func TestDownsample(t *testing.T) {
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var in []models.NodeSample
	for i := 0; i < 1000; i++ {
		in = append(in, models.NodeSample{At: base.Add(time.Duration(i) * time.Minute), CPUPercent: float64(i % 100), WiFiSignal: 80})
	}
	out := downsample(in, 100)
	if len(out) != 100 {
		t.Fatalf("got %d points, want 100", len(out))
	}
	for i := 1; i < len(out); i++ {
		if !out[i].At.After(out[i-1].At) {
			t.Fatal("points must stay in time order")
		}
	}
	// Each bucket is 10 samples with CPU 0..9, 10..19, ...: the average of the first is 4.5.
	if out[0].CPUPercent != 4.5 || out[0].WiFiSignal != 80 {
		t.Fatalf("buckets should be averaged, first = %+v", out[0])
	}
	if got := downsample(in[:50], 100); len(got) != 50 {
		t.Fatalf("few samples are returned as they are, got %d", len(got))
	}
}

func TestSamplesAPI(t *testing.T) {
	svc, db := newNodeTestService(t)
	db.Create(&models.Node{ID: "api-n1", Hostname: "tower", VPNIP: "10.100.0.9"})
	now := time.Now()
	for _, ago := range []time.Duration{90 * time.Minute, 30 * time.Minute, 5 * time.Minute} {
		db.Create(&models.NodeSample{NodeID: "api-n1", At: now.Add(-ago), CPUPercent: 10})
	}
	get := func(id, query string) (int, struct {
		Hours   int                 `json:"hours"`
		Samples []models.NodeSample `json:"samples"`
	}) {
		c, w := newTestContext(http.MethodGet, "/nodes/"+id+"/samples"+query, "")
		c.Params = gin.Params{{Key: "id", Value: id}}
		svc.Samples(c)
		var out struct {
			Hours   int                 `json:"hours"`
			Samples []models.NodeSample `json:"samples"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	if code, out := get("api-n1", "?hours=1"); code != 200 || len(out.Samples) != 2 {
		t.Fatalf("the last hour holds 2 samples, got %d (status %d)", len(out.Samples), code)
	}
	if code, out := get("api-n1", ""); code != 200 || out.Hours != 24 || len(out.Samples) != 3 {
		t.Fatalf("default is 24 hours, got %+v", out)
	}
	if _, out := get("api-n1", fmt.Sprintf("?hours=%d", 9999)); out.Hours != maxSampleHours {
		t.Fatalf("hours must be capped at %d, got %d", maxSampleHours, out.Hours)
	}
	if code, _ := get("ghost", ""); code != http.StatusNotFound {
		t.Fatalf("unknown node should be 404, got %d", code)
	}
}
