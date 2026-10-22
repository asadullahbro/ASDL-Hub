package services

import (
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

// None of this may break the flows it hooks into (registering, heartbeats, the offline sweeper): a failed
// write is logged and the caller carries on as before.

// openOutage records that a node went quiet, unless an outage for it is already open. The outage starts at
// the node's last heartbeat, the last moment it was known to be up.
func openOutage(db *gorm.DB, node *models.Node) {
	var open int64
	if err := db.Model(&models.NodeOutage{}).Where("node_id = ? AND ended_at IS NULL", node.ID).Count(&open).Error; err != nil {
		log.Printf("⚠️ Could not check outages for %s: %v", node.Hostname, err)
		return
	}
	if open > 0 {
		return
	}
	o := models.NodeOutage{NodeID: node.ID, StartedAt: node.LastHeartbeat, WiFiAtStart: node.WiFiSignal, PingAtStart: node.PingLatency}
	if err := db.Create(&o).Error; err != nil {
		log.Printf("⚠️ Could not record the outage of %s: %v", node.Hostname, err)
	}
}

// Thresholds shared with the node health score: under 50 is a weak WiFi signal, over 100 ms a slow link.
const (
	weakWiFi        = 50
	slowPingMs      = 100
	rebootGraceSecs = 30
)

// outageCause guesses why a node went quiet. uptime is the machine's uptime (seconds) in the first heartbeat
// after the outage, or negative when unknown: a machine whose uptime is shorter than the outage was restarted.
func outageCause(o models.NodeOutage, seconds, uptime int64) string {
	if uptime < 0 {
		return ""
	}
	if uptime+rebootGraceSecs < seconds {
		return models.OutageCauseReboot
	}
	if (o.WiFiAtStart > 0 && o.WiFiAtStart < weakWiFi) || o.PingAtStart > slowPingMs {
		return models.OutageCauseWeakLink
	}
	return models.OutageCauseConnection
}

// closeOutage records that a node is sending heartbeats again, ending any outage still open for it. uptime is
// the node's machine uptime in seconds from that heartbeat (negative when not known, which leaves the cause empty).
func closeOutage(db *gorm.DB, nodeID string, now time.Time, uptime int64) {
	var open []models.NodeOutage
	if err := db.Where("node_id = ? AND ended_at IS NULL", nodeID).Find(&open).Error; err != nil {
		log.Printf("⚠️ Could not look up the open outage of %s: %v", nodeID, err)
		return
	}
	for _, o := range open {
		secs := int64(now.Sub(o.StartedAt).Seconds())
		if secs < 0 {
			secs = 0
		}
		if err := db.Model(&models.NodeOutage{}).Where("id = ?", o.ID).Updates(map[string]interface{}{"ended_at": now, "seconds": secs, "cause": outageCause(o, secs, uptime)}).Error; err != nil {
			log.Printf("⚠️ Could not close the outage of %s: %v", nodeID, err)
		}
	}
}

// closeOutagesOfOnlineNodes is the safety net for every way a node can come back (a heartbeat, a registration,
// anything added later): an outage still open for a node that is online has ended, at the node's last heartbeat.
func closeOutagesOfOnlineNodes(db *gorm.DB) {
	var open []models.NodeOutage
	if err := db.Where("ended_at IS NULL").Find(&open).Error; err != nil || len(open) == 0 {
		return
	}
	for _, o := range open {
		var node models.Node
		if err := db.First(&node, "id = ?", o.NodeID).Error; err != nil || !node.Online {
			continue
		}
		end := node.LastHeartbeat
		if end.Before(o.StartedAt) {
			end = o.StartedAt
		}
		closeOutage(db, o.NodeID, end, -1)
	}
}

// openOutagesForOfflineNodes covers nodes that were already offline when this feature (or the Hub) started:
// without it their outage would never be recorded, because the sweeper only sees nodes going offline.
func openOutagesForOfflineNodes(db *gorm.DB) {
	outageTrackingSince(db, time.Now()) // starts the uptime clock the first time this runs
	var nodes []models.Node
	if err := db.Where("online = ?", false).Find(&nodes).Error; err != nil {
		return
	}
	for i := range nodes {
		if !nodes[i].LastHeartbeat.IsZero() {
			openOutage(db, &nodes[i])
		}
	}
}

// recordHeartbeat keeps one heartbeat row per node, always the latest: it updates the node's row in place and
// only creates one for a node that has none. History is not kept (outages are, in NodeOutage).
func recordHeartbeat(db *gorm.DB, hb *models.Heartbeat) {
	res := db.Model(&models.Heartbeat{}).Where("node_id = ?", hb.NodeID).
		Select("CPUPercent", "MemoryUsed", "MemoryTotal", "DiskUsed", "DiskTotal", "LoadAvg1", "LoadAvg5", "LoadAvg15", "Uptime", "Timestamp").
		Updates(hb)
	if res.Error != nil {
		log.Printf("⚠️ Could not update the heartbeat of %s: %v", hb.NodeID, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		if err := db.Create(hb).Error; err != nil {
			log.Printf("⚠️ Could not store the heartbeat of %s: %v", hb.NodeID, err)
		}
	}
}

// vacuumAfter is how many deleted rows make shrinking the table worthwhile (PostgreSQL only gives the disk
// back to the system on VACUUM FULL; plain deletes just free space inside the file).
const vacuumAfter = 10000

// trimHeartbeats deletes every heartbeat except the newest one of each node and returns how many it removed.
// Normally there is nothing to delete; it clears the history older Hubs piled up, and any duplicate a race
// between two simultaneous first heartbeats could leave.
func trimHeartbeats(db *gorm.DB) int64 {
	res := db.Exec("DELETE FROM heartbeats WHERE id NOT IN (SELECT MAX(id) FROM heartbeats GROUP BY node_id)")
	if res.Error != nil {
		log.Printf("⚠️ Could not trim old heartbeats: %v", res.Error)
		return 0
	}
	if res.RowsAffected >= vacuumAfter && db.Dialector.Name() == "postgres" {
		if err := db.Exec("VACUUM FULL heartbeats").Error; err != nil {
			log.Printf("⚠️ Trimmed %d old heartbeats but could not shrink the table: %v", res.RowsAffected, err)
		}
	}
	if res.RowsAffected > 0 {
		log.Printf("🧹 Removed %d old heartbeat rows (only each node's latest is kept)", res.RowsAffected)
	}
	return res.RowsAffected
}

// StartHeartbeatTrimmer trims shortly after the Hub starts, then hourly.
func (s *NodeService) StartHeartbeatTrimmer() {
	go func() {
		time.Sleep(20 * time.Second)
		for {
			trimHeartbeats(s.db)
			time.Sleep(time.Hour)
		}
	}()
}
