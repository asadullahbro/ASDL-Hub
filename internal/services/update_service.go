package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// UpgradeHelper is installed by the installer and allowed via sudo. Given a
// release tag it downloads that release's installer, verifies it and runs it
// detached from the Hub (the installer restarts the Hub).
const UpgradeHelper = "/usr/local/lib/asdl-hub/upgrade"

const (
	releasesAPI         = "https://api.github.com/repos/asadullahbro/ASDL-Hub/releases/latest"
	updateCheckInterval = time.Hour
)

var semverRe = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// UpdateService tells the dashboard whether a newer Hub release exists and
// can start the upgrade.
type UpdateService struct {
	current string
	client  *http.Client

	mu        sync.RWMutex
	latest    string
	notes     string
	url       string
	checkedAt time.Time
	checkErr  string
	upgrading string // target version while an upgrade is running (or waiting to start)
	announced string // the release last announced to notification channels
	// waitingFor is how many jobs the upgrade is waiting on before it restarts the Hub; 0 once it has started.
	waitingFor int

	inFlight func() int // jobs on online nodes that are queued or running; nil counts none
}

// upgradeWaitLimit is the longest an update waits for jobs to finish before going ahead anyway, so one stuck job
// can't hold back updates for good.
const upgradeWaitLimit = 30 * time.Minute

// upgradeWaitPoll is how often a waiting update checks the jobs again.
var upgradeWaitPoll = 15 * time.Second

// SetInFlightJobs tells the update service how to count the jobs an upgrade should wait for. While the Hub
// restarts, agents can't claim jobs or report finished ones, and a report that doesn't get through leaves the
// Hub thinking the job never ended.
func (s *UpdateService) SetInFlightJobs(count func() int) { s.inFlight = count }

func (s *UpdateService) jobsInFlight() int {
	if s.inFlight == nil {
		return 0
	}
	return s.inFlight()
}

func NewUpdateService(current string) *UpdateService {
	return &UpdateService{current: current, client: &http.Client{Timeout: 15 * time.Second}}
}

// Start checks for a new release now and then every hour.
func (s *UpdateService) Start() {
	go func() {
		for {
			s.check()
			time.Sleep(updateCheckInterval)
		}
	}()
}

func (s *UpdateService) check() {
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, releasesAPI, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := s.client.Do(req)
	if err != nil {
		s.setCheckErr(err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		s.setCheckErr(fmt.Sprintf("GitHub returned %d", resp.StatusCode))
		return
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		s.setCheckErr(err.Error())
		return
	}
	s.mu.Lock()
	s.latest, s.notes, s.url = rel.TagName, rel.Body, rel.HTMLURL
	s.checkedAt, s.checkErr = time.Now(), ""
	s.mu.Unlock()
	if newerVersion(rel.TagName, s.current) {
		log.Printf("⬆️ ASDL Hub %s is available (running %s)", rel.TagName, s.current)
		s.mu.Lock()
		first := s.announced != rel.TagName
		s.announced = rel.TagName
		s.mu.Unlock()
		if first {
			notify(Event{
				Type: EventHubUpdate, Level: LevelInfo,
				Title:   "ASDL Hub " + rel.TagName + " is available",
				Message: fmt.Sprintf("You're running %s. Install it from the dashboard with Update now.", s.current),
				Fields:  []EventField{{Name: "Release notes", Value: rel.HTMLURL}},
				URL:     link("/settings"),
			})
		}
	}
}

func (s *UpdateService) setCheckErr(msg string) {
	s.mu.Lock()
	s.checkedAt, s.checkErr = time.Now(), msg
	s.mu.Unlock()
	log.Printf("⚠️ Could not check for Hub updates: %s", msg)
}

// newerVersion reports whether a is a later release than b. Versions that
// aren't vX.Y.Z (e.g. development builds) never count as newer or older.
func newerVersion(a, b string) bool {
	pa, pb := semverRe.FindStringSubmatch(a), semverRe.FindStringSubmatch(b)
	if pa == nil || pb == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x > y
		}
	}
	return false
}

// canSelfUpdate reports whether the upgrade helper is installed (a variable so tests can stand in for it).
var canSelfUpdate = func() bool {
	info, err := os.Stat(UpgradeHelper)
	return err == nil && !info.IsDir()
}

// Status handles GET /system/version.
func (s *UpdateService) Status(c *gin.Context) {
	if c.Query("refresh") == "1" {
		s.check()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"current":          s.current,
		"latest":           s.latest,
		"update_available": newerVersion(s.latest, s.current),
		"release_url":      s.url,
		"notes":            s.notes,
		"checked_at":       s.checkedAt,
		"check_error":      s.checkErr,
		"can_update":       canSelfUpdate(),
		"upgrading":        s.upgrading,
		"waiting_for_jobs": s.waitingFor,
	})
}

// Update handles POST /system/update: upgrade to the latest release. While jobs are queued or running on online
// nodes it waits for them first (at most upgradeWaitLimit) and answers straight away with how many; ?now=1
// upgrades without waiting.
func (s *UpdateService) Update(c *gin.Context) {
	s.mu.Lock()
	target := s.latest
	switch {
	case !newerVersion(target, s.current):
		s.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "already on the latest version"})
		return
	case !canSelfUpdate():
		s.mu.Unlock()
		c.JSON(http.StatusNotImplemented, gin.H{
			"error": "this install can't update itself yet; run the installer once: curl -fsSL https://get.asdl.website/asdl-hub | sudo ASDL_INSTALL=hub bash",
		})
		return
	case s.upgrading != "":
		s.mu.Unlock()
		c.JSON(http.StatusAccepted, gin.H{"upgrading": s.upgrading})
		return
	}
	s.upgrading = target
	s.mu.Unlock()

	if n := s.jobsInFlight(); n > 0 && c.Query("now") != "1" {
		s.mu.Lock()
		s.waitingFor = n
		s.mu.Unlock()
		log.Printf("⏳ Updating ASDL Hub to %s once %d job(s) finish (at most %s)", target, n, upgradeWaitLimit)
		go s.upgradeWhenIdle(target, time.Now().Add(upgradeWaitLimit))
		c.JSON(http.StatusAccepted, gin.H{"upgrading": target, "waiting_for_jobs": n})
		return
	}

	if err := s.startUpgrade(target); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"upgrading": target})
}

// upgradeWhenIdle starts the upgrade once no jobs are in flight, or at deadline.
func (s *UpdateService) upgradeWhenIdle(target string, deadline time.Time) {
	for {
		n := s.jobsInFlight()
		s.mu.Lock()
		s.waitingFor = n
		s.mu.Unlock()
		if n == 0 {
			break
		}
		if !time.Now().Before(deadline) {
			log.Printf("⚠️ %d job(s) still running after %s; updating anyway", n, upgradeWaitLimit)
			break
		}
		time.Sleep(upgradeWaitPoll)
	}
	s.startUpgrade(target)
}

// startUpgrade runs the upgrade helper. The helper hands the work to systemd and returns straight away, so it
// survives the installer restarting this process.
func (s *UpdateService) startUpgrade(target string) error {
	s.mu.Lock()
	s.waitingFor = 0
	s.mu.Unlock()
	out, err := exec.Command("sudo", "-n", UpgradeHelper, target).CombinedOutput()
	if err != nil {
		s.mu.Lock()
		s.upgrading = ""
		s.mu.Unlock()
		log.Printf("❌ Could not start upgrade to %s: %v: %s", target, err, out)
		return fmt.Errorf("could not start the upgrade: %s", out)
	}
	log.Printf("⬆️ Upgrading ASDL Hub %s → %s (log: /var/log/asdl-hub-upgrade.log)", s.current, target)
	return nil
}
