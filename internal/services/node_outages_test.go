package services

import (
	"testing"
	"time"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

func heartbeats(t *testing.T, svc *NodeService) []models.Heartbeat {
	t.Helper()
	var rows []models.Heartbeat
	svc.db.Order("node_id, id").Find(&rows)
	return rows
}

func TestRecordHeartbeat_KeepsOneLatestRowPerNode(t *testing.T) {
	svc, db := newNodeTestService(t)
	t0 := time.Now().Add(-time.Minute)

	recordHeartbeat(db, &models.Heartbeat{NodeID: "a", CPUPercent: 80, MemoryUsed: 5, Timestamp: t0})
	recordHeartbeat(db, &models.Heartbeat{NodeID: "b", CPUPercent: 10, Timestamp: t0})
	// A later beat with zero values must overwrite, not be skipped as "unset"
	recordHeartbeat(db, &models.Heartbeat{NodeID: "a", CPUPercent: 0, MemoryUsed: 0, Timestamp: t0.Add(30 * time.Second)})

	rows := heartbeats(t, svc)
	if len(rows) != 2 {
		t.Fatalf("want one row per node (2), got %d", len(rows))
	}
	a := rows[0]
	if a.NodeID != "a" || a.CPUPercent != 0 || a.MemoryUsed != 0 || !a.Timestamp.After(t0) {
		t.Fatalf("node a's row was not updated in place to the latest values: %+v", a)
	}
	if rows[1].NodeID != "b" || rows[1].CPUPercent != 10 {
		t.Fatalf("node b's row changed: %+v", rows[1])
	}
}

func TestTrimHeartbeats_KeepsOnlyTheNewestPerNode(t *testing.T) {
	svc, db := newNodeTestService(t)
	for i := 0; i < 5; i++ {
		db.Create(&models.Heartbeat{NodeID: "a", CPUPercent: float64(i)})
	}
	for i := 0; i < 3; i++ {
		db.Create(&models.Heartbeat{NodeID: "b", CPUPercent: float64(i)})
	}

	if got := trimHeartbeats(db); got != 6 {
		t.Fatalf("removed %d rows, want 6", got)
	}
	rows := heartbeats(t, svc)
	if len(rows) != 2 || rows[0].CPUPercent != 4 || rows[1].CPUPercent != 2 {
		t.Fatalf("did not keep the newest row of each node: %+v", rows)
	}
	if trimHeartbeats(db) != 0 {
		t.Fatal("a second trim must find nothing to remove")
	}
}

func TestTrimHeartbeats_EmptyTableIsFine(t *testing.T) {
	_, db := newNodeTestService(t)
	if trimHeartbeats(db) != 0 {
		t.Fatal("nothing to trim")
	}
}

func TestOutage_OpenedByTheSweeperAndClosedOnReturn(t *testing.T) {
	svc, db := newNodeTestService(t)
	start := time.Now().Add(-time.Hour)
	now := time.Now()
	last := now.Add(-5 * time.Minute)
	db.Create(&models.Node{ID: "n1", Hostname: "laptop", VPNIP: "10.100.0.2", Online: true, LastHeartbeat: last})

	svc.sweepOffline(now, start)
	svc.sweepOffline(now.Add(15*time.Second), start) // a second sweep must not open a second outage

	var outages []models.NodeOutage
	db.Find(&outages)
	if len(outages) != 1 {
		t.Fatalf("want exactly one open outage, got %d", len(outages))
	}
	o := outages[0]
	if o.NodeID != "n1" || o.EndedAt != nil || !o.StartedAt.Equal(last) {
		t.Fatalf("the outage should start at the last heartbeat and be open: %+v", o)
	}

	back := now.Add(2 * time.Minute)
	closeOutage(db, "n1", back, -1)
	db.Find(&outages)
	if outages[0].EndedAt == nil || outages[0].Seconds != int64(back.Sub(last).Seconds()) {
		t.Fatalf("the outage should end at the return and record its length: %+v", outages[0])
	}
	closeOutage(db, "n1", back.Add(time.Hour), -1) // closing again changes nothing
	db.Find(&outages)
	if outages[0].Seconds != int64(back.Sub(last).Seconds()) {
		t.Fatal("an already closed outage was changed")
	}
}

func TestOutage_OnlyOpenedForNodesThatWereOfflineAtStart(t *testing.T) {
	_, db := newNodeTestService(t)
	gone := time.Now().Add(-48 * time.Hour)
	db.Create(&models.Node{ID: "off", Hostname: "off", VPNIP: "10.100.0.3", Online: false, LastHeartbeat: gone})
	db.Create(&models.Node{ID: "never", Hostname: "never", VPNIP: "10.100.0.4", Online: false}) // never reported
	db.Create(&models.Node{ID: "up", Hostname: "up", VPNIP: "10.100.0.5", Online: true, LastHeartbeat: time.Now()})

	openOutagesForOfflineNodes(db)
	openOutagesForOfflineNodes(db) // restarting again must not duplicate

	var outages []models.NodeOutage
	db.Find(&outages)
	if len(outages) != 1 || outages[0].NodeID != "off" || !outages[0].StartedAt.Equal(gone) {
		t.Fatalf("want one open outage for the node that was offline, got %+v", outages)
	}
}

// The new tables must never get in the way of the flows they hook into.
func TestOutageAndHeartbeatWrites_NeverBreakTheCaller(t *testing.T) {
	db := testutil.NewDB(t, &models.Node{}) // no heartbeats or node_outages tables at all
	svc := NewNodeService(db)
	start := time.Now().Add(-time.Hour)
	now := time.Now()
	db.Create(&models.Node{ID: "n1", Hostname: "a", VPNIP: "10.100.0.2", Online: true, LastHeartbeat: now.Add(-5 * time.Minute)})

	svc.sweepOffline(now, start) // must still mark the node offline
	var n models.Node
	db.First(&n, "id = ?", "n1")
	if n.Online {
		t.Fatal("the sweeper stopped working when the outage table was unavailable")
	}
	recordHeartbeat(db, &models.Heartbeat{NodeID: "n1"}) // must not panic
	closeOutage(db, "n1", now, -1)                       // must not panic
	trimHeartbeats(db)                                   // must not panic
}

func TestOutageCause(t *testing.T) {
	steady := models.NodeOutage{WiFiAtStart: 80, PingAtStart: 20}
	weakWiFiOutage := models.NodeOutage{WiFiAtStart: 30, PingAtStart: 20}
	slowOutage := models.NodeOutage{WiFiAtStart: 0, PingAtStart: 250}
	cases := []struct {
		name    string
		o       models.NodeOutage
		seconds int64
		uptime  int64
		want    string
	}{
		{"machine uptime shorter than the outage means it restarted", steady, 3600, 120, models.OutageCauseReboot},
		{"restart is caught even when WiFi was weak", weakWiFiOutage, 3600, 120, models.OutageCauseReboot},
		{"stayed up, weak WiFi before it dropped", weakWiFiOutage, 150, 90000, models.OutageCauseWeakLink},
		{"stayed up, slow ping before it dropped", slowOutage, 150, 90000, models.OutageCauseWeakLink},
		{"stayed up and looked fine", steady, 150, 90000, models.OutageCauseConnection},
		{"wired node has no WiFi reading (0), which is not weak", models.NodeOutage{PingAtStart: 5}, 150, 90000, models.OutageCauseConnection},
		{"uptime just over the outage is not a restart", steady, 150, 160, models.OutageCauseConnection},
		{"unknown uptime leaves the cause empty", steady, 150, -1, ""},
	}
	for _, tc := range cases {
		if got := outageCause(tc.o, tc.seconds, tc.uptime); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestOpenAndCloseOutage_RecordsLinkAndCause(t *testing.T) {
	_, db := newNodeTestService(t)
	gone := time.Now().Add(-10 * time.Minute)
	node := &models.Node{ID: "n1", Hostname: "laptop", Online: false, LastHeartbeat: gone, WiFiSignal: 35, PingLatency: 12}

	openOutage(db, node)
	closeOutage(db, "n1", gone.Add(3*time.Minute), 86400)

	var o models.NodeOutage
	if err := db.First(&o, "node_id = ?", "n1").Error; err != nil {
		t.Fatal(err)
	}
	if o.WiFiAtStart != 35 || o.PingAtStart != 12 {
		t.Fatalf("did not keep the link readings from before the outage: %+v", o)
	}
	if o.Cause != models.OutageCauseWeakLink || o.Seconds != 180 {
		t.Fatalf("want a 180s weak_link outage, got %+v", o)
	}
}
