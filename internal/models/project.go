// internal/models/project.go
package models

import (
	"gorm.io/gorm"
	"time"
)

type Project struct {
	ID           string         `gorm:"primaryKey;size:36" json:"id"`
	Name         string         `gorm:"size:255;not null" json:"name"`
	Description  string         `gorm:"size:500" json:"description"`
	Domain       string         `gorm:"size:255" json:"domain"`
	Repository   string         `gorm:"size:255;uniqueIndex" json:"repository"`
	NodeID       string         `gorm:"index;not null" json:"node_id"`
	ContainerID  string         `gorm:"index" json:"container_id"`
	DeploymentID string         `gorm:"index" json:"deployment_id"`
	Status       string         `gorm:"size:20;default:running" json:"status"`
	HealthStatus string         `gorm:"size:20;default:unknown" json:"health_status"`
	Image        string         `gorm:"size:255" json:"image"`
	Ports        []string       `gorm:"serializer:json" json:"ports"`
	EnvVars      []EnvVar       `gorm:"serializer:json" json:"env_vars"`
	Volumes      []string       `gorm:"serializer:json" json:"volumes"`
	Uptime       int64          `json:"uptime"`
	LastDeployed time.Time      `json:"last_deployed"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Project) TableName() string {
	return "projects"
}
