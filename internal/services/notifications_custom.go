package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/asdl/hub/internal/models"
)

// Custom notification plugins are stored as one JSON list in settings.
const customNotificationsKey = "notifications.custom.v1"

// NotifyRequest is the HTTP request a custom notification plugin sends.
// URL, Headers and Body are Go templates over the event:
//
//	{{.title}} {{.message}} {{.text}} {{.details}} {{.url}} {{.event}}
//	{{.level}} {{.emoji}} {{.color}} (0xRRGGBB as a number) {{.hex}} ("#rrggbb")
//	{{.project}} {{.node}} {{.time}} {{.fields}} ([{name, value}])
//	{{.config.KEY}} (the plugin's fields)
//
// and {{json X}} writes X as a JSON value (quoted and escaped), so a body is
// built like {"text": {{json .text}}}.
type NotifyRequest struct {
	Method  string            `json:"method,omitempty"` // POST if empty
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

var notifyTemplateFuncs = template.FuncMap{
	"json": func(v interface{}) (string, error) {
		b, err := json.Marshal(v)
		return string(b), err
	},
}

var (
	notifyTypeIDRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,29}$`)
	notifyFieldKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,39}$`)
	headerNameRe     = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
)

func parseNotifyTemplate(name, text string) (*template.Template, error) {
	t, err := template.New(name).Funcs(notifyTemplateFuncs).Option("missingkey=zero").Parse(text)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", name, err)
	}
	return t, nil
}

// validateCustomType checks a custom notification plugin, including that its
// templates parse.
func validateCustomType(t *NotificationType) error {
	if !notifyTypeIDRe.MatchString(t.ID) {
		return fmt.Errorf("id must be lowercase letters, digits and '-' (e.g. teams)")
	}
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("name is required")
	}
	seen := map[string]bool{}
	for _, f := range t.Fields {
		if !notifyFieldKeyRe.MatchString(f.Key) || seen[f.Key] {
			return fmt.Errorf("invalid or duplicate field key %q", f.Key)
		}
		if strings.TrimSpace(f.Label) == "" {
			return fmt.Errorf("field %s needs a label", f.Key)
		}
		seen[f.Key] = true
	}
	r := t.Request
	if r == nil || strings.TrimSpace(r.URL) == "" {
		return fmt.Errorf("request.url is required")
	}
	switch strings.ToUpper(r.Method) {
	case "", "POST", "PUT", "PATCH", "GET":
	default:
		return fmt.Errorf("request.method must be POST, PUT, PATCH or GET")
	}
	if _, err := parseNotifyTemplate("request.url", r.URL); err != nil {
		return err
	}
	if _, err := parseNotifyTemplate("request.body", r.Body); err != nil {
		return err
	}
	for k, v := range r.Headers {
		if !headerNameRe.MatchString(k) {
			return fmt.Errorf("invalid header name %q", k)
		}
		if _, err := parseNotifyTemplate("header "+k, v); err != nil {
			return err
		}
	}
	return nil
}

// eventTemplateData is what a custom plugin's templates see.
func eventTemplateData(ev Event, cfg map[string]string) map[string]interface{} {
	fields := ev.Fields
	if fields == nil {
		fields = []EventField{}
	}
	return map[string]interface{}{
		"event":   ev.Type,
		"level":   ev.Level,
		"title":   ev.Title,
		"message": ev.Message,
		"text":    plainText(ev),
		"details": ev.Details,
		"url":     ev.URL,
		"project": ev.Project,
		"node":    ev.Node,
		"time":    ev.Time.UTC().Format(time.RFC3339),
		"fields":  fields,
		"emoji":   levelEmoji[ev.Level],
		"color":   levelColors[ev.Level],
		"hex":     fmt.Sprintf("#%06x", levelColors[ev.Level]),
		"config":  cfg,
	}
}

// plainText is the whole event as a few lines of text.
func plainText(ev Event) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n%s", levelEmoji[ev.Level], ev.Title, ev.Message)
	for _, f := range ev.Fields {
		fmt.Fprintf(&b, "\n%s: %s", f.Name, f.Value)
	}
	if ev.Details != "" {
		fmt.Fprintf(&b, "\n\n%s", ev.Details)
	}
	if ev.URL != "" {
		fmt.Fprintf(&b, "\n%s", ev.URL)
	}
	return b.String()
}

func render(name, text string, data interface{}) (string, error) {
	t, err := parseNotifyTemplate(name, text)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", fmt.Errorf("%s: %v", name, err)
	}
	return b.String(), nil
}

func templateSender(r NotifyRequest) sendFunc {
	return func(ctx context.Context, n *notifier, cfg map[string]string, ev Event) error {
		data := eventTemplateData(ev, cfg)
		target, err := render("request.url", r.URL, data)
		if err != nil {
			return err
		}
		if err := checkHTTPURL(strings.TrimSpace(target)); err != nil {
			return fmt.Errorf("request.url doesn't give an http(s) URL")
		}
		body, err := render("request.body", r.Body, data)
		if err != nil {
			return err
		}
		method := strings.ToUpper(r.Method)
		if method == "" {
			method = http.MethodPost
		}
		req, err := http.NewRequestWithContext(ctx, method, strings.TrimSpace(target), strings.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "ASDL-Hub")
		if strings.HasPrefix(strings.TrimSpace(body), "{") || strings.HasPrefix(strings.TrimSpace(body), "[") {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range r.Headers {
			hv, err := render("header "+k, v, data)
			if err != nil {
				return err
			}
			// An empty value (e.g. an optional token left out) sends no header.
			if hv = strings.TrimSpace(hv); hv != "" {
				req.Header.Set(k, hv)
			}
		}
		return n.do(req)
	}
}

func customNotificationTypes(db *gorm.DB) []NotificationType {
	var st models.Setting
	if db.First(&st, "key = ?", customNotificationsKey).Error != nil {
		return nil
	}
	var out []NotificationType
	if err := json.Unmarshal([]byte(st.Value), &out); err != nil {
		log.Printf("⚠️ custom notification plugins: %v", err)
		return nil
	}
	return out
}

func saveCustomNotificationTypes(db *gorm.DB, list []NotificationType) error {
	b, _ := json.Marshal(list)
	return db.Clauses(clause.OnConflict{UpdateAll: true}).
		Create(&models.Setting{Key: customNotificationsKey, Value: string(b), UpdatedAt: time.Now()}).Error
}

// notificationCatalog returns every notification plugin: built-in, then custom.
func notificationCatalog(db *gorm.DB) []NotificationType {
	out := append([]NotificationType(nil), builtinNotificationTypes...)
	for _, t := range customNotificationTypes(db) {
		if t.Request == nil {
			continue
		}
		t.Builtin = false
		t.send = templateSender(*t.Request)
		out = append(out, t)
	}
	return out
}

func (s *NotificationService) typeByID(id string) *NotificationType {
	for _, t := range notificationCatalog(s.db) {
		if t.ID == id {
			t := t
			return &t
		}
	}
	return nil
}

// AddType handles POST /notifications/types: adds (or replaces) a custom
// notification plugin.
func (s *NotificationService) AddType(c *gin.Context) {
	var t NotificationType
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}
	t.Builtin = false
	if err := validateCustomType(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	for _, b := range builtinNotificationTypes {
		if b.ID == t.ID {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("%q is a built-in notification plugin", t.ID)})
			return
		}
	}
	list := customNotificationTypes(s.db)
	replaced := false
	for i := range list {
		if list[i].ID == t.ID {
			list[i], replaced = t, true
		}
	}
	if !replaced {
		list = append(list, t)
	}
	if err := saveCustomNotificationTypes(s.db, list); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, t)
}

// RemoveType handles DELETE /notifications/types/:id. A plugin that channels
// still use can't be removed.
func (s *NotificationService) RemoveType(c *gin.Context) {
	id := c.Param("id")
	var inUse int64
	s.db.Model(&models.NotificationChannel{}).Where("type = ?", id).Count(&inUse)
	if inUse > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("%d channel(s) use this plugin; remove them first", inUse)})
		return
	}
	list := customNotificationTypes(s.db)
	out := list[:0]
	for _, t := range list {
		if t.ID != id {
			out = append(out, t)
		}
	}
	if err := saveCustomNotificationTypes(s.db, out); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
