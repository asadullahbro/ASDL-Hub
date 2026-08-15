package models

import (
	"time"

	"gorm.io/gorm"
)

// GitHubApp holds the GitHub App credentials stored in the hub.
// Only one app config exists at a time — stored via the settings system.
// Keys: "github_app_id", "github_app_private_key", "github_webhook_secret"

// GitHubInstallation represents a GitHub App installation on a user or org account.
// Created when the user completes the GitHub App install flow and hits our callback.
type GitHubInstallation struct {
	ID             string         `gorm:"primaryKey;size:36" json:"id"`
	InstallationID int64          `gorm:"uniqueIndex;not null" json:"installation_id"` // GitHub's installation ID
	AccountLogin   string         `gorm:"size:255;not null" json:"account_login"`      // github username or org
	AccountType    string         `gorm:"size:50;not null" json:"account_type"`        // "User" or "Organization"
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

// GitHubRepository represents a GitHub repo linked to an ASDL Hub project.
// Stores only what the hub actually needs — no redundant GitHub metadata.
type GitHubRepository struct {
	ID             string         `gorm:"primaryKey;size:36" json:"id"`
	InstallationID int64          `gorm:"index;not null" json:"installation_id"` // -> GitHubInstallation.InstallationID
	RepoID         int64          `gorm:"uniqueIndex;not null" json:"repo_id"`   // GitHub's repo ID
	Owner          string         `gorm:"size:255;not null" json:"owner"`
	Name           string         `gorm:"size:255;not null" json:"name"`
	FullName       string         `gorm:"size:511;not null" json:"full_name"` // "owner/name"
	DefaultBranch  string         `gorm:"size:100;not null" json:"default_branch"`
	ProjectID      string         `gorm:"index;size:36" json:"project_id"` // linked ASDL project, empty if not yet linked
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

// GitHubWebhookEvent records incoming webhook deliveries for idempotency.
// Prevents duplicate deployments from duplicate webhook deliveries.
type GitHubWebhookEvent struct {
	ID         string    `gorm:"primaryKey;size:36" json:"id"`
	DeliveryID string    `gorm:"uniqueIndex;size:100;not null" json:"delivery_id"` // X-GitHub-Delivery header
	Event      string    `gorm:"size:50;not null" json:"event"`
	RepoID     int64     `gorm:"index;not null" json:"repo_id"`
	ProjectID  string    `gorm:"index;size:36" json:"project_id"`
	Processed  bool      `gorm:"default:false" json:"processed"`
	CreatedAt  time.Time `json:"created_at"`
}
