package models

import "time"

// AppOutage is one stretch during which an app stopped answering its health checks, kept for good (a few rows a
// week) so the status page can say how available each app has been.
type AppOutage struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	ProjectID string `gorm:"index;not null;size:36" json:"project_id"`
	// StartedAt is when its run of failed checks began.
	StartedAt time.Time `json:"started_at"`
	// EndedAt is when it answered again; nil while it is still down.
	EndedAt *time.Time `json:"ended_at"`
	// Seconds is the length of the outage, filled in when it ends.
	Seconds int64 `json:"seconds"`
}
