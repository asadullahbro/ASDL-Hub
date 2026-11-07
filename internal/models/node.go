package models

import (
	"gorm.io/gorm"
	"time"
)

type Node struct {
	ID            string         `gorm:"primaryKey;size:36" json:"id"`
	Hostname      string         `gorm:"size:255;not null" json:"hostname"`
	VPNIP         string         `gorm:"size:45;uniqueIndex;not null" json:"vpn_ip"`
	OS            string         `gorm:"size:50" json:"os"`
	Architecture  string         `gorm:"size:20" json:"architecture"`
	CPU           int            `json:"cpu_cores"`
	MemoryTotal   int64          `json:"memory_total"`
	MemoryUsed    int64          `json:"memory_used"` // ADD THIS
	DiskTotal     int64          `json:"disk_total"`
	DiskUsed      int64          `json:"disk_used"` // ADD THIS
	Online        bool           `json:"online"`
	LastHeartbeat time.Time      `json:"last_heartbeat"`
	Capabilities  []string       `gorm:"serializer:json" json:"capabilities"`
	Uptime        int64          `json:"uptime"`
	Status        string         `gorm:"size:20;default:healthy" json:"status"` // ADD THIS
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
	HealthScore   int            `json:"health_score"`
	HealthDetails string         `json:"health_details"` // JSON string with breakdown
	LastCheck     time.Time      `json:"last_check"`
	FailureCount  int            `json:"failure_count"`
	MaxFailures   int            `json:"max_failures"`
	PingLatency   float64        `json:"ping_latency"` // ms
	WiFiSignal    int            `json:"wifi_signal"`  // 0-100
	LoadAvg1      float64        `json:"load_avg_1"`
	LoadAvg5      float64        `json:"load_avg_5"`
	LoadAvg15     float64        `json:"load_avg_15"`
	AgentVersion  string         `gorm:"size:40" json:"agent_version"`
	// Maintenance nodes get no new apps and are never failover targets;
	// their apps are moved elsewhere when maintenance starts.
	Maintenance      bool       `gorm:"not null;default:false" json:"maintenance"`
	MaintenanceSince *time.Time `json:"maintenance_since,omitempty"`
	MaintenanceBy    string     `gorm:"size:100" json:"maintenance_by,omitempty"`
	// Containers is what the agent last reported running on the node.
	Containers []NodeContainer `gorm:"serializer:json" json:"containers"`
	// ParallelJobs is how many jobs the node may run at once; 1 runs them one after another. It is set from the
	// node's hardware when it enrolls (see RecommendedParallelJobs) and can be changed later. 0: not set yet.
	ParallelJobs int `gorm:"not null;default:0" json:"parallel_jobs"`
	// RecommendedParallelJobs is what the node's hardware suggests, for the dashboard. Not stored.
	RecommendedParallelJobs int `gorm:"-" json:"recommended_parallel_jobs"`
	// AgentStartedAt is when the node's agent last registered (it does so when it starts). Jobs still marked
	// running from before then belong to an agent process that is gone, so they don't hold up the queue.
	AgentStartedAt *time.Time `json:"agent_started_at,omitempty"`
	// GoingAwayAt is when the node's agent last said it was stopping on purpose; cleared when it is back.
	GoingAwayAt *time.Time `json:"going_away_at,omitempty"`
}

// ParallelJobsCapability is what an agent reports when it can run several jobs at once.
const ParallelJobsCapability = "parallel_jobs"

// MaxParallelJobs is the most jobs a node may be set to run at once.
const MaxParallelJobs = 8

// RecommendedParallelJobs is how many jobs a node with these resources should run at once: one per 4 GB of RAM
// and per 2 CPU cores (a deploy pulls and unpacks an image and starts a container, which is mostly memory and
// disk), at least 1 and at most 4. Unknown resources don't limit it.
func RecommendedParallelJobs(cpus int, memoryTotal int64) int {
	const gib = 1 << 30
	n := 4
	if memoryTotal > 0 {
		n = min(n, int(memoryTotal/(4*gib)))
	}
	if cpus > 0 {
		n = min(n, cpus/2)
	}
	return max(n, 1)
}

func (n *Node) AfterFind(*gorm.DB) error {
	n.RecommendedParallelJobs = RecommendedParallelJobs(n.CPU, n.MemoryTotal)
	return nil
}

// SupportsParallelJobs reports whether the node's agent can run several jobs at once.
func (n *Node) SupportsParallelJobs() bool {
	for _, c := range n.Capabilities {
		if c == ParallelJobsCapability {
			return true
		}
	}
	return false
}

// JobLimit is how many jobs the node may run at once right now: its setting (or the recommendation while it has
// none), and 1 when its agent can only run one.
func (n *Node) JobLimit() int {
	if !n.SupportsParallelJobs() {
		return 1
	}
	if n.ParallelJobs > 0 {
		return n.ParallelJobs
	}
	return RecommendedParallelJobs(n.CPU, n.MemoryTotal)
}

// NodeSample is one reading of a node's load, for the graphs on its page. Unlike Heartbeat (only the latest per
// node is kept) these build up, so they are taken at most once a minute and dropped after two weeks.
type NodeSample struct {
	ID            uint      `gorm:"primaryKey" json:"-"`
	NodeID        string    `gorm:"index:idx_node_samples_node_at,priority:1;not null;size:36" json:"-"`
	At            time.Time `gorm:"index:idx_node_samples_node_at,priority:2;index" json:"at"`
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryPercent float64   `json:"memory_percent"`
	DiskPercent   float64   `json:"disk_percent"`
	Load1         float64   `json:"load_1"`
	PingMs        float64   `json:"ping_ms"`
	WiFiSignal    int       `json:"wifi_signal"`
}

// Why a node went quiet, as guessed when it came back (NodeOutage.Cause).
const (
	OutageCauseReboot     = "reboot"
	OutageCauseWeakLink   = "weak_link"
	OutageCauseConnection = "connection"
)

// NodeOutage is one stretch during which a node sent no heartbeats: how long it was gone, kept for good
// (a few rows a week), unlike heartbeats, of which only the latest per node is kept.
type NodeOutage struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	NodeID string `gorm:"index;not null;size:36" json:"node_id"`
	// StartedAt is the node's last heartbeat before it went quiet.
	StartedAt time.Time `json:"started_at"`
	// EndedAt is when its first heartbeat came back; nil while it is still offline.
	EndedAt *time.Time `json:"ended_at"`
	// Seconds is the length of the outage, filled in when it ends.
	Seconds int64 `json:"seconds"`
	// Cause is a best guess made when the node returns: "reboot" (the machine restarted: power or a crash),
	// "weak_link" (it stayed up, and its WiFi or ping was poor just before it went quiet) or "connection"
	// (it stayed up and looked fine, so the network or the agent dropped). Empty when it could not be told.
	Cause string `gorm:"size:20" json:"cause"`
	// WiFiAtStart and PingAtStart are the node's last reported WiFi signal (0-100) and ping (ms) before it went quiet.
	WiFiAtStart int     `json:"wifi_at_start"`
	PingAtStart float64 `json:"ping_at_start"`
	// Planned is set when the node's agent said it was going away just before it went quiet (someone stopped it,
	// or the machine was shut down or rebooted cleanly). Planned outages don't count as the node being unreliable
	// when the Hub picks where an app goes.
	Planned bool `gorm:"not null;default:false" json:"planned"`
}

// NodeContainer is one container as the node's agent reports it.
type NodeContainer struct {
	Name      string `json:"name"`
	Image     string `json:"image"`
	State     string `json:"state"`  // running, exited, restarting, ...
	Status    string `json:"status"` // Docker's text, e.g. "Up 3 hours"
	Ports     string `json:"ports"`
	Managed   bool   `json:"managed"` // started by the Hub
	ProjectID string `json:"project_id,omitempty"`
}

// Available limits a node query to nodes that can take apps: online and not
// in maintenance.
func Available(db *gorm.DB) *gorm.DB {
	return db.Where("online = ? AND maintenance = ?", true, false)
}

type Heartbeat struct {
	ID          uint      `gorm:"primaryKey" json:"-"`
	NodeID      string    `gorm:"index;not null" json:"node_id"`
	CPUPercent  float64   `json:"cpu_percent"`
	MemoryUsed  int64     `json:"memory_used"`
	MemoryTotal int64     `json:"memory_total"`
	DiskUsed    int64     `json:"disk_used"`
	DiskTotal   int64     `json:"disk_total"`
	LoadAvg1    float64   `json:"load_avg_1"`
	LoadAvg5    float64   `json:"load_avg_5"`
	LoadAvg15   float64   `json:"load_avg_15"`
	Uptime      int64     `json:"uptime"`
	Timestamp   time.Time `json:"timestamp"`
}
