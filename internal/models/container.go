package models

import (
    "time"
    "gorm.io/gorm"
)

type Container struct {
    ID           string         `gorm:"primaryKey;size:36" json:"id"`
    NodeID       string         `gorm:"index;not null" json:"node_id"`
    Name         string         `gorm:"size:255;not null" json:"name"`
    Image        string         `gorm:"size:255;not null" json:"image"`
    Status       string         `gorm:"size:50;not null" json:"status"` // running, stopped, exited, paused
    Ports        []string       `gorm:"serializer:json" json:"ports"`
    EnvVars      []EnvVar       `gorm:"serializer:json" json:"env_vars"`
    Volumes      []string       `gorm:"serializer:json" json:"volumes"`
    DeploymentID string         `gorm:"index" json:"deployment_id"`
    Domain       string         `gorm:"size:255" json:"domain"` // NEW: domain name for routing
    CreatedAt    time.Time      `json:"created_at"`
    UpdatedAt    time.Time      `json:"updated_at"`
    DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Container) TableName() string {
    return "containers"
}