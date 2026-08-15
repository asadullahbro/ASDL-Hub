package models

import "time"

type RepoDeploymentRule struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	Repository  string    `json:"repository"`
	RepoOwner   string    `json:"repo_owner"`
	Environment string    `json:"environment"`
	ProjectID   string    `json:"project_id"`
	NodeID      string    `json:"node_id"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type OIDCDeployment struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	RuleID      string    `json:"rule_id"`
	Repository  string    `json:"repository"`
	Environment string    `json:"environment"`
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
	Project     string `json:"project" binding:"required"`
	Environment string `json:"environment" binding:"required"`
	Image       string `json:"image" binding:"required"`
}

type DeployResponse struct {
	DeploymentID string `json:"deployment_id"`
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
}
