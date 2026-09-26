package services

import (
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

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
	if code := call("good", "10.0.0.4"); code != http.StatusForbidden {
		t.Errorf("node ok changing good: %d, want 403", code)
	}
	if code := call("good", "10.0.0.3"); code != http.StatusOK {
		t.Errorf("node good changing itself: %d, want 200", code)
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
