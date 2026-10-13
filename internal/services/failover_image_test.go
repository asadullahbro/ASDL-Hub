package services

import (
	"testing"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

// A failover that fires while a CI deploy is still running must start the
// image being deployed, not the one the project last finished deploying.
func TestFailoverImage_PrefersInFlightDeploy(t *testing.T) {
	db := testutil.NewDB(t, &models.Project{}, &models.Deployment{})
	svc := &HealthService{db: db}

	db.Create(&models.Deployment{ID: "d-new", ProjectID: "p", ImageName: "img:new", Status: models.DeploymentStatusPending})
	db.Create(&models.Deployment{ID: "d-done", ProjectID: "p", ImageName: "img:old", Status: models.DeploymentStatusCompleted})

	pending := &models.Project{ID: "p", Image: "img:old", DeploymentID: "d-new"}
	if got := svc.failoverImage(pending); got != "img:new" {
		t.Errorf("with a deploy in flight, failover image = %q, want img:new", got)
	}

	finished := &models.Project{ID: "p", Image: "img:old", DeploymentID: "d-done"}
	if got := svc.failoverImage(finished); got != "img:old" {
		t.Errorf("with no deploy in flight, failover image = %q, want img:old", got)
	}

	none := &models.Project{ID: "p", Image: "img:old"}
	if got := svc.failoverImage(none); got != "img:old" {
		t.Errorf("with no deployment, failover image = %q, want img:old", got)
	}
}
