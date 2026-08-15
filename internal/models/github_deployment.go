package models

import "time"

type OIDCDeployment struct {
	ID          string    `json:"id" gorm:"primaryKey"`
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
