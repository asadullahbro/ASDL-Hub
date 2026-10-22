package services

import (
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

// settingStatusPublic is "true" when the status page may be read without logging in. Off unless an admin turns it on.
const settingStatusPublic = "status_page_public"

const (
	StatusOperational = "operational"
	StatusDegraded    = "degraded"
	StatusDown        = "down"
)

// StatusApp is one app on the status page. Only what a visitor needs: a name and how it is doing, never where it runs.
type StatusApp struct {
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Uptime7d  *float64 `json:"uptime_7d"`
	Uptime30d *float64 `json:"uptime_30d"`
}

// StatusIncident is one time an app was down.
type StatusIncident struct {
	App       string     `json:"app"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Seconds   int64      `json:"seconds"`
}

type StatusReport struct {
	UpdatedAt time.Time `json:"updated_at"`
	// Overall is operational when every app is, down when every app is, and degraded in between.
	Overall      string           `json:"overall"`
	Apps         []StatusApp      `json:"apps"`
	Incidents    []StatusIncident `json:"incidents"`
	TrackedSince time.Time        `json:"tracked_since"`
}

// appStatus is the state of an app as a visitor would see it. An app whose health is still unknown (just
// deployed, not checked yet) counts as operational, like the dashboard does.
func appStatus(p *models.Project) string {
	switch {
	case p.Status == "failed" || p.HealthStatus == "unhealthy":
		return StatusDown
	case p.HealthStatus == "degraded" || p.HealthStatus == "migrating" || (p.Status != "running" && p.Status != ""):
		return StatusDegraded
	}
	return StatusOperational
}

func overallStatus(apps []StatusApp) string {
	if len(apps) == 0 {
		return StatusOperational
	}
	up, down := 0, 0
	for _, a := range apps {
		switch a.Status {
		case StatusOperational:
			up++
		case StatusDown:
			down++
		}
	}
	switch {
	case up == len(apps):
		return StatusOperational
	case down == len(apps):
		return StatusDown
	}
	return StatusDegraded
}

type StatusService struct {
	db *gorm.DB
}

func NewStatusService(db *gorm.DB) *StatusService { return &StatusService{db: db} }

// IsPublic reports whether the status page may be read without logging in.
func (s *StatusService) IsPublic() bool {
	var set models.Setting
	return s.db.First(&set, "key = ?", settingStatusPublic).Error == nil && set.Value == "true"
}

// SetPublic turns public access on or off.
func (s *StatusService) SetPublic(on bool) error {
	v := "false"
	if on {
		v = "true"
	}
	return s.db.Save(&models.Setting{Key: settingStatusPublic, Value: v, UpdatedAt: time.Now()}).Error
}

// Report builds the page. publicOnly limits it to apps that have a domain: the ones already reachable from the
// internet, so listing them tells a visitor nothing new.
func (s *StatusService) Report(now time.Time, publicOnly bool) StatusReport {
	var projects []models.Project
	q := s.db.Order("name asc")
	if publicOnly {
		q = q.Where("domain != ''")
	}
	q.Find(&projects)

	since := appTrackingSince(s.db, now)
	rep := StatusReport{UpdatedAt: now, Apps: []StatusApp{}, Incidents: []StatusIncident{}, TrackedSince: since}
	names := map[string]string{}
	ids := make([]string, 0, len(projects))
	for i := range projects {
		p := &projects[i]
		names[p.ID] = p.Name
		ids = append(ids, p.ID)
		var outages []models.AppOutage
		s.db.Where("project_id = ?", p.ID).Order("id desc").Limit(500).Find(&outages)
		asNode := make([]models.NodeOutage, len(outages))
		for j, o := range outages {
			asNode[j] = models.NodeOutage{StartedAt: o.StartedAt, EndedAt: o.EndedAt}
		}
		window := func(d time.Duration) time.Time {
			from := now.Add(-d)
			if since.After(from) {
				from = since
			}
			if p.CreatedAt.After(from) {
				from = p.CreatedAt
			}
			return from
		}
		u7, u30 := uptimePercent(asNode, window(7*24*time.Hour), now), uptimePercent(asNode, window(30*24*time.Hour), now)
		rep.Apps = append(rep.Apps, StatusApp{Name: p.Name, Status: appStatus(p), Uptime7d: &u7, Uptime30d: &u30})
	}
	rep.Overall = overallStatus(rep.Apps)

	if len(ids) > 0 {
		var recent []models.AppOutage
		s.db.Where("project_id IN ? AND (ended_at IS NULL OR ended_at > ?)", ids, now.Add(-30*24*time.Hour)).
			Order("started_at desc").Limit(10).Find(&recent)
		for _, o := range recent {
			secs := o.Seconds
			if o.EndedAt == nil {
				secs = int64(now.Sub(o.StartedAt).Seconds())
			}
			rep.Incidents = append(rep.Incidents, StatusIncident{App: names[o.ProjectID], StartedAt: o.StartedAt, EndedAt: o.EndedAt, Seconds: secs})
		}
		sort.SliceStable(rep.Incidents, func(i, j int) bool { return rep.Incidents[i].StartedAt.After(rep.Incidents[j].StartedAt) })
	}
	return rep
}

// Public handles GET /status/apps, with no login: the page for visitors, available only once an admin has made it public.
func (s *StatusService) Public(c *gin.Context) {
	if !s.IsPublic() {
		c.JSON(http.StatusNotFound, gin.H{"error": "the status page is not public"})
		return
	}
	c.JSON(http.StatusOK, s.Report(time.Now(), true))
}

// Internal handles GET /status/apps/internal, for logged-in users: every app, public or not.
func (s *StatusService) Internal(c *gin.Context) {
	c.JSON(http.StatusOK, s.Report(time.Now(), false))
}

// GetSetting handles GET /settings/status-page.
func (s *StatusService) GetSetting(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"public": s.IsPublic()})
}

// PutSetting handles PUT /settings/status-page {"public": bool}.
func (s *StatusService) PutSetting(c *gin.Context) {
	var req struct {
		Public *bool `json:"public"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Public == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": `expected {"public": true|false}`})
		return
	}
	if err := s.SetPublic(*req.Public); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"public": *req.Public})
}
