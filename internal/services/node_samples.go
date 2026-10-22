package services

import (
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

const (
	// sampleEvery is the least time between two samples of a node. Agents beat every 30 seconds, and a graph
	// does not need more than a point a minute.
	sampleEvery = time.Minute
	// sampleKeep is how long samples are kept: 14 days of one a minute is about 20,000 small rows per node.
	sampleKeep = 14 * 24 * time.Hour
	// maxSamplePoints is the most points the API returns; longer ranges are averaged down to this.
	maxSamplePoints = 300
	maxSampleHours  = 14 * 24
)

var (
	sampleMu   sync.Mutex
	lastSample = map[string]time.Time{}
)

// sampleDue says whether a node's next sample is due, and if so counts it as taken at now.
func sampleDue(nodeID string, now time.Time) bool {
	sampleMu.Lock()
	defer sampleMu.Unlock()
	if last, ok := lastSample[nodeID]; ok && now.Sub(last) < sampleEvery {
		return false
	}
	lastSample[nodeID] = now
	return true
}

func percentOf(used, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

// recordSample stores a reading of the node, unless one was taken in the last minute. Like the other
// bookkeeping around heartbeats, a failure is logged and never reaches the node.
func recordSample(db *gorm.DB, node *models.Node, cpuPercent float64, now time.Time) {
	if !sampleDue(node.ID, now) {
		return
	}
	sample := models.NodeSample{
		NodeID: node.ID, At: now, CPUPercent: cpuPercent,
		MemoryPercent: percentOf(node.MemoryUsed, node.MemoryTotal), DiskPercent: percentOf(node.DiskUsed, node.DiskTotal),
		Load1: node.LoadAvg1, PingMs: node.PingLatency, WiFiSignal: node.WiFiSignal,
	}
	if err := db.Create(&sample).Error; err != nil {
		log.Printf("⚠️ Could not store a sample of %s: %v", node.Hostname, err)
	}
}

// trimSamples deletes samples older than sampleKeep and returns how many it removed.
func trimSamples(db *gorm.DB, now time.Time) int64 {
	res := db.Where("at < ?", now.Add(-sampleKeep)).Delete(&models.NodeSample{})
	if res.Error != nil {
		log.Printf("⚠️ Could not trim old samples: %v", res.Error)
		return 0
	}
	return res.RowsAffected
}

// downsample averages samples into at most max points, keeping the order. Few enough samples are returned as they are.
func downsample(in []models.NodeSample, max int) []models.NodeSample {
	if max < 1 || len(in) <= max {
		return in
	}
	out := make([]models.NodeSample, 0, max)
	for i := 0; i < max; i++ {
		lo, hi := i*len(in)/max, (i+1)*len(in)/max
		if lo == hi {
			continue
		}
		var a models.NodeSample
		for _, s := range in[lo:hi] {
			a.CPUPercent += s.CPUPercent
			a.MemoryPercent += s.MemoryPercent
			a.DiskPercent += s.DiskPercent
			a.Load1 += s.Load1
			a.PingMs += s.PingMs
			a.WiFiSignal += s.WiFiSignal
		}
		n := float64(hi - lo)
		out = append(out, models.NodeSample{
			At:         in[lo+(hi-lo)/2].At,
			CPUPercent: a.CPUPercent / n, MemoryPercent: a.MemoryPercent / n, DiskPercent: a.DiskPercent / n,
			Load1: a.Load1 / n, PingMs: a.PingMs / n, WiFiSignal: int(float64(a.WiFiSignal)/n + 0.5),
		})
	}
	return out
}

// Samples handles GET /nodes/:id/samples?hours=24: the node's readings over the last N hours, oldest first.
func (s *NodeService) Samples(c *gin.Context) {
	hours, err := strconv.Atoi(c.DefaultQuery("hours", "24"))
	if err != nil || hours < 1 {
		hours = 24
	}
	if hours > maxSampleHours {
		hours = maxSampleHours
	}
	var node models.Node
	if err := s.db.Select("id").First(&node, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}
	samples := []models.NodeSample{}
	s.db.Where("node_id = ? AND at >= ?", node.ID, time.Now().Add(-time.Duration(hours)*time.Hour)).Order("at asc").Find(&samples)
	c.JSON(http.StatusOK, gin.H{"hours": hours, "samples": downsample(samples, maxSamplePoints)})
}
