package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

// Plugin snapshots keep a copy of a plugin's data on the Hub, so the data can be put back on another node when
// the node it lived on dies or the project moves. They are opt-in per attachment (the plugin's "persist" and
// "snapshot" settings, both off by default).
//
//   - Every few minutes the Hub gives the node that hosts the project a job: tell the plugin to write its data
//     to disk, pack the data folder and upload it here (UploadSnapshot).
//   - Every deploy of a plugin that has snapshots on first compares the newest snapshot here with the one this
//     node last took or restored; if the Hub's is newer the node downloads it before starting the plugin.
//   - A planned move takes a last snapshot on the old node and makes the new node wait for it.

const (
	defaultSnapshotMaxBytes = 32 << 20
	defaultSnapshotKeep     = 3
	// How long a node's deploy waits for a final snapshot taken for a move before carrying on without it.
	snapshotMoveWaitSeconds = 90
)

// snapshotHub is how the Hub signs and addresses snapshots; set once at startup.
var snapshotHub struct {
	Secret   string
	URL      string // how nodes reach the Hub over the mesh, e.g. http://10.101.0.1:8080
	MaxBytes int64
	Keep     int
}

// SetSnapshotHub configures snapshots. An empty url switches the feature off everywhere.
func SetSnapshotHub(secret, url string, maxBytes int64, keep int) {
	if maxBytes <= 0 {
		maxBytes = defaultSnapshotMaxBytes
	}
	if keep <= 0 {
		keep = defaultSnapshotKeep
	}
	snapshotHub.Secret, snapshotHub.URL, snapshotHub.MaxBytes, snapshotHub.Keep = secret, strings.TrimRight(url, "/"), maxBytes, keep
}

func snapshotsEnabled() bool { return snapshotHub.URL != "" && snapshotHub.Secret != "" }

// SnapshotToken is what a node presents to upload or download one plugin's snapshots. It only ever travels in
// that project's own jobs (whose environment is encrypted at rest), and only works for that project and plugin.
func SnapshotToken(secret, projectID, pluginID string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("asdl-hub/snapshot/v1\x00" + projectID + "\x00" + pluginID))
	return hex.EncodeToString(m.Sum(nil))
}

func snapshotURL(projectID, pluginID string) string {
	return snapshotHub.URL + "/api/v1/snapshots/" + projectID + "/" + pluginID
}

// fillSnapshot adds what only the Hub knows (address, token, which job to wait for) to a plugin's snapshot spec.
func fillSnapshot(project *models.Project, pl *ResolvedPlugin, afterJob string) {
	if pl.Snapshot == nil {
		return
	}
	if !snapshotsEnabled() {
		pl.Snapshot = nil
		return
	}
	pl.Snapshot.URL = snapshotURL(project.ID, pl.PluginID)
	pl.Snapshot.Token = SnapshotToken(snapshotHub.Secret, project.ID, pl.PluginID)
	pl.Snapshot.AfterJob = afterJob
}

// ---------- the scripts a node runs ----------

// snapshotJobMarker tags the jobs this file creates, so the scheduler can tell a snapshot is already queued.
const snapshotJobMarker = "snapshot"

// BuildSnapshotCommand returns the sh script (and job environment) that snapshots one plugin on its node.
func BuildSnapshotCommand(pl ResolvedPlugin, trigger, jobID string) (string, []string) {
	spec := pl.Snapshot
	var b strings.Builder
	var env []string
	b.WriteString("set -eu\n")
	fmt.Fprintf(&b, "DATA=%s\nSTATE=%s\n", shellQuote(spec.DataDir), shellQuote(spec.StateDir))
	b.WriteString("[ -d \"$DATA\" ] || { echo 'no data folder yet, nothing to snapshot'; exit 0; }\n")
	if len(spec.Before) > 0 {
		args := make([]string, len(spec.Before))
		for i, a := range spec.Before {
			v := fmt.Sprintf("ASDL_SNAP_B%d", i)
			env = append(env, v+"="+a)
			args[i] = `"$` + v + `"`
		}
		fmt.Fprintf(&b, "docker exec %s %s\n", shellQuote(pl.ContainerName), strings.Join(args, " "))
	}
	env = append(env, "ASDL_SNAPSHOT_TOKEN="+spec.Token)
	b.WriteString("TMP=$(mktemp)\ntrap 'rm -f \"$TMP\"' EXIT\n")
	// Packed inside a throwaway container from the plugin's own image: the data is owned by the plugin's user
	// (Redis writes dump.rdb as itself, mode 0600), which the node's agent may not be allowed to read.
	fmt.Fprintf(&b, "docker run --rm -v \"$DATA\":/data --entrypoint tar %s -czf - -C /data . > \"$TMP\"\n", shellQuote(pl.Image))
	fmt.Fprintf(&b, "RESP=$(curl -fsS --max-time 300 -X POST -H \"Authorization: Bearer $ASDL_SNAPSHOT_TOKEN\" -H 'Content-Type: application/gzip' -H %s -H %s --data-binary @\"$TMP\" %s)\n",
		shellQuote("X-Snapshot-Trigger: "+trigger), shellQuote("X-Job-Id: "+jobID), shellQuote(spec.URL))
	b.WriteString("GEN=$(printf '%s' \"$RESP\" | sed -n 's/.*\"generation\":\\([0-9][0-9]*\\).*/\\1/p')\n")
	b.WriteString("[ -n \"$GEN\" ] || { echo \"snapshot upload failed: $RESP\"; exit 1; }\n")
	b.WriteString("mkdir -p \"$STATE\" && echo \"$GEN\" > \"$STATE/generation\"\n")
	b.WriteString("echo \"snapshot saved: generation $GEN\"\n")
	return b.String(), env
}

// writeSnapshotCheck emits, before a plugin is (re)started, the shell that asks the Hub for its newest snapshot
// and sets NEED<i>=yes when it is newer than what this node holds. A deploy that can't reach the Hub never
// fails because of it: it carries on with whatever data the node has.
func writeSnapshotCheck(b *strings.Builder, i int, pl ResolvedPlugin, env *[]string) {
	spec := pl.Snapshot
	tok := fmt.Sprintf("ASDL_PLUGIN_%d_SNAPTOKEN", i)
	*env = append(*env, tok+"="+spec.Token)
	wait := ""
	if spec.AfterJob != "" {
		wait = fmt.Sprintf("?after_job=%s&wait=%d", spec.AfterJob, snapshotMoveWaitSeconds)
	}
	fmt.Fprintf(b, "NEED%d=no; META%d=''\n", i, i)
	fmt.Fprintf(b, "if META%d=$(curl -fsS --max-time %d -H \"Authorization: Bearer $%s\" %s 2>/dev/null); then\n", i, snapshotMoveWaitSeconds+15, tok, shellQuote(spec.URL+"/meta"+wait))
	fmt.Fprintf(b, "  HGEN=$(printf '%%s' \"$META%d\" | sed -n 's/.*\"generation\":\\([0-9][0-9]*\\).*/\\1/p')\n", i)
	fmt.Fprintf(b, "  LGEN=$(cat %s 2>/dev/null || echo 0)\n", shellQuote(spec.StateDir+"/generation"))
	fmt.Fprintf(b, "  if [ -n \"$HGEN\" ] && [ \"$HGEN\" -gt \"$LGEN\" ] 2>/dev/null; then NEED%d=yes; fi\n", i)
	fmt.Fprintf(b, "else echo 'snapshots: the Hub could not be reached for %s, using the data on this node'; fi\n", pl.PluginID)
}

// writeSnapshotRestore emits the restore itself (inside a plugin's fresh start). A failed restore never fails the deploy.
func writeSnapshotRestore(b *strings.Builder, i int, pl ResolvedPlugin) {
	spec := pl.Snapshot
	tok := fmt.Sprintf("ASDL_PLUGIN_%d_SNAPTOKEN", i)
	fmt.Fprintf(b, "  if [ \"$NEED%d\" = yes ]; then\n", i)
	b.WriteString("    (\n")
	fmt.Fprintf(b, "    HGEN=$(printf '%%s' \"$META%d\" | sed -n 's/.*\"generation\":\\([0-9][0-9]*\\).*/\\1/p')\n", i)
	fmt.Fprintf(b, "    WANT=$(printf '%%s' \"$META%d\" | sed -n 's/.*\"sha256\":\"\\([0-9a-f]*\\)\".*/\\1/p')\n", i)
	b.WriteString("    TMP=$(mktemp)\n")
	fmt.Fprintf(b, "    if curl -fsS --max-time 300 -H \"Authorization: Bearer $%s\" -o \"$TMP\" %s; then\n", tok, shellQuote(spec.URL+"/latest"))
	b.WriteString("      GOT=$( (sha256sum \"$TMP\" 2>/dev/null || shasum -a 256 \"$TMP\") | cut -d' ' -f1)\n")
	b.WriteString("      if [ -n \"$WANT\" ] && [ \"$GOT\" = \"$WANT\" ]; then\n")
	// Unpacked the same way, as root inside a throwaway container, so the files come back owned as they were.
	fmt.Fprintf(b, "        mkdir -p %s && docker run --rm -i -v %s:/data --entrypoint sh %s -c %s < \"$TMP\" && mkdir -p %s && echo \"$HGEN\" > %s && echo \"snapshots: restored %s generation $HGEN\"\n",
		shellQuote(spec.DataDir), shellQuote(spec.DataDir), shellQuote(pl.Image),
		shellQuote("rm -rf /data/* /data/.[!.]* /data/..?* 2>/dev/null; tar -xzf - -C /data"),
		shellQuote(spec.StateDir), shellQuote(spec.StateDir+"/generation"), pl.PluginID)
	b.WriteString("      else echo 'snapshots: the download did not match its checksum, not restoring'; fi\n")
	b.WriteString("    else echo 'snapshots: could not download the snapshot, using the data on this node'; fi\n")
	b.WriteString("    rm -f \"$TMP\"\n")
	b.WriteString("    ) || true\n")
	b.WriteString("  fi\n")
}

// ---------- the service ----------

type SnapshotService struct {
	db *gorm.DB
	// redeploy restarts a project with its current settings (and plugins).
	redeploy func(*models.Project) error
}

func NewSnapshotService(db *gorm.DB, redeploy func(*models.Project) error) *SnapshotService {
	return &SnapshotService{db: db, redeploy: redeploy}
}

// attachedWithSnapshots lists a project's plugin attachments that have snapshots on.
func (s *SnapshotService) attachedWithSnapshots(project *models.Project) []ResolvedPlugin {
	var out []ResolvedPlugin
	for _, pl := range resolvePlugins(s.db, project) {
		if pl.Snapshot != nil {
			out = append(out, pl)
		}
	}
	return out
}

func (s *SnapshotService) newJob(project *models.Project, nodeID string, pl ResolvedPlugin, trigger string) (*models.Job, error) {
	fillSnapshot(project, &pl, "")
	if pl.Snapshot == nil {
		return nil, errors.New("snapshots are not set up on this Hub")
	}
	return newSnapshotJob(s.db, project, nodeID, pl, trigger)
}

// newSnapshotJob queues the snapshot script on a node. pl must already have its snapshot spec filled.
func newSnapshotJob(db *gorm.DB, project *models.Project, nodeID string, pl ResolvedPlugin, trigger string) (*models.Job, error) {
	id := uuid.New().String()
	script, env := BuildSnapshotCommand(pl, trigger, id)
	job := &models.Job{
		ID: id, NodeID: nodeID, Type: models.JobTypeCommand, Status: models.JobStatusPending,
		Command: script, Environment: env, Timeout: 600, MaxRetries: 0, CreatedAt: time.Now(),
		Payload: &models.JobPayload{Operation: snapshotJobMarker, ContainerName: pl.ContainerName},
	}
	if err := db.Create(job).Error; err != nil {
		return nil, err
	}
	return job, nil
}

// ---------- node-facing routes (mesh only, token protected) ----------

func (s *SnapshotService) authorize(c *gin.Context) (*models.Project, *ResolvedPlugin, bool) {
	if !snapshotsEnabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "snapshots are not set up on this Hub"})
		return nil, nil, false
	}
	projectID, pluginID := c.Param("project"), c.Param("plugin")
	given := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if !hmac.Equal([]byte(given), []byte(SnapshotToken(snapshotHub.Secret, projectID, pluginID))) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "bad token"})
		return nil, nil, false
	}
	var project models.Project
	if err := s.db.First(&project, "id = ?", projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no such project"})
		return nil, nil, false
	}
	for _, pl := range s.attachedWithSnapshots(&project) {
		if pl.PluginID == pluginID {
			pl := pl
			return &project, &pl, true
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "snapshots are off for this plugin"})
	return nil, nil, false
}

func (s *SnapshotService) newestMeta(projectID, pluginID string) *models.PluginSnapshot {
	var snap models.PluginSnapshot
	if s.db.Omit("data").Where("project_id = ? AND plugin_id = ?", projectID, pluginID).Order("generation DESC").First(&snap).Error != nil {
		return nil
	}
	return &snap
}

// Upload handles POST /snapshots/:project/:plugin from the node that hosts the project.
func (s *SnapshotService) Upload(c *gin.Context) {
	project, _, ok := s.authorize(c)
	if !ok {
		return
	}
	node, err := nodeByAddress(s.db, c)
	if err != nil || node.ID != project.NodeID {
		// A node the project has since moved away from must not overwrite the newer data.
		c.JSON(http.StatusConflict, gin.H{"error": "this node does not host the project"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, snapshotHub.MaxBytes))
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": fmt.Sprintf("snapshot is larger than %d MB", snapshotHub.MaxBytes>>20)})
		return
	}
	if len(body) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty snapshot"})
		return
	}
	trigger := c.GetHeader("X-Snapshot-Trigger")
	switch trigger {
	case models.SnapshotScheduled, models.SnapshotMove, models.SnapshotManual:
	default:
		trigger = models.SnapshotScheduled
	}
	sum := sha256.Sum256(body)
	snap, err := s.store(project.ID, c.Param("plugin"), node.ID, trigger, c.GetHeader("X-Job-Id"), body, hex.EncodeToString(sum[:]))
	if err != nil {
		log.Printf("⚠️ saving a snapshot of %s/%s failed: %v", project.Name, c.Param("plugin"), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save the snapshot"})
		return
	}
	log.Printf("📸 Snapshot %d of %s/%s saved (%d KB, %s)", snap.Generation, project.Name, snap.PluginID, snap.SizeBytes/1024, trigger)
	c.JSON(http.StatusCreated, gin.H{"generation": snap.Generation, "sha256": snap.SHA256, "size_bytes": snap.SizeBytes})
}

// store saves a snapshot as the newest generation and drops the oldest beyond the retention.
func (s *SnapshotService) store(projectID, pluginID, nodeID, trigger, jobID string, data []byte, sha string) (*models.PluginSnapshot, error) {
	snap := &models.PluginSnapshot{ID: uuid.New().String(), ProjectID: projectID, PluginID: pluginID, NodeID: nodeID, Trigger: trigger,
		JobID: jobID, SizeBytes: int64(len(data)), SHA256: sha, Data: data, CreatedAt: time.Now()}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var top int64
		tx.Model(&models.PluginSnapshot{}).Where("project_id = ? AND plugin_id = ?", projectID, pluginID).Select("COALESCE(MAX(generation), 0)").Scan(&top)
		snap.Generation = top + 1
		if err := tx.Create(snap).Error; err != nil {
			return err
		}
		var old []models.PluginSnapshot
		tx.Omit("data").Where("project_id = ? AND plugin_id = ?", projectID, pluginID).Order("generation DESC").Offset(snapshotHub.Keep).Find(&old)
		for _, o := range old {
			tx.Where("id = ?", o.ID).Delete(&models.PluginSnapshot{})
		}
		return nil
	})
	return snap, err
}

// Meta handles GET /snapshots/:project/:plugin/meta[?after_job=ID&wait=SECONDS]. With after_job it waits (up to
// wait seconds) until that job's snapshot has arrived or the job is over, so a move restores the final snapshot.
func (s *SnapshotService) Meta(c *gin.Context) {
	project, _, ok := s.authorize(c)
	if !ok {
		return
	}
	pluginID := c.Param("plugin")
	if after := c.Query("after_job"); after != "" {
		wait, _ := strconv.Atoi(c.Query("wait"))
		if wait > snapshotMoveWaitSeconds {
			wait = snapshotMoveWaitSeconds
		}
		deadline := time.Now().Add(time.Duration(wait) * time.Second)
		for time.Now().Before(deadline) && !s.jobSettled(project.ID, pluginID, after) {
			select {
			case <-c.Request.Context().Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
	snap := s.newestMeta(project.ID, pluginID)
	if snap == nil {
		c.JSON(http.StatusOK, gin.H{"generation": 0})
		return
	}
	c.JSON(http.StatusOK, gin.H{"generation": snap.Generation, "sha256": snap.SHA256, "size_bytes": snap.SizeBytes, "created_at": snap.CreatedAt})
}

// jobSettled: the snapshot job's data is in, or the job can't produce any more (finished, failed, gone).
func (s *SnapshotService) jobSettled(projectID, pluginID, jobID string) bool {
	var n int64
	s.db.Model(&models.PluginSnapshot{}).Where("project_id = ? AND plugin_id = ? AND job_id = ?", projectID, pluginID, jobID).Count(&n)
	if n > 0 {
		return true
	}
	var job models.Job
	if s.db.Select("status").First(&job, "id = ?", jobID).Error != nil {
		return true
	}
	return job.Status != models.JobStatusPending && job.Status != models.JobStatusRunning
}

// Latest handles GET /snapshots/:project/:plugin/latest: the newest snapshot's bytes.
func (s *SnapshotService) Latest(c *gin.Context) {
	project, _, ok := s.authorize(c)
	if !ok {
		return
	}
	var snap models.PluginSnapshot
	if err := s.db.Where("project_id = ? AND plugin_id = ?", project.ID, c.Param("plugin")).Order("generation DESC").First(&snap).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no snapshot yet"})
		return
	}
	c.Header("X-Generation", strconv.FormatInt(snap.Generation, 10))
	c.Header("X-SHA256", snap.SHA256)
	c.Data(http.StatusOK, "application/gzip", snap.Data)
}

// ---------- scheduler ----------

// StartScheduler queues snapshot jobs: every 30 seconds it looks at each plugin that has snapshots on and, when
// its newest snapshot is older than its interval and no snapshot job is already waiting, asks the project's node
// for a new one.
func (s *SnapshotService) StartScheduler() {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		log.Println("📸 Snapshot scheduler started")
		for range ticker.C {
			s.tick(time.Now())
		}
	}()
}

func (s *SnapshotService) tick(now time.Time) {
	if !snapshotsEnabled() {
		return
	}
	var projects []models.Project
	s.db.Where("status = ?", "running").Find(&projects)
	for i := range projects {
		project := &projects[i]
		plugins := s.attachedWithSnapshots(project)
		if len(plugins) == 0 {
			continue
		}
		var node models.Node
		if s.db.First(&node, "id = ?", project.NodeID).Error != nil || !node.Online {
			continue
		}
		for _, pl := range plugins {
			if last := s.newestMeta(project.ID, pl.PluginID); last != nil && now.Sub(last.CreatedAt) < time.Duration(pl.Snapshot.Minutes)*time.Minute {
				continue
			}
			if s.snapshotJobQueued(node.ID, pl.ContainerName) {
				continue
			}
			if _, err := s.newJob(project, node.ID, pl, models.SnapshotScheduled); err != nil {
				log.Printf("⚠️ could not queue a snapshot of %s/%s: %v", project.Name, pl.PluginID, err)
			}
		}
	}
}

func (s *SnapshotService) snapshotJobQueued(nodeID, container string) bool {
	var jobs []models.Job
	s.db.Select("id", "payload").Where("node_id = ? AND type = ? AND status IN ?", nodeID, models.JobTypeCommand,
		[]string{models.JobStatusPending, models.JobStatusRunning}).Find(&jobs)
	for _, j := range jobs {
		if j.Payload != nil && j.Payload.Operation == snapshotJobMarker && j.Payload.ContainerName == container {
			return true
		}
	}
	return false
}

// ---------- dashboard and CLI routes ----------

type snapshotPluginView struct {
	PluginID        string                  `json:"plugin_id"`
	Persist         bool                    `json:"persist"`
	Snapshots       bool                    `json:"snapshots"`
	IntervalMinutes int                     `json:"interval_minutes"`
	LastSnapshotAt  *time.Time              `json:"last_snapshot_at"`
	AgeSeconds      *int64                  `json:"age_seconds"`
	Healthy         bool                    `json:"healthy"`
	Items           []models.PluginSnapshot `json:"items"`
}

// List handles GET /projects/:id/snapshots: every plugin that can keep data, with its snapshots (metadata only).
func (s *SnapshotService) List(c *gin.Context) {
	var project models.Project
	if err := s.db.First(&project, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	catalog := PluginCatalog(s.db)
	var attached []models.ProjectPlugin
	s.db.Where("project_id = ?", project.ID).Order("plugin_id").Find(&attached)
	views := []snapshotPluginView{}
	for _, a := range attached {
		m, ok := catalog[a.PluginID]
		if !ok || m.Persist == nil {
			continue
		}
		pl := m.Resolve(&project, a.Vars)
		v := snapshotPluginView{PluginID: a.PluginID, Persist: len(pl.Volumes) > 0, Snapshots: pl.Snapshot != nil, Items: []models.PluginSnapshot{}}
		if pl.Snapshot != nil {
			v.IntervalMinutes = pl.Snapshot.Minutes
		}
		s.db.Omit("data").Where("project_id = ? AND plugin_id = ?", project.ID, a.PluginID).Order("generation DESC").Find(&v.Items)
		if len(v.Items) > 0 {
			t := v.Items[0].CreatedAt
			age := int64(time.Since(t).Seconds())
			v.LastSnapshotAt, v.AgeSeconds = &t, &age
		}
		// Quiet for longer than three intervals (and a little slack) means something stopped working.
		v.Healthy = pl.Snapshot != nil && v.AgeSeconds != nil && *v.AgeSeconds <= int64(pl.Snapshot.Minutes*3*60+120)
		views = append(views, v)
	}
	c.JSON(http.StatusOK, gin.H{"enabled": snapshotsEnabled(), "keep": snapshotHub.Keep, "max_mb": snapshotHub.MaxBytes >> 20, "plugins": views})
}

// TakeNow handles POST /projects/:id/snapshots {"plugin_id"}: queue a snapshot right away.
func (s *SnapshotService) TakeNow(c *gin.Context) {
	var project models.Project
	if err := s.db.First(&project, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	var req struct {
		PluginID string `json:"plugin_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var node models.Node
	if s.db.First(&node, "id = ?", project.NodeID).Error != nil || !node.Online {
		c.JSON(http.StatusConflict, gin.H{"error": "the project's node is offline"})
		return
	}
	for _, pl := range s.attachedWithSnapshots(&project) {
		if pl.PluginID != req.PluginID {
			continue
		}
		job, err := s.newJob(&project, node.ID, pl, models.SnapshotManual)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"job_id": job.ID, "message": "snapshot queued"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": "snapshots are not turned on for that plugin"})
}

func (s *SnapshotService) findSnapshot(c *gin.Context, withData bool) (*models.Project, *models.PluginSnapshot, bool) {
	var project models.Project
	if err := s.db.First(&project, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return nil, nil, false
	}
	var snap models.PluginSnapshot
	q := s.db.Where("id = ? AND project_id = ?", c.Param("snapshot"), project.ID)
	if !withData {
		q = q.Omit("data")
	}
	if err := q.First(&snap).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "snapshot not found"})
		return nil, nil, false
	}
	return &project, &snap, true
}

// Delete handles DELETE /projects/:id/snapshots/:snapshot.
func (s *SnapshotService) Delete(c *gin.Context) {
	_, snap, ok := s.findSnapshot(c, false)
	if !ok {
		return
	}
	s.db.Where("id = ?", snap.ID).Delete(&models.PluginSnapshot{})
	c.JSON(http.StatusOK, gin.H{"message": "snapshot deleted"})
}

// DeleteAll handles DELETE /projects/:id/snapshots?plugin_id=redis: every stored snapshot of that plugin
// (or of the whole project without plugin_id). Used after snapshots are turned off, if the data should go too.
func (s *SnapshotService) DeleteAll(c *gin.Context) {
	var project models.Project
	if err := s.db.First(&project, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	q := s.db.Where("project_id = ?", project.ID)
	if pid := c.Query("plugin_id"); pid != "" {
		q = q.Where("plugin_id = ?", pid)
	}
	res := q.Delete(&models.PluginSnapshot{})
	c.JSON(http.StatusOK, gin.H{"deleted": res.RowsAffected})
}

// Download handles GET /projects/:id/snapshots/:snapshot/download.
func (s *SnapshotService) Download(c *gin.Context) {
	project, snap, ok := s.findSnapshot(c, true)
	if !ok {
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s-gen%d.tar.gz"`, project.Name, snap.PluginID, snap.Generation))
	c.Data(http.StatusOK, "application/gzip", snap.Data)
}

// Restore handles POST /projects/:id/snapshots/:snapshot/restore: the chosen snapshot becomes the newest
// generation and the project is redeployed, so the plugin comes back up with that data.
func (s *SnapshotService) Restore(c *gin.Context) {
	project, snap, ok := s.findSnapshot(c, true)
	if !ok {
		return
	}
	if len(s.attachedWithSnapshots(project)) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "snapshots are not turned on for this project"})
		return
	}
	copyOf, err := s.store(project.ID, snap.PluginID, snap.NodeID, models.SnapshotRestore, "", snap.Data, snap.SHA256)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not restore: " + err.Error()})
		return
	}
	log.Printf("♻️ Restoring snapshot %d of %s/%s as generation %d", snap.Generation, project.Name, snap.PluginID, copyOf.Generation)
	redeploying := false
	if s.redeploy != nil {
		redeploying = s.redeploy(project) == nil
	}
	c.JSON(http.StatusAccepted, gin.H{"generation": copyOf.Generation, "redeploying": redeploying})
}

// RemoveForProject drops a project's snapshots (used when a project or an attachment goes away).
func (s *SnapshotService) RemoveForProject(projectID string) {
	s.db.Where("project_id = ?", projectID).Delete(&models.PluginSnapshot{})
}
