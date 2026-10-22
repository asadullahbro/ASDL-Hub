package services

import (
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

// A node needs this much memory and disk free (share of its total) to take another app comfortably. Nodes
// short of either are only used when no node has enough.
const (
	minFreeMemoryShare = 0.10
	minFreeDiskShare   = 0.05
	// A node that went quiet in the last day scores this much lower for each outage: it is flapping.
	recentOutagePenalty = 15.0
)

// placementCandidate is what the ranking knows about a node that could take an app.
type placementCandidate struct {
	Node         models.Node
	Uptime7d     float64 // 0-100; negative when the node has no uptime record yet
	OutagesToday int     // outages that began in the last 24 hours
	IsMaster     bool
}

// score is the node's health score blended with how reliably it has actually stayed up, minus a penalty for
// each outage in the last day. Downtime counts heavily (each 1% down costs 3 points of the reliability score),
// because a node that looks healthy right now but was gone for a day last week is the one that fails you again.
// A node with no uptime record is judged on its health score alone.
func (c placementCandidate) score() float64 {
	s := float64(c.Node.HealthScore)
	if c.Uptime7d >= 0 {
		reliability := math.Max(0, 100-(100-c.Uptime7d)*3)
		s = s*0.4 + reliability*0.6
	}
	return s - recentOutagePenalty*float64(c.OutagesToday)
}

// hasHeadroom is true when the node has room to spare, or when it has not reported its totals (so it cannot be judged).
func (c placementCandidate) hasHeadroom() bool {
	n := c.Node
	if n.MemoryTotal > 0 && float64(n.MemoryTotal-n.MemoryUsed)/float64(n.MemoryTotal) < minFreeMemoryShare {
		return false
	}
	if n.DiskTotal > 0 && float64(n.DiskTotal-n.DiskUsed)/float64(n.DiskTotal) < minFreeDiskShare {
		return false
	}
	return true
}

// rankPlacements orders candidates best first: nodes with headroom ahead of nodes without, the master node
// ahead of the others within each group (it is where the Hub keeps its own things), then by score, then by name
// so the order never depends on how the database happened to return them.
func rankPlacements(cands []placementCandidate) []placementCandidate {
	out := append([]placementCandidate(nil), cands...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.hasHeadroom() != b.hasHeadroom() {
			return a.hasHeadroom()
		}
		if a.IsMaster != b.IsMaster {
			return a.IsMaster
		}
		if sa, sb := a.score(), b.score(); sa != sb {
			return sa > sb
		}
		return a.Node.Hostname < b.Node.Hostname
	})
	return out
}

// placementCandidates gathers the nodes that could take the project (online, not in maintenance, not the one it
// is on, and not one a failover to just failed on), with the facts the ranking needs.
func placementCandidates(db *gorm.DB, project *models.Project, excluded []string, now time.Time) []placementCandidate {
	q := db.Scopes(models.Available).Where("id != ?", project.NodeID)
	if len(excluded) > 0 {
		q = q.Where("id NOT IN ?", excluded)
	}
	var nodes []models.Node
	q.Find(&nodes)

	var master models.Setting
	hasMaster := db.First(&master, "key = ?", "master_node_id").Error == nil
	since := outageTrackingSince(db, now)
	dayAgo := now.Add(-24 * time.Hour)

	cands := make([]placementCandidate, 0, len(nodes))
	for i := range nodes {
		c := placementCandidate{Node: nodes[i], Uptime7d: -1, IsMaster: hasMaster && nodes[i].ID == master.Value}
		if u := uptimeOf(db, &nodes[i], now, since); u.Uptime7d != nil {
			c.Uptime7d = *u.Uptime7d
		}
		var today int64
		db.Model(&models.NodeOutage{}).Where("node_id = ? AND (ended_at IS NULL OR ended_at > ?)", nodes[i].ID, dayAgo).Count(&today)
		c.OutagesToday = int(today)
		cands = append(cands, c)
	}
	return cands
}

// PlacementOption is one node as GET /projects/:id/placement describes it.
type PlacementOption struct {
	NodeID       string   `json:"node_id"`
	Hostname     string   `json:"hostname"`
	Score        float64  `json:"score"`
	HealthScore  int      `json:"health_score"`
	Uptime7d     *float64 `json:"uptime_7d"`
	OutagesToday int      `json:"outages_24h"`
	FreeMemory   *int     `json:"free_memory_percent"`
	FreeDisk     *int     `json:"free_disk_percent"`
	Headroom     bool     `json:"headroom"`
	Master       bool     `json:"master"`
	Recommended  bool     `json:"recommended"`
}

func freePercent(total, used int64) *int {
	if total <= 0 {
		return nil
	}
	p := int(float64(total-used) / float64(total) * 100)
	return &p
}

// GetPlacement handles GET /projects/:id/placement: the nodes the project could move to, best first.
func (s *HealthService) GetPlacement(c *gin.Context) {
	var project models.Project
	if err := s.db.First(&project, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	ranked := rankPlacements(placementCandidates(s.db, &project, nil, time.Now()))
	options := make([]PlacementOption, 0, len(ranked))
	for i, r := range ranked {
		o := PlacementOption{
			NodeID: r.Node.ID, Hostname: r.Node.Hostname, Score: r.score(), HealthScore: r.Node.HealthScore,
			OutagesToday: r.OutagesToday, FreeMemory: freePercent(r.Node.MemoryTotal, r.Node.MemoryUsed),
			FreeDisk: freePercent(r.Node.DiskTotal, r.Node.DiskUsed), Headroom: r.hasHeadroom(),
			Master: r.IsMaster, Recommended: i == 0,
		}
		if r.Uptime7d >= 0 {
			u := r.Uptime7d
			o.Uptime7d = &u
		}
		options = append(options, o)
	}
	c.JSON(http.StatusOK, gin.H{"project_id": project.ID, "node_id": project.NodeID, "options": options})
}
