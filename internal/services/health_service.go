package services

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

// failoverThreshold is how many health checks in a row must fail before a
// project is failed over, so one slow response or a restart doesn't move it.
// With healthCheckInterval that is about 30 seconds of downtime.
const (
	failoverThreshold   = 3
	healthCheckInterval = 10 * time.Second
	healthCheckTimeout  = 3 * time.Second
	// startupGrace is how long after a deploy an app may still be starting. Apps with a slow start (a bot
	// that registers commands and runs migrations) refuse connections for a while; those checks must not
	// count as failures, or the Hub marks a healthy app failed and gives up on it.
	startupGrace = 3 * time.Minute
)

// inStartupGrace reports whether a project deployed at lastDeployed is still within its startup window.
// A zero time (never deployed through the Hub) is never in grace.
func inStartupGrace(lastDeployed, now time.Time) bool {
	if lastDeployed.IsZero() {
		return false
	}
	d := now.Sub(lastDeployed)
	return d >= 0 && d < startupGrace
}

type HealthService struct {
	db               *gorm.DB
	migrationService *MigrationService
	nginxService     *NginxService
	deployer         *Deployer
	// failures counts consecutive failed checks per project ID. Only the
	// checker goroutine touches it.
	failures map[string]int
	// down holds when each project was announced down, so "back up" is
	// announced once it answers again. Only the checker goroutine touches it.
	down map[string]time.Time
	// failingSince holds when each project's current run of failed checks
	// began. Only the checker goroutine touches it.
	failingSince map[string]time.Time
	// unconfirmed counts, per project, how many times in a row it stopped answering without its node's agent
	// reporting the container as down. Only the checker goroutine touches it.
	unconfirmed map[string]int
	// restarts holds when each project was last restarted in place, to cap the attempts. Only the checker goroutine touches it.
	restarts map[string][]time.Time
	// recovering counts consecutive good checks per failed project. Only the checker goroutine touches it.
	recovering map[string]int
}

func NewHealthService(db *gorm.DB, migrationService *MigrationService, nginxService *NginxService, deployer *Deployer) *HealthService {
	return &HealthService{
		db:               db,
		migrationService: migrationService,
		nginxService:     nginxService,
		deployer:         deployer,
		failures:         make(map[string]int),
		recovering:       make(map[string]int),
		restarts:         make(map[string][]time.Time),
		unconfirmed:      make(map[string]int),
		down:             make(map[string]time.Time),
		failingSince:     make(map[string]time.Time),
	}
}

func (s *HealthService) StartHealthChecker() {
	ticker := time.NewTicker(healthCheckInterval)
	go func() {
		log.Println("🩺 Health checker started")
		for range ticker.C {
			s.checkAllProjects()
		}
	}()
}

const (
	// maxInPlaceRestarts is how many times an app is restarted on its own node within restartWindow before the
	// Hub gives up: a crash loop (a bad image, a broken config) won't be fixed by restarting it again.
	maxInPlaceRestarts = 2
	restartWindow      = 30 * time.Minute
)

// restartAllowed reports whether another in-place restart is allowed, given the times of earlier ones, and
// returns the attempts that still count (those inside the window).
func restartAllowed(history []time.Time, now time.Time) (bool, []time.Time) {
	recent := history[:0:0]
	for _, t := range history {
		if now.Sub(t) < restartWindow {
			recent = append(recent, t)
		}
	}
	return len(recent) < maxInPlaceRestarts, recent
}

// deployInProgress reports whether the project has a deploy that is still pending or running.
func (s *HealthService) deployInProgress(project *models.Project) bool {
	if project.DeploymentID == "" {
		return false
	}
	var dep models.Deployment
	if s.db.First(&dep, "id = ?", project.DeploymentID).Error != nil {
		return false
	}
	return dep.Status == models.DeploymentStatusPending || dep.Status == models.DeploymentStatusRunning
}

// agentReportFresh is how recent the node's last heartbeat must be to trust the container list it carried.
// Agents beat every 30s.
const agentReportFresh = 60 * time.Second

// maxUnconfirmedWaits is how many rounds (about 30s each) an app that isn't answering may wait for its agent to
// confirm the container is down. The agent's report can be up to 30s behind, so right after a real crash it
// still shows the container running; after this many rounds the Hub stops waiting and treats the app as failed.
const maxUnconfirmedWaits = 3

// containerConfirmedDown reports whether the node's own agent says the project's container is not running:
// a fresh report that lacks it or shows it in another state. A failed health check alone is not enough,
// because the Hub's check can fail while the app is fine (a network blip between the Hub and the node,
// which can last a minute before the node counts as offline), and redeploying a healthy app is a real outage.
// An agent that reports no containers (an old one) gives no answer, so this is false.
func containerConfirmedDown(node *models.Node, project *models.Project, now time.Time) bool {
	if now.Sub(node.LastHeartbeat) > agentReportFresh || len(node.Containers) == 0 {
		return false
	}
	for _, c := range node.Containers {
		if (c.ProjectID != "" && c.ProjectID == project.ID) || c.Name == project.Name {
			return c.State != "running"
		}
	}
	return true // the node is reporting and the container is not among what runs
}

// restartInPlace redeploys the project's image on the node it already runs on, for an app that stopped
// answering while its node is still online (a crashed container, not a lost node). It reports whether it
// took over: true means a restart was queued or is already underway, false means the caller should give up.
func (s *HealthService) restartInPlace(project *models.Project, image string) bool {
	var node models.Node
	if s.db.First(&node, "id = ?", project.NodeID).Error != nil || !node.Online {
		return false
	}
	if s.deployInProgress(project) {
		log.Printf("⏳ %s already has a deploy underway, waiting for it", project.Name)
		return true
	}
	if !containerConfirmedDown(&node, project, time.Now()) {
		s.unconfirmed[project.ID]++
		if s.unconfirmed[project.ID] <= maxUnconfirmedWaits {
			log.Printf("⏳ %s is not answering, but its node's agent does not report the container as down yet (%d/%d): waiting before redeploying a possibly healthy app",
				project.Name, s.unconfirmed[project.ID], maxUnconfirmedWaits)
			return true
		}
		delete(s.unconfirmed, project.ID)
		log.Printf("❌ %s still does not answer and its agent still reports it running: not redeploying (a network problem, or an app that hangs)", project.Name)
		return false
	}
	delete(s.unconfirmed, project.ID)
	ok, recent := restartAllowed(s.restarts[project.ID], time.Now())
	s.restarts[project.ID] = recent
	if !ok {
		log.Printf("❌ %s was already restarted %d times in %s and is still down", project.Name, maxInPlaceRestarts, restartWindow)
		return false
	}
	log.Printf("🔁 Restarting %s on %s (it stopped answering but the node is online)", project.Name, node.Hostname)
	if _, _, err := s.deployer.Dispatch(project, &node, image, DeployMeta{Trigger: TriggerFailover, Repository: project.Repository}); err != nil {
		log.Printf("❌ Failed to queue the restart of %s: %v", project.Name, err)
		return false
	}
	s.restarts[project.ID] = append(s.restarts[project.ID], time.Now())
	s.db.Model(project).Update("health_status", "degraded")
	return true
}

// recoveryChecks is how many consecutive good checks a failed project needs before it is put back in service.
const recoveryChecks = 2

// recoveryReached reports whether a failed project has answered enough times in a row to be restored.
// It updates the per-project counter: a good check adds one, a bad one resets it.
func recoveryReached(counts map[string]int, id string, ok bool) bool {
	if !ok {
		delete(counts, id)
		return false
	}
	counts[id]++
	if counts[id] >= recoveryChecks {
		delete(counts, id)
		return true
	}
	return false
}

// recoverFailed re-checks projects marked failed. One that answers again (it was only slow to start, or the
// node came back) is set running again and routed to, instead of staying down until someone redeploys it.
func (s *HealthService) recoverFailed() {
	var projects []models.Project
	s.db.Where("status = ?", "failed").Find(&projects)
	for i := range projects {
		project := &projects[i]
		if !recoveryReached(s.recovering, project.ID, s.checkProjectHealth(project)) {
			continue
		}
		log.Printf("✅ Project %s answers again, putting it back in service", project.Name)
		s.db.Model(project).Updates(map[string]interface{}{"status": "running", "health_status": "healthy"})
		if err := s.nginxService.UpdateNginxConfig(); err != nil {
			log.Printf("⚠️ Failed to update Nginx config: %v", err)
		}
		node := s.nodeName(project.NodeID)
		delete(s.down, project.ID)
		notify(Event{
			Type: EventAppRecovered, Level: LevelSuccess,
			Title:   project.Name + " is back up",
			Message: fmt.Sprintf("%s was marked failed but answers again on %s, so it is back in service.", project.Name, node),
			URL:     link("/projects"), ProjectID: project.ID, Project: project.Name, Node: node,
		})
	}
}

func (s *HealthService) checkAllProjects() {
	s.recoverFailed()
	var projects []models.Project
	s.db.Where("status = ?", "running").Find(&projects)

	// Check all projects at once so one dead node (every check timing out)
	// doesn't delay the rest; act on the results one by one.
	healthy := make([]bool, len(projects))
	var wg sync.WaitGroup
	for i := range projects {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			healthy[i] = s.checkProjectHealth(&projects[i])
		}(i)
	}
	wg.Wait()

	for i, project := range projects {
		if healthy[i] {
			delete(s.failures, project.ID)
			delete(s.failingSince, project.ID)
			delete(s.unconfirmed, project.ID)
			if project.HealthStatus != "healthy" {
				s.db.Model(&project).Update("health_status", "healthy")
			}
			if since, ok := s.down[project.ID]; ok {
				delete(s.down, project.ID)
				node := s.nodeName(project.NodeID)
				notify(Event{
					Type: EventAppRecovered, Level: LevelSuccess,
					Title:   project.Name + " is back up",
					Message: fmt.Sprintf("%s answers again on %s, after %s down.", project.Name, node, time.Since(since).Round(time.Second)),
					URL:     link("/projects"), ProjectID: project.ID, Project: project.Name, Node: node,
				})
			}
			continue
		}

		// Still starting up after a deploy: not a failure yet, just not answering so far
		if inStartupGrace(project.LastDeployed, time.Now()) {
			if project.HealthStatus != "degraded" {
				s.db.Model(&project).Update("health_status", "degraded")
			}
			log.Printf("⏳ Project %s is still starting (deployed %s ago), not counting this check", project.Name, time.Since(project.LastDeployed).Round(time.Second))
			continue
		}

		s.failures[project.ID]++
		if _, ok := s.failingSince[project.ID]; !ok {
			s.failingSince[project.ID] = time.Now()
		}
		if s.failures[project.ID] < failoverThreshold {
			s.db.Model(&project).Update("health_status", "degraded")
			log.Printf("⚠️ Project %s failed health check %d/%d", project.Name, s.failures[project.ID], failoverThreshold)
			continue
		}

		delete(s.failures, project.ID)
		log.Printf("⚠️ Project %s is unhealthy, attempting failover...", project.Name)
		s.handleUnhealthyProject(&project)
	}
}

func (s *HealthService) checkProjectHealth(project *models.Project) bool {
	var node models.Node
	if err := s.db.First(&node, "id = ?", project.NodeID).Error; err != nil {
		log.Printf("❌ Node not found for project %s: %v", project.Name, err)
		return false
	}

	if !node.Online {
		log.Printf("❌ Node %s is offline for project %s", node.Hostname, project.Name)
		return false
	}

	client := http.Client{Timeout: healthCheckTimeout}
	url := fmt.Sprintf("http://%s:%s/health", node.VPNIP, project.HostPort())

	resp, err := client.Get(url)
	if err != nil {
		log.Printf("❌ Health check failed for %s: %v", project.Name, err)
		return false
	}
	defer resp.Body.Close()

	// Any non-5xx answer means the app is up and serving. Apps without a
	// /health route answer 404, which shouldn't trigger a failover.
	if resp.StatusCode < 500 {
		return true
	}

	log.Printf("❌ Project %s returned status %d", project.Name, resp.StatusCode)
	return false
}

// failoverCooldown is how long a node that failed to take over a project is
// skipped as a failover target for that project.
const failoverCooldown = 30 * time.Minute

// handleUnhealthyProject redeploys the project on the healthiest other online
// node. Nodes that already failed to take it over recently are skipped; only
// when no node is left is the project marked failed.
func (s *HealthService) handleUnhealthyProject(project *models.Project) {
	var active int64
	s.db.Model(&models.Migration{}).
		Where("project_id = ? AND status IN ?", project.ID,
			[]string{models.MigrationStatusPending, models.MigrationStatusRunning}).
		Count(&active)
	if active > 0 {
		log.Printf("⏳ %s already has a failover in progress", project.Name)
		return
	}

	image := s.failoverImage(project)
	if image == "" {
		log.Printf("❌ %s has no image to fail over with", project.Name)
		s.announceDown(project, "and it has no image to start elsewhere.")
		s.markFailed(project)
		return
	}

	target := s.failoverTarget(project)
	if target == nil {
		// Nowhere else to go: if its own node is up, the container probably crashed, so start it again there
		if s.restartInPlace(project, image) {
			return
		}
		log.Printf("❌ No node left to run %s; marking it failed", project.Name)
		s.announceDown(project, "and no other node can take it over, so it stays down until one can or it recovers.")
		s.markFailed(project)
		return
	}
	s.announceDown(project, fmt.Sprintf("so it is being moved to %s.", target.Hostname))

	log.Printf("🔄 Failing over %s: %s -> %s", project.Name, project.NodeID, target.Hostname)
	job, _, err := s.deployer.Dispatch(project, target, image, DeployMeta{
		Trigger:    TriggerFailover,
		Repository: project.Repository,
	})
	if err != nil {
		log.Printf("❌ Failed to dispatch failover of %s: %v", project.Name, err)
		return
	}

	now := time.Now()
	migration := &models.Migration{
		ID:           uuid.New().String(),
		ProjectID:    project.ID,
		ContainerID:  project.ContainerID,
		SourceNodeID: project.NodeID,
		TargetNodeID: target.ID,
		Status:       models.MigrationStatusRunning,
		JobID:        job.ID,
		CreatedAt:    now,
	}
	if err := s.db.Create(migration).Error; err != nil {
		log.Printf("⚠️ Failed to record failover of %s: %v", project.Name, err)
	}
	s.db.Model(project).Update("health_status", "migrating")
}

// failoverImage is the image a failover should start. project.Image is only
// updated when a deploy finishes, so a failover that fires while a CI deploy
// is still running would bring back the previous version. A deploy that is
// pending or running is the newest intent; use its image.
func (s *HealthService) failoverImage(project *models.Project) string {
	if project.DeploymentID != "" {
		var dep models.Deployment
		if s.db.First(&dep, "id = ?", project.DeploymentID).Error == nil &&
			dep.ImageName != "" &&
			(dep.Status == models.DeploymentStatusPending || dep.Status == models.DeploymentStatusRunning) {
			return dep.ImageName
		}
	}
	return project.Image
}

// failoverTarget returns the best online node for project other than its
// current one, preferring the master node, or nil if there is none.
func (s *HealthService) failoverTarget(project *models.Project) *models.Node {
	var failed []string
	s.db.Model(&models.Migration{}).
		Where("project_id = ? AND status = ? AND created_at > ?", project.ID,
			models.MigrationStatusFailed, time.Now().Add(-failoverCooldown)).
		Pluck("target_node_id", &failed)

	q := s.db.Scopes(models.Available).Where("id != ?", project.NodeID)
	if len(failed) > 0 {
		q = q.Where("id NOT IN ?", failed)
	}
	var nodes []models.Node
	q.Order("health_score desc").Find(&nodes)
	if len(nodes) == 0 {
		return nil
	}

	var master models.Setting
	if s.db.First(&master, "key = ?", "master_node_id").Error == nil {
		for i := range nodes {
			if nodes[i].ID == master.Value {
				return &nodes[i]
			}
		}
	}
	return &nodes[0]
}

// announceDown sends "app down" once per outage; what continues the
// sentence says what happens next.
func (s *HealthService) announceDown(project *models.Project, next string) {
	if _, ok := s.down[project.ID]; ok {
		return
	}
	since, ok := s.failingSince[project.ID]
	if !ok {
		since = time.Now()
	}
	s.down[project.ID] = since
	node := s.nodeName(project.NodeID)
	notify(Event{
		Type: EventAppDown, Level: LevelError,
		Title:   project.Name + " is down",
		Message: fmt.Sprintf("%s stopped answering on %s, %s", project.Name, node, next),
		URL:     link("/projects"), ProjectID: project.ID, Project: project.Name, Node: node,
	})
}

func (s *HealthService) nodeName(id string) string {
	var node models.Node
	if s.db.Select("hostname").First(&node, "id = ?", id).Error != nil {
		return "an unknown node"
	}
	return node.Hostname
}

func (s *HealthService) markFailed(project *models.Project) {
	s.db.Model(project).Updates(map[string]interface{}{"status": "failed", "health_status": "unhealthy"})
	if err := s.nginxService.UpdateNginxConfig(); err != nil {
		log.Printf("⚠️ Failed to update Nginx config: %v", err)
	}
}

func (s *HealthService) GetProjectHealth(c *gin.Context) {
	id := c.Param("id")
	var project models.Project
	if err := s.db.First(&project, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}

	healthy := s.checkProjectHealth(&project)
	status := "healthy"
	if !healthy {
		status = "unhealthy"
	}

	c.JSON(http.StatusOK, gin.H{
		"project_id": project.ID,
		"name":       project.Name,
		"status":     project.Status,
		"health":     status,
		"node_id":    project.NodeID,
		"last_check": time.Now(),
	})
}
