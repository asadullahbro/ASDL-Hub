package services

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

func newStatusDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewDB(t, &models.Project{}, &models.AppOutage{}, &models.Setting{})
}

func TestAppStatus(t *testing.T) {
	cases := []struct {
		status, health, want string
	}{
		{"running", "healthy", StatusOperational},
		{"running", "unknown", StatusOperational}, // just deployed, not checked yet
		{"running", "degraded", StatusDegraded},
		{"running", "migrating", StatusDegraded},
		{"running", "unhealthy", StatusDown},
		{"failed", "unhealthy", StatusDown},
		{"failed", "healthy", StatusDown},
		{"deploying", "unknown", StatusDegraded},
	}
	for _, tc := range cases {
		if got := appStatus(&models.Project{Status: tc.status, HealthStatus: tc.health}); got != tc.want {
			t.Errorf("%s/%s: got %s, want %s", tc.status, tc.health, got, tc.want)
		}
	}
}

func TestOverallStatus(t *testing.T) {
	app := func(s string) StatusApp { return StatusApp{Status: s} }
	cases := []struct {
		apps []StatusApp
		want string
	}{
		{nil, StatusOperational},
		{[]StatusApp{app(StatusOperational), app(StatusOperational)}, StatusOperational},
		{[]StatusApp{app(StatusOperational), app(StatusDown)}, StatusDegraded},
		{[]StatusApp{app(StatusOperational), app(StatusDegraded)}, StatusDegraded},
		{[]StatusApp{app(StatusDown), app(StatusDown)}, StatusDown},
	}
	for i, tc := range cases {
		if got := overallStatus(tc.apps); got != tc.want {
			t.Errorf("case %d: got %s, want %s", i, got, tc.want)
		}
	}
}

func TestAppOutages_OpenOnceCloseAndReload(t *testing.T) {
	db := newStatusDB(t)
	t0 := time.Now().Add(-10 * time.Minute)
	openAppOutage(db, "p1", t0)
	openAppOutage(db, "p1", t0.Add(time.Minute)) // already open: no second row
	var n int64
	db.Model(&models.AppOutage{}).Count(&n)
	if n != 1 {
		t.Fatalf("want one open outage, got %d", n)
	}
	if got := openAppOutages(db); len(got) != 1 || !got["p1"].Equal(t0) {
		t.Fatalf("open outages = %v", got)
	}

	closeAppOutage(db, "p1", t0.Add(4*time.Minute))
	var o models.AppOutage
	db.First(&o)
	if o.EndedAt == nil || o.Seconds != 240 {
		t.Fatalf("want a closed 240s outage, got %+v", o)
	}
	if len(openAppOutages(db)) != 0 {
		t.Fatal("nothing should be open any more")
	}
	closeAppOutage(db, "p1", t0.Add(time.Hour)) // closing again changes nothing
	db.First(&o)
	if o.Seconds != 240 {
		t.Fatalf("a second close must not change the outage, got %+v", o)
	}
}

func TestReport_UptimeIncidentsAndPublicFilter(t *testing.T) {
	db := newStatusDB(t)
	now := time.Now()
	created := now.Add(-40 * 24 * time.Hour)
	db.Create(&models.Project{ID: "web", Name: "website", Domain: "example.com", Status: "running", HealthStatus: "healthy", CreatedAt: created})
	db.Create(&models.Project{ID: "int", Name: "internal-bot", Status: "running", HealthStatus: "unhealthy", CreatedAt: created})
	db.Save(&models.Setting{Key: settingAppTrackedSince, Value: now.Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)})
	// website was down for 1 day out of the last week
	end := now.Add(-2 * 24 * time.Hour)
	db.Create(&models.AppOutage{ProjectID: "web", StartedAt: end.Add(-24 * time.Hour), EndedAt: &end, Seconds: 86400})
	db.Create(&models.AppOutage{ProjectID: "int", StartedAt: now.Add(-5 * time.Minute)})
	svc := NewStatusService(db)

	pub := svc.Report(now, true)
	if len(pub.Apps) != 1 || pub.Apps[0].Name != "website" {
		t.Fatalf("the public page lists only apps with a domain, got %+v", pub.Apps)
	}
	if got := *pub.Apps[0].Uptime7d; got < 85.6 || got > 85.8 { // 6 of 7 days
		t.Fatalf("7-day uptime = %v, want about 85.71", got)
	}
	if got := *pub.Apps[0].Uptime30d; got < 96.6 || got > 96.8 { // 29 of 30 days
		t.Fatalf("30-day uptime = %v, want about 96.67", got)
	}
	if len(pub.Incidents) != 1 || pub.Incidents[0].App != "website" || pub.Incidents[0].Seconds != 86400 {
		t.Fatalf("incidents = %+v", pub.Incidents)
	}
	if pub.Overall != StatusOperational {
		t.Fatalf("overall = %s", pub.Overall)
	}

	all := svc.Report(now, false)
	if len(all.Apps) != 2 || all.Overall != StatusDegraded {
		t.Fatalf("the internal page lists every app: %+v overall %s", all.Apps, all.Overall)
	}
	if len(all.Incidents) != 2 || all.Incidents[0].App != "internal-bot" || all.Incidents[0].EndedAt != nil {
		t.Fatalf("an ongoing outage comes first and has no end: %+v", all.Incidents)
	}
}

func TestStatusEndpoints_PrivateUntilAnAdminTurnsItOn(t *testing.T) {
	db := newStatusDB(t)
	db.Create(&models.Project{ID: "web", Name: "website", Domain: "example.com", Status: "running", HealthStatus: "healthy", NodeID: "secret-node-id"})
	svc := NewStatusService(db)

	c, w := newTestContext(http.MethodGet, "/status/apps", "")
	svc.Public(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("private by default: want 404, got %d", w.Code)
	}

	c, w = newTestContext(http.MethodPut, "/settings/status-page", `{}`)
	svc.PutSetting(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a body without public must be refused, got %d", w.Code)
	}
	c, w = newTestContext(http.MethodPut, "/settings/status-page", `{"public": true}`)
	svc.PutSetting(c)
	if w.Code != http.StatusOK || !svc.IsPublic() {
		t.Fatalf("turning it on: %d %s", w.Code, w.Body.String())
	}

	c, w = newTestContext(http.MethodGet, "/status/apps", "")
	svc.Public(c)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `"website"`) {
		t.Fatalf("public now: %d %s", w.Code, body)
	}
	for _, leak := range []string{"secret-node-id", "node_id", "example.com", "image", "ports"} {
		if strings.Contains(body, leak) {
			t.Fatalf("the public page must not expose %q: %s", leak, body)
		}
	}

	c, _ = newTestContext(http.MethodPut, "/settings/status-page", `{"public": false}`)
	svc.PutSetting(c)
	c, w = newTestContext(http.MethodGet, "/status/apps", "")
	svc.Public(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("turned off again: want 404, got %d", w.Code)
	}
}

// healthyApp serves /health and returns a project on it, for the health checker to find answering.
func healthyApp(t *testing.T) (*HealthService, *gorm.DB) {
	t.Helper()
	hs, _, db := newFailoverEnv(t)
	if err := db.AutoMigrate(&models.AppOutage{}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	db.Model(&models.Node{}).Where("id = ?", "good").Update("vpn_ip", "127.0.0.1")
	db.Model(&models.Project{}).Where("id = ?", "p1").Updates(map[string]interface{}{"node_id": "good", "status": "running", "health_status": "unhealthy"})
	db.Exec("UPDATE projects SET ports = ? WHERE id = ?", `["`+u.Port()+`:8000"]`, "p1")
	return hs, db
}

func openOutagesOf(db *gorm.DB) (open, closed int64) {
	db.Model(&models.AppOutage{}).Where("ended_at IS NULL").Count(&open)
	db.Model(&models.AppOutage{}).Where("ended_at IS NOT NULL").Count(&closed)
	return
}

func TestHealth_RecordsAnAppOutageFromDownToBackUp(t *testing.T) {
	hs, db := healthyApp(t)
	p := loadProject(db)
	hs.announceDown(&p, "and it is being moved.")
	if open, _ := openOutagesOf(db); open != 1 {
		t.Fatalf("announcing an app down should open an outage, got %d open", open)
	}
	hs.announceDown(&p, "again") // announced once per outage
	if open, closed := openOutagesOf(db); open != 1 || closed != 0 {
		t.Fatalf("still one open outage, got open=%d closed=%d", open, closed)
	}

	hs.checkAllProjects() // the app answers again
	if open, closed := openOutagesOf(db); open != 0 || closed != 1 {
		t.Fatalf("recovery should close it, got open=%d closed=%d", open, closed)
	}
}

func TestHealth_ClosesAnOutageThatWasOpenBeforeTheHubRestarted(t *testing.T) {
	hs, db := healthyApp(t)
	openAppOutage(db, "p1", time.Now().Add(-time.Hour)) // recorded by the Hub's previous run
	if len(hs.down) != 0 {
		t.Fatal("a fresh health service knows of no outage yet")
	}

	hs.checkAllProjects()
	if open, closed := openOutagesOf(db); open != 0 || closed != 1 {
		t.Fatalf("the outage from before the restart should be closed, got open=%d closed=%d", open, closed)
	}
}
