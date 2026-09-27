package models

import "time"

// ProjectPlugin attaches a plugin to a project. The plugin's container runs
// next to the project on whichever node the project is on, and moves with it.
type ProjectPlugin struct {
	ID        string `gorm:"primaryKey;size:36" json:"id"`
	ProjectID string `gorm:"size:36;not null;uniqueIndex:idx_project_plugin" json:"project_id"`
	PluginID  string `gorm:"size:40;not null;uniqueIndex:idx_project_plugin" json:"plugin_id"`
	// Vars are the values chosen for the plugin's settings; stored encrypted
	// and masked in API responses like project env vars.
	Vars      SecretEnvVars `gorm:"type:text" json:"vars"`
	CreatedAt time.Time     `json:"created_at"`
}
