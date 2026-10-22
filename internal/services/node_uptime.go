package services

import (
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

// settingTrackedSince holds when outage tracking began. Outages before that were never recorded, so uptime
// is only counted from then on (otherwise a node would look perfect for a period nobody measured).
const settingTrackedSince = "outage_tracking_since"

// outageTrackingSince returns when outage tracking began, recording now on the very first call.
func outageTrackingSince(db *gorm.DB, now time.Time) time.Time {
	var s models.Setting
	if err := db.First(&s, "key = ?", settingTrackedSince).Error; err == nil {
		if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
			return t
		}
	}
	db.Save(&models.Setting{Key: settingTrackedSince, Value: now.UTC().Format(time.RFC3339)})
	return now
}

// uptimePercent is the share of [from, now] a node was not in an outage, 0 to 100, to two decimals.
// An outage still open counts up to now; outages are clipped to the window.
func uptimePercent(outages []models.NodeOutage, from, now time.Time) float64 {
	total := now.Sub(from)
	if total <= 0 {
		return 100
	}
	var down time.Duration
	for _, o := range outages {
		start, end := o.StartedAt, now
		if o.EndedAt != nil {
			end = *o.EndedAt
		}
		if start.Before(from) {
			start = from
		}
		if end.After(now) {
			end = now
		}
		if end.After(start) {
			down += end.Sub(start)
		}
	}
	if down > total {
		down = total
	}
	return math.Round((1-float64(down)/float64(total))*10000) / 100
}

// NodeUptime is a node's availability, for the dashboard and the command line.
type NodeUptime struct {
	NodeID string `json:"node_id"`
	// Percentages over the last 7 and 30 days (or since tracking began, whichever is shorter); nil for a
	// node that has never sent a heartbeat, which has no uptime to speak of.
	Uptime7d   *float64           `json:"uptime_7d"`
	Uptime30d  *float64           `json:"uptime_30d"`
	Outages7d  int                `json:"outages_7d"`
	LastOutage *models.NodeOutage `json:"last_outage"`
}

func (s *NodeService) uptimeFor(node *models.Node, now, trackedSince time.Time) NodeUptime {
	return uptimeOf(s.db, node, now, trackedSince)
}

// uptimeOf is a node's availability, for callers that have a database but no NodeService (placement).
func uptimeOf(db *gorm.DB, node *models.Node, now, trackedSince time.Time) NodeUptime {
	u := NodeUptime{NodeID: node.ID}
	var outages []models.NodeOutage
	db.Where("node_id = ?", node.ID).Order("id DESC").Limit(500).Find(&outages)
	if len(outages) > 0 {
		last := outages[0]
		u.LastOutage = &last
	}
	if node.LastHeartbeat.IsZero() && len(outages) == 0 {
		return u
	}
	window := func(d time.Duration) time.Time {
		from := now.Add(-d)
		if trackedSince.After(from) {
			from = trackedSince
		}
		if node.CreatedAt.After(from) {
			from = node.CreatedAt
		}
		return from
	}
	p7, p30 := uptimePercent(outages, window(7*24*time.Hour), now), uptimePercent(outages, window(30*24*time.Hour), now)
	u.Uptime7d, u.Uptime30d = &p7, &p30
	weekAgo := now.Add(-7 * 24 * time.Hour)
	for _, o := range outages {
		if o.EndedAt == nil || o.EndedAt.After(weekAgo) {
			u.Outages7d++
		}
	}
	return u
}

// Uptime handles GET /nodes/uptime: every node's availability at a glance.
func (s *NodeService) Uptime(c *gin.Context) {
	now := time.Now()
	since := outageTrackingSince(s.db, now)
	var nodes []models.Node
	s.db.Order("online DESC, vpn_ip ASC").Find(&nodes)
	out := make([]NodeUptime, 0, len(nodes))
	for i := range nodes {
		out = append(out, s.uptimeFor(&nodes[i], now, since))
	}
	c.JSON(http.StatusOK, gin.H{"tracked_since": since, "nodes": out})
}

// recentOutages is a node's latest outages, newest first, for its detail page.
func (s *NodeService) recentOutages(nodeID string, limit int) []models.NodeOutage {
	out := []models.NodeOutage{}
	s.db.Where("node_id = ?", nodeID).Order("id DESC").Limit(limit).Find(&out)
	return out
}
