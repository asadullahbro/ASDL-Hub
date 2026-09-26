// internal/models/project.go
package models

import (
	"gorm.io/gorm"
	"time"
)

type Project struct {
	ID           string   `gorm:"primaryKey;size:36" json:"id"`
	Name         string   `gorm:"size:255;not null" json:"name"`
	Description  string   `gorm:"size:500" json:"description"`
	Domain       string   `gorm:"size:255" json:"domain"`
	Repository   string   `gorm:"size:255;uniqueIndex" json:"repository"`
	NodeID       string   `gorm:"index;not null" json:"node_id"`
	ContainerID  string   `gorm:"index" json:"container_id"`
	DeploymentID string   `gorm:"index" json:"deployment_id"`
	Status       string   `gorm:"size:20;default:running" json:"status"`
	HealthStatus string   `gorm:"size:20;default:unknown" json:"health_status"`
	Image        string   `gorm:"size:255" json:"image"`
	Ports        []string `gorm:"serializer:json" json:"ports"`
	// AutoPort means the hub picks the node port; Ports then holds the
	// mapping it last chose. Projects without explicit ports are auto.
	AutoPort     bool           `gorm:"default:false" json:"auto_port"`
	EnvVars      SecretEnvVars  `gorm:"type:text" json:"env_vars"`
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

// DefaultPortMapping is used when a project has no ports configured: the
// container's port 8000 is published on the node's port 8000.
const DefaultPortMapping = "8000:8000"

// PortMappings returns the project's ports, or the default mapping when none
// are configured, so deploys, health checks and nginx all agree on one port.
func (p *Project) PortMappings() []string {
	if len(p.Ports) == 0 {
		return []string{DefaultPortMapping}
	}
	return p.Ports
}

// HostPort is the node port that traffic for the project is sent to: the host
// side of the first port mapping.
func (p *Project) HostPort() string {
	host, _, err := ParsePortMapping(p.PortMappings()[0])
	if err != nil {
		host, _, _ = ParsePortMapping(DefaultPortMapping)
	}
	return host
}
