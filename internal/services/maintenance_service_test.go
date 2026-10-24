package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

func TestMaintenance_MovesAppsAndBlocksNewOnes(t *testing.T) {
	hs, _, db := newFailoverEnv(t)
	// p1 runs on "good"; "ok" is the only other available node.
	db.Model(&models.Project{}).Where("id = ?", "p1").Update("node_id", "good")
	ops := NewNodeOpsService(db, NewDeployer(db))

	var good models.Node
	db.First(&good, "id = ?", "good")
	res, err := ops.SetMaintenance(&good, true, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Moving) != 1 || len(res.Stays) != 0 || !res.Node.Maintenance || res.Node.MaintenanceSince == nil {
		t.Fatalf("result = %+v", res)
	}
	var job models.Job
	if err := db.First(&job, "type = ?", models.JobTypeDeploy).Error; err != nil || job.NodeID != "ok" {
		t.Fatalf("expected the app to be redeployed on ok: %+v %v", job, err)
	}
	if loadProject(db).NodeID != "good" {
		t.Error("the app must stay on its node until the new copy runs")
	}

	// A maintenance node is never a failover target.
	db.Model(&models.Project{}).Where("id = ?", "p1").Update("node_id", "ok")
	p := loadProject(db)
	if target := hs.failoverTarget(&p); target != nil {
		t.Errorf("failover picked %s; the only other node is in maintenance", target.Hostname)
	}
}

func TestMaintenance_AppStaysWhenNoOtherNode(t *testing.T) {
	_, _, db := newFailoverEnv(t)
	db.Model(&models.Project{}).Where("id = ?", "p1").Update("node_id", "good")
	db.Model(&models.Node{}).Where("id = ?", "ok").Update("online", false)
	ops := NewNodeOpsService(db, NewDeployer(db))

	var good models.Node
	db.First(&good, "id = ?", "good")
	res, _ := ops.SetMaintenance(&good, true, "tester")
	if len(res.Stays) != 1 || len(res.Moving) != 0 {
		t.Fatalf("with nowhere to go the app stays: %+v", res)
	}
	var n int64
	db.Model(&models.Job{}).Count(&n)
	if n != 0 {
		t.Errorf("no jobs expected, got %d", n)
	}
}

func TestMaintenance_NodeCanOnlyChangeItsOwn(t *testing.T) {
	_, _, db := newFailoverEnv(t)
	ops := NewNodeOpsService(db, NewDeployer(db))
	call := func(nodeID, fromIP string) int {
		c, w := newTestContext(http.MethodPost, "/nodes/"+nodeID+"/maintenance", `{"enabled":true}`)
		c.Params = gin.Params{{Key: "id", Value: nodeID}}
		c.Set("vpn_ip", fromIP)
		ops.SetMaintenanceFromNode(c)
		return w.Code
	}
	inMaintenance := func(id string) bool {
		var n models.Node
		db.First(&n, "id = ?", id)
		return n.Maintenance
	}

	// The node is whoever's address the request came from; the ID in the URL
	// plays no part. Node ok (10.0.0.4) naming "good" changes ok, never good.
	if code := call("good", "10.0.0.4"); code != http.StatusOK {
		t.Errorf("node ok asking, with good's ID in the URL: %d, want 200", code)
	}
	if inMaintenance("good") {
		t.Error("node ok put node good into maintenance")
	}
	if !inMaintenance("ok") {
		t.Error("the request came from node ok, so ok is the one that changes")
	}

	// Node good, asking for itself (by its ID, or "self" as new agents send).
	if code := call("self", "10.0.0.3"); code != http.StatusOK {
		t.Errorf("node good changing itself: %d, want 200", code)
	}
	if !inMaintenance("good") {
		t.Error("node good should be in maintenance after asking")
	}

	// An address that belongs to no node is refused.
	if code := call("good", "10.0.0.99"); code != http.StatusForbidden {
		t.Errorf("unknown address: %d, want 403", code)
	}
}

func TestParseWGDump(t *testing.T) {
	dump := "PRIVKEY\tPUBKEY\t51821\toff\n" +
		"peerA\t(none)\t1.2.3.4:5000\t10.101.0.3/32\t1790000000\t100\t200\t25\n" +
		"peerB\t(none)\t(none)\t10.101.0.4/32\t0\t0\t0\toff\n"
	got := parseWGDump([]byte(dump))
	if !got["10.101.0.3"].Equal(time.Unix(1790000000, 0)) {
		t.Errorf("handshake for 10.101.0.3 = %v", got["10.101.0.3"])
	}
	if _, ok := got["10.101.0.4"]; ok {
		t.Error("a peer that never completed a handshake has no time")
	}
}

func containerLogsRequest(t *testing.T, query string, caps []string) (*httptest.ResponseRecorder, *gorm.DB) {
	t.Helper()
	_, _, db := newFailoverEnv(t)
	// A struct update, because the JSON serializer of Capabilities doesn't run for a plain column update.
	db.Model(&models.Node{}).Where("id = ?", "good").Select("capabilities").Updates(&models.Node{Capabilities: caps})
	ops := NewNodeOpsService(db, NewDeployer(db))
	c, w := newTestContext(http.MethodPost, "/nodes/good/containers/api/logs"+query, "")
	c.Params = gin.Params{{Key: "id", Value: "good"}, {Key: "name", Value: "api"}}
	ops.ContainerLogs(c)
	return w, db
}

func jobsOn(db *gorm.DB, nodeID string) []models.Job {
	var jobs []models.Job
	db.Where("node_id = ?", nodeID).Find(&jobs)
	return jobs
}

func TestContainerLogs_SnapshotNeedsNoSpecialAgent(t *testing.T) {
	w, db := containerLogsRequest(t, "?lines=50", nil)
	jobs := jobsOn(db, "good")
	if w.Code != http.StatusAccepted || len(jobs) != 1 {
		t.Fatalf("status %d, %d jobs: %s", w.Code, len(jobs), w.Body.String())
	}
	if j := jobs[0]; j.Type != models.JobTypeCommand || j.Timeout != containerJobTimeout || strings.Contains(j.Command, "--follow") {
		t.Fatalf("a snapshot is a short command job without --follow: %+v", j)
	}
}

func TestContainerLogs_FollowIsAStreamJobForAnAgentThatSupportsIt(t *testing.T) {
	w, db := containerLogsRequest(t, "?lines=50&follow=1", []string{"bash", followLogsCapability})
	jobs := jobsOn(db, "good")
	if w.Code != http.StatusAccepted || len(jobs) != 1 {
		t.Fatalf("status %d, %d jobs: %s", w.Code, len(jobs), w.Body.String())
	}
	j := jobs[0]
	if j.Type != models.JobTypeLogsFollow || j.Timeout != followLogsTimeout || !strings.Contains(j.Command, "--follow --tail 50 --timestamps 'api'") {
		t.Fatalf("following runs docker logs --follow as a logs_follow job with a time limit: %+v", j)
	}
}

func TestContainerLogs_FollowIsRefusedForAnAgentThatWouldGetStuck(t *testing.T) {
	for _, caps := range [][]string{nil, {"bash", "docker"}} {
		w, db := containerLogsRequest(t, "?follow=1", caps)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "too old to follow logs") {
			t.Fatalf("want 409 for an agent without the capability, got %d: %s", w.Code, w.Body.String())
		}
		if n := len(jobsOn(db, "good")); n != 0 {
			t.Fatalf("no job may be queued for an agent that would get stuck on it, got %d", n)
		}
	}
}

func TestStaleInteractiveJobs_AreCancelledAfterAWhile(t *testing.T) {
	svc, db := newJobTestService(t)
	now := time.Now()
	old := now.Add(-time.Hour)
	mk := func(id, typ, cmd string, created time.Time) {
		db.Create(&models.Job{ID: id, NodeID: "n1", Type: typ, Status: models.JobStatusPending, Command: cmd, CreatedAt: created})
	}
	mk("follow-old", models.JobTypeLogsFollow, "docker logs --follow --tail 10 'api'", old)
	mk("oldstyle-follow", models.JobTypeCommand, "docker logs --follow --tail 10 --timestamps 'api' 2>&1", old)
	mk("restart-old", models.JobTypeCommand, "docker restart 'api'", old)
	mk("snapshot-old", models.JobTypeCommand, "docker logs --tail 10 --timestamps 'api' 2>&1", old)
	mk("follow-new", models.JobTypeLogsFollow, "docker logs --follow 'api'", now.Add(-time.Minute))
	mk("generic-old", models.JobTypeCommand, "./nightly-backup.sh", old)
	mk("deploy-old", models.JobTypeDeploy, "docker run something", old)

	svc.cancelStaleInteractiveJobs(now)

	status := func(id string) string {
		var j models.Job
		db.First(&j, "id = ?", id)
		return j.Status
	}
	for _, id := range []string{"follow-old", "oldstyle-follow", "restart-old", "snapshot-old"} {
		if status(id) != models.JobStatusCancelled {
			t.Errorf("%s waited an hour and should be cancelled, is %s", id, status(id))
		}
	}
	for _, id := range []string{"follow-new", "generic-old", "deploy-old"} {
		if status(id) != models.JobStatusPending {
			t.Errorf("%s must be left alone, is %s", id, status(id))
		}
	}
	var j models.Job
	db.First(&j, "id = ?", "follow-old")
	if j.CompletedAt == nil || !strings.Contains(j.Logs, "nobody is waiting") {
		t.Errorf("a cancelled job says why: %+v", j)
	}
}
