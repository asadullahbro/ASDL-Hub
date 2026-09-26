package services

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

// firstAutoPort is where the hub starts assigning node ports to projects.
const firstAutoPort = 20000

const (
	TriggerCI        = "ci"
	TriggerFailover  = "failover"
	TriggerMigration = "migration"
)

// DeployMeta describes where a deploy came from, for the deployment record.
type DeployMeta struct {
	Trigger    string
	Repository string
	Branch     string
	Commit     string
}

// Deployer starts a project's container on a node. CI deploys, failovers and
// migrations all go through it, so every start gets the project's secrets,
// ports and a registry login, and JobService.Complete records the outcome.
type Deployer struct {
	db *gorm.DB
}

func NewDeployer(db *gorm.DB) *Deployer {
	return &Deployer{db: db}
}

// Dispatch queues a deploy job for project on node and records it as the
// project's current deployment.
func (d *Deployer) Dispatch(project *models.Project, node *models.Node, image string, meta DeployMeta) (*models.Job, *models.Deployment, error) {
	spec := DeploySpec{
		Project:           project,
		Image:             image,
		PreferredHostPort: d.preferredHostPort(project, node.ID),
	}

	var env []string
	if strings.HasPrefix(image, "ghcr.io/") {
		var token models.GitHubToken
		if d.db.Order("created_at desc").First(&token).Error == nil && token.Token != "" {
			env = append(env, RegistryTokenEnv+"="+string(token.Token))
			spec.RegistryLogin = true
		}
	}
	command, projectEnv := BuildDeployCommand(spec)
	env = append(env, projectEnv...)

	now := time.Now()
	job := &models.Job{
		ID:          uuid.New().String(),
		NodeID:      node.ID,
		Type:        models.JobTypeDeploy,
		Status:      models.JobStatusPending,
		Command:     command,
		Environment: env,
		MaxRetries:  1,
		CreatedAt:   now,
	}
	if err := d.db.Create(job).Error; err != nil {
		return nil, nil, fmt.Errorf("failed to create deploy job: %w", err)
	}

	dep := &models.Deployment{
		ID:            uuid.New().String(),
		ProjectID:     project.ID,
		Trigger:       meta.Trigger,
		JobID:         job.ID,
		NodeID:        node.ID,
		Repository:    meta.Repository,
		Branch:        meta.Branch,
		Commit:        meta.Commit,
		ImageName:     image,
		ContainerName: project.Name,
		Ports:         project.Ports,
		Volumes:       project.Volumes,
		Type:          models.DeploymentTypeDocker,
		Status:        models.DeploymentStatusPending,
		CreatedAt:     now,
	}
	if err := d.db.Create(dep).Error; err != nil {
		return nil, nil, fmt.Errorf("failed to create deployment record: %w", err)
	}

	// Status is left alone: a running project keeps serving (and keeps its
	// nginx route) until JobService.Complete records the outcome.
	d.db.Model(project).Update("deployment_id", dep.ID)
	project.DeploymentID = dep.ID

	log.Printf("🚀 Deploy queued: %s (%s) on %s [%s, job %s]", project.Name, image, node.Hostname, meta.Trigger, job.ID)
	return job, dep, nil
}

// preferredHostPort keeps a project on the node port it already has on that
// node, and otherwise picks the lowest port no other project on the node uses.
// The node still checks the port is free and moves up if it isn't.
func (d *Deployer) preferredHostPort(project *models.Project, nodeID string) int {
	var others []models.Project
	d.db.Where("node_id = ? AND id != ?", nodeID, project.ID).Find(&others)
	used := make(map[int]bool)
	for _, o := range others {
		for _, m := range o.Ports {
			if host, _, err := models.ParsePortMapping(m); err == nil {
				n, _ := strconv.Atoi(host)
				used[n] = true
			}
		}
	}

	if project.NodeID == nodeID && len(project.Ports) > 0 {
		if host, _, err := models.ParsePortMapping(project.Ports[0]); err == nil {
			if n, _ := strconv.Atoi(host); !used[n] {
				return n
			}
		}
	}
	for port := firstAutoPort; ; port++ {
		if !used[port] {
			return port
		}
	}
}
