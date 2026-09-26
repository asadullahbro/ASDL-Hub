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
	upgrading string // target version while an upgrade is running
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

func canSelfUpdate() bool {
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
	})
}

// Update handles POST /system/update: upgrade to the latest release.
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
			"error": "this install can't update itself yet; run the installer once: curl -fsSL https://get.asdl.website/asdl-hub | sudo bash",
		})
		return
	case s.upgrading != "":
		s.mu.Unlock()
		c.JSON(http.StatusAccepted, gin.H{"upgrading": s.upgrading})
		return
	}
	s.upgrading = target
	s.mu.Unlock()

	// The helper hands the work to systemd and returns straight away, so it
	// survives the installer restarting this process.
	out, err := exec.Command("sudo", "-n", UpgradeHelper, target).CombinedOutput()
	if err != nil {
		s.mu.Lock()
		s.upgrading = ""
		s.mu.Unlock()
		log.Printf("❌ Could not start upgrade to %s: %v: %s", target, err, out)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("could not start the upgrade: %s", out)})
		return
	}
	log.Printf("⬆️ Upgrading ASDL Hub %s → %s (log: /var/log/asdl-hub-upgrade.log)", s.current, target)
	c.JSON(http.StatusAccepted, gin.H{"upgrading": target})
}
