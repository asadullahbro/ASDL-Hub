package models

import (
	"gorm.io/gorm"
	"time"
)

type JobPayload struct {
	Image         string    `json:"image"`
	ContainerName string    `json:"container_name"`
	SourceNodeIP  string    `json:"source_node_ip"`
	Repository    string    `json:"repository"`
	Branch        string    `json:"branch"`
	BuildCommand  string    `json:"build_command"`
	StartCommand  string    `json:"start_command"`
	InstallCmd    string    `json:"install_cmd"`
	LastDeployed  time.Time `json:"last_deployed"`
	Ports         []string  `json:"ports"`
	Volumes       []string  `json:"volumes"`
	EnvVars       []EnvVar  `json:"env_vars"`
	Operation     string    `json:"operation"` // "stop", "start", "deploy", "migrate"
	MigrationID   string    `json:"migration_id"`
}

type Job struct {
	ID          string         `gorm:"primaryKey;size:36" json:"id"`
	NodeID      string         `gorm:"index;not null" json:"node_id"`
	Type        string         `gorm:"size:50;not null" json:"type"`
	Status      string         `gorm:"size:20;not null;default:pending" json:"status"`
	Command     string         `gorm:"type:text" json:"command"`
	Payload     *JobPayload    `gorm:"serializer:json" json:"payload,omitempty"`
	WorkingDir  string         `gorm:"size:255" json:"working_dir"`
	Environment []string       `gorm:"serializer:json" json:"environment"`
	Logs        string         `gorm:"type:text" json:"logs"`
	ExitCode    int            `json:"exit_code"`
	Retries     int            `json:"retries"`
	MaxRetries  int            `json:"max_retries"`
	Timeout     int            `json:"timeout"`
	CreatedAt   time.Time      `json:"created_at"`
	StartedAt   *time.Time     `json:"started_at,omitempty"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

const (
	JobStatusPending   = "pending"
	JobStatusRunning   = "running"
	JobStatusCompleted = "completed"
	JobStatusFailed    = "failed"
	JobStatusCancelled = "cancelled"
)

const (
	JobTypeDeploy        = "deploy"
	JobTypeRestart       = "restart"
	JobTypeCommand       = "command"
	JobTypeBackup        = "backup"
	JobTypeUpdate        = "update"
	JobTypeMigrateStart  = "migrate_start"
	JobTypeMigrateStop   = "migrate_stop"
	JobTypeFailoverStart = "failover_start"
	JobTypeFailoverStop  = "failover_stop"
	JobTypeImagePull     = "image_pull"
	JobTypeImagePush     = "image_push"
)
