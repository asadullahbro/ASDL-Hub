package models

import "time"

type AllowedRepo struct {
	ID          string    `gorm:"primaryKey" json:"id"`
	Repository  string    `gorm:"uniqueIndex:idx_repo_env" json:"repository"`
	Environment string    `gorm:"size:50;uniqueIndex:idx_repo_env" json:"environment"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type GitHubToken struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	Label     string    `gorm:"size:255" json:"label"`
	Token     string    `gorm:"size:255" json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

type OIDCDeployment struct {
	ID          string    `gorm:"primaryKey" json:"id"`
	Repository  string    `json:"repository"`
	Environment string    `json:"environment"`
	ProjectID   string    `json:"project_id"`
	NodeID      string    `json:"node_id"`
	SHA         string    `json:"sha"`
	Ref         string    `json:"ref"`
	Workflow    string    `json:"workflow"`
	RunID       string    `json:"run_id"`
	Image       string    `json:"image"`
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type DeployRequest struct {
	OIDCToken   string `json:"oidc_token" binding:"required"`
	Image       string `json:"image" binding:"required"`
	Environment string `json:"environment"`
}

type DeployResponse struct {
	DeploymentID string `json:"deployment_id"`
	ProjectID    string `json:"project_id"`
	NodeID       string `json:"node_id"`
	Status       string `json:"status"`
}
