package models

import "time"

// NotificationChannel is somewhere the Hub sends alerts: a Discord or Slack
// webhook, a Telegram chat, an ntfy topic, an email address or any URL.
type NotificationChannel struct {
	ID   string `gorm:"primaryKey;size:36" json:"id"`
	Name string `gorm:"size:100;not null" json:"name"`
	Type string `gorm:"size:30;not null" json:"type"`
	// Config holds the channel's settings (webhook URL, token, ...); stored
	// encrypted. Handlers mask the secret ones before returning them.
	Config SecretEnvVars `gorm:"type:text" json:"-"`
	// Events it receives; see services.NotificationEvents.
	Events []string `gorm:"serializer:json;type:text" json:"events"`
	// Projects limits app events to these project IDs; empty means all.
	Projects   []string   `gorm:"serializer:json;type:text" json:"projects"`
	Enabled    bool       `gorm:"not null" json:"enabled"`
	LastSentAt *time.Time `json:"last_sent_at"`
	LastError  string     `gorm:"type:text;not null;default:''" json:"last_error"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
