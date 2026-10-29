package models

import "time"

// PluginSnapshot is a copy of one plugin's data folder, kept on the Hub so the
// data can be put back on another node after a failover or a move. The data
// can be private (an app's cache or counters), so it is never part of an API
// response; only the metadata is.
type PluginSnapshot struct {
	ID         string `gorm:"primaryKey;size:36" json:"id"`
	ProjectID  string `gorm:"size:36;not null;index:idx_snapshot_plugin,priority:1" json:"project_id"`
	PluginID   string `gorm:"size:40;not null;index:idx_snapshot_plugin,priority:2" json:"plugin_id"`
	NodeID     string `gorm:"size:36" json:"node_id"`
	Generation int64  `gorm:"not null;index:idx_snapshot_plugin,priority:3" json:"generation"`
	// Trigger is what made it: scheduled, move, manual or restore.
	Trigger   string    `gorm:"size:20;not null" json:"trigger"`
	JobID     string    `gorm:"size:36;index" json:"job_id"`
	SizeBytes int64     `json:"size_bytes"`
	SHA256    string    `gorm:"size:64" json:"sha256"`
	Data      []byte    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

const (
	SnapshotScheduled = "scheduled"
	SnapshotMove      = "move"
	SnapshotManual    = "manual"
	SnapshotRestore   = "restore"
)
