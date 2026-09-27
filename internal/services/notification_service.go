package services

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

const (
	notifyTimeout = 15 * time.Second
	// The same event about the same thing within this window is sent once,
	// so a flapping node doesn't flood a channel.
	notifyDedupWindow = time.Minute
)

// NotificationService stores notification channels and delivers events to
// them in the background.
type NotificationService struct {
	db *gorm.DB
	n  *notifier

	mu     sync.Mutex
	recent map[string]time.Time
}

// hubNotifications receives the events services emit; nil (e.g. in tests)
// means events are dropped.
var hubNotifications *NotificationService

func NewNotificationService(db *gorm.DB, hubURL string) *NotificationService {
	s := &NotificationService{
		db:     db,
		n:      &notifier{client: &http.Client{Timeout: notifyTimeout}, hubURL: strings.TrimRight(hubURL, "/")},
		recent: map[string]time.Time{},
	}
	hubNotifications = s
	return s
}

// notify sends ev to every channel that wants it, in the background.
func notify(ev Event) {
	if s := hubNotifications; s != nil {
		go s.Emit(ev)
	}
}

// link returns a dashboard URL for path, or "" if the Hub's address isn't known.
func link(path string) string {
	if s := hubNotifications; s != nil && s.n.hubURL != "" {
		return s.n.hubURL + path
	}
	return ""
}

func (s *NotificationService) Emit(ev Event) {
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	key := ev.Type + "|" + ev.Project + "|" + ev.Node + "|" + ev.Title
	s.mu.Lock()
	for k, t := range s.recent {
		if time.Since(t) > notifyDedupWindow {
			delete(s.recent, k)
		}
	}
	if _, dup := s.recent[key]; dup {
		s.mu.Unlock()
		return
	}
	s.recent[key] = time.Now()
	s.mu.Unlock()

	var channels []models.NotificationChannel
	s.db.Where("enabled = ?", true).Find(&channels)
	for i := range channels {
		ch := &channels[i]
		if !wantsEvent(ch, ev) {
			continue
		}
		go func() {
			err := s.deliver(ch, ev)
			if err != nil {
				// One retry covers a blip in the network or the service.
				time.Sleep(5 * time.Second)
				err = s.deliver(ch, ev)
			}
			if err != nil {
				log.Printf("⚠️ Notification %s to %s (%s) failed: %v", ev.Type, ch.Name, ch.Type, err)
			}
		}()
	}
}

func wantsEvent(ch *models.NotificationChannel, ev Event) bool {
	if ev.Type == EventTest {
		return true
	}
	found := false
	for _, e := range ch.Events {
		if e == ev.Type {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	if ev.ProjectID == "" || len(ch.Projects) == 0 {
		return true
	}
	for _, p := range ch.Projects {
		if p == ev.ProjectID {
			return true
		}
	}
	return false
}

// deliver sends ev to ch now and records the outcome on the channel.
func (s *NotificationService) deliver(ch *models.NotificationChannel, ev Event) error {
	t := s.typeByID(ch.Type)
	if t == nil {
		return fmt.Errorf("the %q notification plugin no longer exists", ch.Type)
	}
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	err := t.send(ctx, s.n, configMap(ch.Config), ev)
	updates := map[string]interface{}{"last_error": ""}
	if err != nil {
		updates["last_error"] = truncate(err.Error(), 500)
	} else {
		updates["last_sent_at"] = time.Now()
	}
	s.db.Model(&models.NotificationChannel{}).Where("id = ?", ch.ID).Updates(updates)
	return err
}

func configMap(vars []models.EnvVar) map[string]string {
	m := make(map[string]string, len(vars))
	for _, v := range vars {
		m[v.Key] = v.Value
	}
	return m
}

// --- HTTP handlers (admin only) ---

type channelView struct {
	models.NotificationChannel
	// Config with secret values masked.
	Config []models.EnvVar `json:"config"`
}

func viewChannel(ch models.NotificationChannel, t *NotificationType) channelView {
	secret := map[string]bool{}
	if t != nil {
		for _, f := range t.Fields {
			secret[f.Key] = f.Secret
		}
	}
	cfg := make([]models.EnvVar, 0, len(ch.Config))
	for _, v := range ch.Config {
		if secret[v.Key] && v.Value != "" {
			v.Value = models.MaskedValue
		}
		cfg = append(cfg, v)
	}
	if ch.Events == nil {
		ch.Events = []string{}
	}
	if ch.Projects == nil {
		ch.Projects = []string{}
	}
	return channelView{NotificationChannel: ch, Config: cfg}
}

// Types handles GET /notifications/types: channel types and events.
func (s *NotificationService) Types(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"types": notificationCatalog(s.db), "events": NotificationEvents})
}

func (s *NotificationService) List(c *gin.Context) {
	var channels []models.NotificationChannel
	s.db.Order("created_at").Find(&channels)
	out := make([]channelView, 0, len(channels))
	for _, ch := range channels {
		out = append(out, viewChannel(ch, s.typeByID(ch.Type)))
	}
	c.JSON(http.StatusOK, out)
}

type channelRequest struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Config   []models.EnvVar `json:"config"`
	Events   []string        `json:"events"`
	Projects []string        `json:"projects"`
	Enabled  *bool           `json:"enabled"`
}

// apply validates req and copies it onto ch. Masked secret values keep the
// value ch already has.
func (s *NotificationService) apply(ch *models.NotificationChannel, req channelRequest) error {
	t := s.typeByID(ch.Type)
	if t == nil {
		return fmt.Errorf("unknown notification plugin %q", ch.Type)
	}
	if req.Name = strings.TrimSpace(req.Name); req.Name != "" {
		ch.Name = truncate(req.Name, 100)
	}
	if ch.Name == "" {
		ch.Name = t.Name
	}
	if req.Config != nil {
		given := configMap(models.MergeMasked(req.Config, ch.Config))
		cfg := make([]models.EnvVar, 0, len(t.Fields))
		for _, f := range t.Fields {
			v := strings.TrimSpace(given[f.Key])
			if v == "" {
				v = f.Default
			}
			if v == "" && f.Required {
				return fmt.Errorf("%s is required", f.Label)
			}
			cfg = append(cfg, models.EnvVar{Key: f.Key, Value: v})
		}
		if t.check != nil {
			if err := t.check(configMap(cfg)); err != nil {
				return err
			}
		}
		ch.Config = cfg
	}
	if req.Events != nil {
		events := []string{}
		for _, e := range req.Events {
			if !knownEvent(e) {
				return fmt.Errorf("unknown event %q", e)
			}
			events = append(events, e)
		}
		ch.Events = events
	}
	if req.Projects != nil {
		ch.Projects = req.Projects
	}
	if req.Enabled != nil {
		ch.Enabled = *req.Enabled
	}
	return nil
}

func (s *NotificationService) Create(c *gin.Context) {
	var req channelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ch := models.NotificationChannel{ID: uuid.New().String(), Type: req.Type, Enabled: true}
	if req.Events == nil {
		for _, e := range NotificationEvents {
			req.Events = append(req.Events, e.ID)
		}
	}
	if req.Config == nil {
		req.Config = []models.EnvVar{}
	}
	if err := s.apply(&ch, req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.db.Create(&ch).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, viewChannel(ch, s.typeByID(ch.Type)))
}

func (s *NotificationService) Update(c *gin.Context) {
	var ch models.NotificationChannel
	if err := s.db.First(&ch, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return
	}
	var req channelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Type = ""
	if err := s.apply(&ch, req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.db.Save(&ch).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, viewChannel(ch, s.typeByID(ch.Type)))
}

func (s *NotificationService) Delete(c *gin.Context) {
	s.db.Delete(&models.NotificationChannel{}, "id = ?", c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// Test handles POST /notifications/:id/test: sends a test message now and
// returns what the service answered.
func (s *NotificationService) Test(c *gin.Context) {
	var ch models.NotificationChannel
	if err := s.db.First(&ch, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return
	}
	ev := Event{
		Type:    EventTest,
		Level:   LevelSuccess,
		Title:   "Notifications work",
		Message: fmt.Sprintf("This is a test from ASDL Hub. \"%s\" will get the events chosen for it.", ch.Name),
		Fields:  []EventField{{Name: "Channel", Value: ch.Name}, {Name: "Events", Value: fmt.Sprint(len(ch.Events))}},
		URL:     link("/plugins?tab=notifications"),
		Time:    time.Now(),
	}
	if err := s.deliver(&ch, ev); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sent": true})
}
