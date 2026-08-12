package models

import (
	"gorm.io/gorm"
	"time"
)

type Deployment struct {
	ID         string `gorm:"primaryKey;size:36" json:"id"`
	Repository string `gorm:"size:255;not null" json:"repository"`
	Branch     string `gorm:"size:100;not null" json:"branch"`
	Commit     string `gorm:"size:40" json:"commit"`
	CommitMsg  string `gorm:"size:255" json:"commit_msg"`
	NodeID     string `gorm:"index;not null" json:"node_id"`
	Status     string `gorm:"size:20;default:pending" json:"status"`
	Type       string `gorm:"size:20;default:docker" json:"type"` // "docker" or "direct"

	// Docker specific
	Dockerfile    string   `gorm:"size:255" json:"dockerfile"`
	ImageName     string   `gorm:"size:255" json:"image_name"`
	ContainerName string   `gorm:"size:255" json:"container_name"`
	Ports         []string `gorm:"serializer:json" json:"ports"`
	Volumes       []string `gorm:"serializer:json" json:"volumes"`

	// Direct install specific
	BuildCommand string `gorm:"size:255" json:"build_command"`
	StartCommand string `gorm:"size:255" json:"start_command"`
	InstallCmd   string `gorm:"size:255" json:"install_command"`

	// Common
	EnvVars     []EnvVar       `gorm:"serializer:json" json:"env_vars"`
	Logs        string         `gorm:"type:text" json:"logs"`
	JobID       string         `gorm:"index" json:"job_id"`
	CreatedAt   time.Time      `json:"created_at"`
	StartedAt   *time.Time     `json:"started_at,omitempty"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

const (
	DeploymentStatusPending   = "pending"
	DeploymentStatusRunning   = "running"
	DeploymentStatusCompleted = "completed"
	DeploymentStatusFailed    = "failed"

	DeploymentTypeDocker = "docker"
	DeploymentTypeDirect = "direct"
	DeploymentTypeAuto   = "auto"
)
