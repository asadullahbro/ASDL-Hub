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
	AgentVersion  string         `gorm:"size:20" json:"agent_version"`
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
