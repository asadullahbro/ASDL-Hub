package services

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

// badReleaseWindow is how long after a deploy finishes that failing health checks are blamed on it. The Hub only
// counts failures after the startup grace (3 minutes) plus a few checks, so this leaves a wide margin; an app
// that dies hours later is a different problem and is failed over or restarted as usual.
const badReleaseWindow = 10 * time.Minute

// deploymentBefore is the newest deployment of the project that completed before dep was created: what the
// project was running when dep replaced it. Nil when there is none.
func deploymentBefore(db *gorm.DB, dep *models.Deployment) *models.Deployment {
	var prev models.Deployment
	err := db.Where("project_id = ? AND status = ? AND id != ? AND created_at < ? AND image_name != ''",
		dep.ProjectID, models.DeploymentStatusCompleted, dep.ID, dep.CreatedAt).
		Order("created_at desc").First(&prev).Error
	if err != nil {
		return nil
	}
	return &prev
}

// autoRollbackTarget is the deployment to go back to when project's latest deploy has gone bad, or nil when it
// should not be rolled back. Only a push or a redeploy that changed the image is rolled back, and only
// shortly after it finished: a rollback or failover is never undone, and neither is a redeploy of the same
// image (a config change an older image cannot fix).
func autoRollbackTarget(db *gorm.DB, project *models.Project, now time.Time) *models.Deployment {
	if project.DeploymentID == "" {
		return nil
	}
	var dep models.Deployment
	if db.First(&dep, "id = ?", project.DeploymentID).Error != nil {
		return nil
	}
	if dep.Status != models.DeploymentStatusCompleted || dep.CompletedAt == nil || now.Sub(*dep.CompletedAt) > badReleaseWindow {
		return nil
	}
	if dep.Trigger != TriggerCI && dep.Trigger != TriggerManual {
		return nil
	}
	prev := deploymentBefore(db, &dep)
	if prev == nil || prev.ImageName == dep.ImageName {
		return nil
	}
	return prev
}

// manualRollbackTarget is the deployment `asdl-hub rollback` goes back to: the newest completed one that ran a
// different image than the project does now. Nil when the project has only ever run one image.
func manualRollbackTarget(db *gorm.DB, project *models.Project) *models.Deployment {
	var prev models.Deployment
	err := db.Where("project_id = ? AND status = ? AND image_name != '' AND image_name != ?",
		project.ID, models.DeploymentStatusCompleted, project.Image).
		Order("created_at desc").First(&prev).Error
	if err != nil {
		return nil
	}
	return &prev
}

// rollbackUnderway reports whether project's current deployment is a rollback that has not finished, so the
// health checker waits for it instead of failing the app over while it is being put back.
func (s *HealthService) rollbackUnderway(project *models.Project) bool {
	if project.DeploymentID == "" {
		return false
	}
	var dep models.Deployment
	if s.db.First(&dep, "id = ?", project.DeploymentID).Error != nil {
		return false
	}
	return dep.Trigger == TriggerRollback &&
		(dep.Status == models.DeploymentStatusPending || dep.Status == models.DeploymentStatusRunning)
}

// tryRollback puts the previous image back when an app starts failing right after a new one was deployed. It
// reports whether it took over (a rollback was queued), so the caller should not fail the app over.
func (s *HealthService) tryRollback(project *models.Project) bool {
	prev := autoRollbackTarget(s.db, project, time.Now())
	if prev == nil {
		return false
	}
	var node models.Node
	if s.db.First(&node, "id = ?", project.NodeID).Error != nil || !node.Online {
		return false // its node is down: that is a failover, not a bad release
	}
	bad := project.Image
	log.Printf("⏪ %s fails its health checks after deploying %s: rolling back to %s on %s", project.Name, bad, prev.ImageName, node.Hostname)
	if _, _, err := s.deployer.Dispatch(project, &node, prev.ImageName, DeployMeta{Trigger: TriggerRollback, Repository: project.Repository}); err != nil {
		log.Printf("❌ Failed to queue the rollback of %s: %v", project.Name, err)
		return false
	}
	s.db.Model(project).Update("health_status", "degraded")
	// A bad release is a failed deploy, so this goes to the channels that already follow those.
	notify(Event{
		Type: EventDeployFailed, Level: LevelWarning,
		Title:   project.Name + " was rolled back",
		Message: fmt.Sprintf("%s stopped answering after %s was deployed on %s, so the previous version (%s) is being put back.", project.Name, bad, node.Hostname, prev.ImageName),
		URL:     link("/projects"), ProjectID: project.ID, Project: project.Name, Node: node.Hostname,
	})
	return true
}

// RollbackInfo handles GET /projects/:id/rollback: what a rollback would put back.
func (s *ProjectService) RollbackInfo(c *gin.Context) {
	var project models.Project
	if err := s.db.First(&project, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	out := gin.H{"project_id": project.ID, "current_image": project.Image, "previous": nil}
	if prev := manualRollbackTarget(s.db, &project); prev != nil {
		out["previous"] = gin.H{"image": prev.ImageName, "deployed_at": prev.CreatedAt, "commit": prev.Commit, "commit_msg": prev.CommitMsg}
	}
	c.JSON(http.StatusOK, out)
}

// Rollback handles POST /projects/:id/rollback: redeploy the previous image on the project's node.
func (s *ProjectService) Rollback(c *gin.Context) {
	var project models.Project
	if err := s.db.First(&project, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	prev := manualRollbackTarget(s.db, &project)
	if prev == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "there is no earlier version to roll back to"})
		return
	}
	var running int64
	s.db.Model(&models.Deployment{}).Where("project_id = ? AND status IN ?", project.ID,
		[]string{models.DeploymentStatusPending, models.DeploymentStatusRunning}).Count(&running)
	if running > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "a deploy is already in progress; wait for it to finish"})
		return
	}
	job, err := s.deployImage(&project, prev.ImageName, TriggerRollback)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"job_id": job.ID, "node_id": job.NodeID, "image": prev.ImageName})
}
