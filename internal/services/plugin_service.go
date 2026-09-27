package services

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/asdl/hub/internal/models"
)

// Plugins are optional features listed in the dashboard. The Hub only ships
// their description: what a plugin needs to run (containers, timers, scripts)
// is fetched onto the nodes you choose when you install it, so Hubs and nodes
// that don't use a plugin carry none of it. The installer isn't built yet, so
// installing is refused for now; Installed records a plugin set up by hand.
type Plugin struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Footprint says what installing puts where, so nothing arrives unannounced.
	Footprint   string          `json:"footprint"`
	Status      string          `json:"status"` // "available" or "coming_soon"
	Installable bool            `json:"installable"`
	Installed   bool            `json:"installed"`
	Config      json.RawMessage `json:"config,omitempty"`
	// Custom plugins are added by an admin; Source is where their files live.
	Custom bool   `json:"custom,omitempty"`
	Source string `json:"source,omitempty"`
}

var pluginRegistry = []Plugin{
	{
		ID:   "databases",
		Name: "Databases",
		Description: "Keep track of the databases your apps depend on: where the primary runs, " +
			"which node holds a live standby, and which projects use it.",
		Footprint: "On the primary's node: a nightly backup timer and a replication access keeper. " +
			"On the standby node: a Postgres container (same image as the primary) and its firewall rule.",
		Status: "available",
	},
	{
		ID:   "notifications",
		Name: "Notifications",
		Description: "Get told when a node goes offline, an app fails over, a deploy fails or an " +
			"update is available — on Discord, ntfy or email.",
		Footprint: "Nothing on the nodes; the Hub sends the messages.",
		Status:    "coming_soon",
	},
}

// DatabaseEntry is one database the Databases plugin knows about.
type DatabaseEntry struct {
	Name            string   `json:"name"`
	Engine          string   `json:"engine"` // e.g. "Supabase (Postgres 15)"
	PrimaryNode     string   `json:"primary_node"`
	PrimaryEndpoint string   `json:"primary_endpoint"` // host:port on the mesh
	StandbyNode     string   `json:"standby_node,omitempty"`
	StandbyEndpoint string   `json:"standby_endpoint,omitempty"`
	Backups         string   `json:"backups,omitempty"` // where and how often
	Projects        []string `json:"projects"`          // project names using it
	Notes           string   `json:"notes,omitempty"`
}

var (
	endpointRe = regexp.MustCompile(`^[A-Za-z0-9.-]+:\d{1,5}$`)
	pluginIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,39}$`)
)

const customPluginsKey = "plugins.custom"

type PluginService struct {
	db *gorm.DB
}

func NewPluginService(db *gorm.DB) *PluginService {
	return &PluginService{db: db}
}

func pluginKey(id, field string) string { return "plugin." + id + "." + field }

func (s *PluginService) setting(key string) (string, bool) {
	var st models.Setting
	if err := s.db.First(&st, "key = ?", key).Error; err != nil {
		return "", false
	}
	return st.Value, true
}

func (s *PluginService) save(key, value string) error {
	return s.db.Clauses(clause.OnConflict{UpdateAll: true}).
		Create(&models.Setting{Key: key, Value: value, UpdatedAt: time.Now()}).Error
}

func (s *PluginService) customPlugins() []Plugin {
	var out []Plugin
	if v, ok := s.setting(customPluginsKey); ok {
		_ = json.Unmarshal([]byte(v), &out)
	}
	return out
}

func (s *PluginService) all() []Plugin {
	return append(append([]Plugin{}, pluginRegistry...), s.customPlugins()...)
}

func (s *PluginService) find(id string) (Plugin, bool) {
	for _, p := range s.all() {
		if p.ID == id {
			return p, true
		}
	}
	return Plugin{}, false
}

func (s *PluginService) load(p Plugin) Plugin {
	if v, ok := s.setting(pluginKey(p.ID, "installed")); ok {
		p.Installed = v == "true"
	}
	if v, ok := s.setting(pluginKey(p.ID, "config")); ok && json.Valid([]byte(v)) {
		p.Config = json.RawMessage(v)
	}
	return p
}

// List handles GET /plugins.
func (s *PluginService) List(c *gin.Context) {
	out := []Plugin{}
	for _, p := range s.all() {
		out = append(out, s.load(p))
	}
	c.JSON(http.StatusOK, out)
}

// SetInstalled handles PUT /plugins/:id {"installed": bool}. Marking a
// plugin installed is only for one set up by hand, with
// {"installed": true, "manual": true}; installing through the Hub needs the
// installer, which doesn't exist yet.
func (s *PluginService) SetInstalled(c *gin.Context) {
	p, ok := s.find(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown plugin"})
		return
	}
	var req struct {
		Installed *bool `json:"installed"`
		Manual    bool  `json:"manual"`
	}
	if err := c.BindJSON(&req); err != nil || req.Installed == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": `send {"installed": true|false}`})
		return
	}
	if *req.Installed {
		switch {
		case p.Status != "available":
			c.JSON(http.StatusConflict, gin.H{"error": p.Name + " isn't available yet"})
			return
		case !p.Installable && !req.Manual:
			c.JSON(http.StatusNotImplemented, gin.H{"error": "installing plugins from the Hub isn't available yet"})
			return
		}
	}
	if err := s.save(pluginKey(p.ID, "installed"), map[bool]string{true: "true", false: "false"}[*req.Installed]); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, s.load(p))
}

// SetDatabases handles PUT /plugins/databases/config {"databases": [...]}.
func (s *PluginService) SetDatabases(c *gin.Context) {
	var req struct {
		Databases []DatabaseEntry `json:"databases"`
	}
	if err := c.BindJSON(&req); err != nil || req.Databases == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": `send {"databases": [...]}`})
		return
	}
	seen := map[string]bool{}
	for i, d := range req.Databases {
		switch {
		case d.Name == "":
			c.JSON(http.StatusBadRequest, gin.H{"error": "every database needs a name"})
			return
		case seen[d.Name]:
			c.JSON(http.StatusBadRequest, gin.H{"error": "duplicate database name " + d.Name})
			return
		case !endpointRe.MatchString(d.PrimaryEndpoint):
			c.JSON(http.StatusBadRequest, gin.H{"error": d.Name + ": primary endpoint must be host:port"})
			return
		case d.StandbyEndpoint != "" && !endpointRe.MatchString(d.StandbyEndpoint):
			c.JSON(http.StatusBadRequest, gin.H{"error": d.Name + ": standby endpoint must be host:port"})
			return
		}
		if d.Projects == nil {
			req.Databases[i].Projects = []string{}
		}
		seen[d.Name] = true
	}
	raw, _ := json.Marshal(req)
	if err := s.save(pluginKey("databases", "config"), string(raw)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	p, _ := s.find("databases")
	c.JSON(http.StatusOK, s.load(p))
}

// AddCustom handles POST /plugins/custom: add a plugin to the catalog. Like
// the built-in ones it is only a description; nothing is downloaded.
func (s *PluginService) AddCustom(c *gin.Context) {
	var req struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Source      string `json:"source"`
		Footprint   string `json:"footprint"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	u, err := url.Parse(req.Source)
	switch {
	case !pluginIDRe.MatchString(req.ID):
		c.JSON(http.StatusBadRequest, gin.H{"error": "id: 2-40 lowercase letters, digits or dashes"})
		return
	case strings.TrimSpace(req.Name) == "":
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	case err != nil || u.Scheme != "https" || u.Host == "":
		c.JSON(http.StatusBadRequest, gin.H{"error": "source must be an https:// link to the plugin's repository or manifest"})
		return
	}
	if _, exists := s.find(req.ID); exists {
		c.JSON(http.StatusConflict, gin.H{"error": "a plugin called " + req.ID + " already exists"})
		return
	}
	p := Plugin{ID: req.ID, Name: strings.TrimSpace(req.Name), Description: strings.TrimSpace(req.Description),
		Footprint: strings.TrimSpace(req.Footprint), Source: req.Source, Status: "available", Custom: true}
	if err := s.saveCustom(append(s.customPlugins(), p)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, p)
}

// RemoveCustom handles DELETE /plugins/custom/:id.
func (s *PluginService) RemoveCustom(c *gin.Context) {
	id := c.Param("id")
	kept := []Plugin{}
	found := false
	for _, p := range s.customPlugins() {
		if p.ID == id {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "no custom plugin called " + id})
		return
	}
	if err := s.saveCustom(kept); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	s.db.Where("key LIKE ?", "plugin."+id+".%").Delete(&models.Setting{})
	c.JSON(http.StatusOK, gin.H{"message": "removed"})
}

func (s *PluginService) saveCustom(list []Plugin) error {
	raw, _ := json.Marshal(list)
	return s.save(customPluginsKey, string(raw))
}
