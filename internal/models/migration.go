package models

import (
	"gorm.io/gorm"
	"time"
)

// internal/models/migration.go
type Migration struct {
	ID           string         `gorm:"primaryKey;size:36" json:"id"`
	ProjectID    string         `gorm:"index" json:"project_id"` // ADD THIS
	ContainerID  string         `gorm:"index;not null" json:"container_id"`
	SourceNodeID string         `gorm:"index;not null" json:"source_node_id"`
	TargetNodeID string         `gorm:"index;not null" json:"target_node_id"`
	Status       string         `gorm:"size:20;default:pending" json:"status"`
	JobID        string         `gorm:"index" json:"job_id"`
	Logs         string         `gorm:"type:text" json:"logs"`
	CreatedAt    time.Time      `json:"created_at"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

const (
	MigrationStatusPending   = "pending"
	MigrationStatusRunning   = "running"
	MigrationStatusCompleted = "completed"
	MigrationStatusFailed    = "failed"
)

func (Migration) TableName() string {
	return "migrations"
}
