package services

import (
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

// Like the node outage bookkeeping, none of this may break the health checker: a failed write is logged and the
// checker carries on.

// settingAppTrackedSince holds when app outage tracking began. Before that no outage was recorded, so an app's
// availability is only counted from then on.
const settingAppTrackedSince = "app_outage_tracking_since"

// appTrackingSince returns when app outage tracking began, recording now on the very first call.
func appTrackingSince(db *gorm.DB, now time.Time) time.Time {
	var s models.Setting
	if err := db.First(&s, "key = ?", settingAppTrackedSince).Error; err == nil {
		if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
			return t
		}
	}
	db.Save(&models.Setting{Key: settingAppTrackedSince, Value: now.UTC().Format(time.RFC3339)})
	return now
}

// openAppOutage records that an app went down at since, unless an outage for it is already open.
func openAppOutage(db *gorm.DB, projectID string, since time.Time) {
	appTrackingSince(db, since) // starts the availability clock the first time an app goes down
	var open int64
	if err := db.Model(&models.AppOutage{}).Where("project_id = ? AND ended_at IS NULL", projectID).Count(&open).Error; err != nil {
		log.Printf("⚠️ Could not check app outages for %s: %v", projectID, err)
		return
	}
	if open > 0 {
		return
	}
	if err := db.Create(&models.AppOutage{ProjectID: projectID, StartedAt: since}).Error; err != nil {
		log.Printf("⚠️ Could not record the outage of app %s: %v", projectID, err)
	}
}

// closeAppOutage records that an app answers again, ending any outage still open for it.
func closeAppOutage(db *gorm.DB, projectID string, now time.Time) {
	var open []models.AppOutage
	if err := db.Where("project_id = ? AND ended_at IS NULL", projectID).Find(&open).Error; err != nil {
		log.Printf("⚠️ Could not look up the open outage of app %s: %v", projectID, err)
		return
	}
	for _, o := range open {
		secs := int64(now.Sub(o.StartedAt).Seconds())
		if secs < 0 {
			secs = 0
		}
		if err := db.Model(&models.AppOutage{}).Where("id = ?", o.ID).Updates(map[string]interface{}{"ended_at": now, "seconds": secs}).Error; err != nil {
			log.Printf("⚠️ Could not close the outage of app %s: %v", projectID, err)
		}
	}
}

// openAppOutages maps each app that has an outage open to when it began, so a restarted Hub carries on
// with the outages it already knew about instead of leaving them open for good.
func openAppOutages(db *gorm.DB) map[string]time.Time {
	out := map[string]time.Time{}
	var open []models.AppOutage
	if err := db.Where("ended_at IS NULL").Find(&open).Error; err != nil {
		return out
	}
	for _, o := range open {
		if t, ok := out[o.ProjectID]; !ok || o.StartedAt.Before(t) {
			out[o.ProjectID] = o.StartedAt
		}
	}
	return out
}
