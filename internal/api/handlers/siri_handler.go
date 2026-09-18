package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

// SiriHandler exposes a small set of hostname-keyed, plain-text-response
// endpoints meant to be called from an iOS/macOS Shortcuts "Get Contents of
// URL" action. They sit behind the same Bearer auth (JWT or permanent
// token) and operator role as the rest of the protected API — Shortcuts
// just isn't a great place to juggle node UUIDs, so these take a hostname
// instead and return a short sentence Siri can speak.
type SiriHandler struct {
	db *gorm.DB
}

func NewSiriHandler(db *gorm.DB) *SiriHandler {
	return &SiriHandler{db: db}
}

func (h *SiriHandler) findNodeByHostname(hostname string) (*models.Node, error) {
	var node models.Node
	err := h.db.Where("LOWER(hostname) = LOWER(?)", hostname).First(&node).Error
	return &node, err
}

// Nodes returns a plain list of hostnames, meant to feed a Shortcuts
// "Choose from List" step so Run/Shutdown can offer a picker instead of
// requiring the hostname to be typed out.
func (h *SiriHandler) Nodes(c *gin.Context) {
	var nodes []models.Node
	h.db.Order("hostname ASC").Find(&nodes)

	hostnames := make([]string, len(nodes))
	for i, n := range nodes {
		hostnames[i] = n.Hostname
	}

	c.JSON(http.StatusOK, hostnames)
}

// Health returns a one-line spoken-friendly summary of fleet health, e.g.
// "3 of 4 nodes online. mac-mini has been offline since 14:02."
func (h *SiriHandler) Health(c *gin.Context) {
	var nodes []models.Node
	if err := h.db.Find(&nodes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Could not reach the hub database."})
		return
	}

	if len(nodes) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "No nodes are enrolled yet."})
		return
	}

	online := 0
	var offline []string
	for _, n := range nodes {
		if n.Online {
			online++
		} else {
			offline = append(offline, n.Hostname)
		}
	}

	msg := fmt.Sprintf("%d of %d nodes online.", online, len(nodes))
	if len(offline) > 0 {
		msg += fmt.Sprintf(" Offline: %s.", strings.Join(offline, ", "))
	}

	c.JSON(http.StatusOK, gin.H{
		"message": msg,
		"online":  online,
		"total":   len(nodes),
		"offline": offline,
	})
}

// Run dispatches an arbitrary shell command to a node, looked up by
// hostname, as a pending job for the agent to pick up on its next poll.
func (h *SiriHandler) Run(c *gin.Context) {
	var req struct {
		Hostname string `json:"hostname" binding:"required"`
		Command  string `json:"command" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "I need both a hostname and a command.", "error": err.Error()})
		return
	}

	node, err := h.findNodeByHostname(req.Hostname)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": fmt.Sprintf("I couldn't find a node called %s.", req.Hostname)})
		return
	}

	job := &models.Job{
		ID:         uuid.New().String(),
		NodeID:     node.ID,
		Type:       models.JobTypeCommand,
		Status:     models.JobStatusPending,
		Command:    req.Command,
		MaxRetries: 1,
		CreatedAt:  time.Now(),
	}
	if err := h.db.Create(job).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to queue the command."})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": fmt.Sprintf("Command queued for %s.", node.Hostname),
		"job_id":  job.ID,
	})
}

// Shutdown queues an OS-appropriate shutdown command for a node.
func (h *SiriHandler) Shutdown(c *gin.Context) {
	var req struct {
		Hostname string `json:"hostname" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "I need a hostname to shut down.", "error": err.Error()})
		return
	}

	node, err := h.findNodeByHostname(req.Hostname)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": fmt.Sprintf("I couldn't find a node called %s.", req.Hostname)})
		return
	}

	// "shutdown -h now" works unchanged on both macOS and Linux agents.
	command := "sudo shutdown -h now"

	job := &models.Job{
		ID:         uuid.New().String(),
		NodeID:     node.ID,
		Type:       models.JobTypeCommand,
		Status:     models.JobStatusPending,
		Command:    command,
		MaxRetries: 1,
		CreatedAt:  time.Now(),
	}
	if err := h.db.Create(job).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to queue the shutdown."})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": fmt.Sprintf("Shutting down %s.", node.Hostname),
		"job_id":  job.ID,
	})
}
