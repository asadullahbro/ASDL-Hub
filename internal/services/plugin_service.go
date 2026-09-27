package services

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/asdl/hub/internal/models"
)

// Custom plugin manifests are stored as one JSON list in settings.
const customPluginsKey = "plugins.custom.v2"

// PluginCatalog returns every plugin the Hub knows: built-in and custom.
func PluginCatalog(db *gorm.DB) map[string]PluginManifest {
	out := map[string]PluginManifest{}
	for _, m := range builtinPlugins {
		out[m.ID] = m
	}
	for _, m := range customPlugins(db) {
		if _, taken := out[m.ID]; !taken {
			out[m.ID] = m
		}
	}
	return out
}

func customPlugins(db *gorm.DB) []PluginManifest {
	var st models.Setting
	if db.First(&st, "key = ?", customPluginsKey).Error != nil {
		return nil
	}
	var out []PluginManifest
	if err := json.Unmarshal([]byte(st.Value), &out); err != nil {
		log.Printf("⚠️ custom plugins: %v", err)
	}
	return out
}

func saveCustomPlugins(db *gorm.DB, list []PluginManifest) error {
	b, _ := json.Marshal(list)
	return db.Clauses(clause.OnConflict{UpdateAll: true}).
		Create(&models.Setting{Key: customPluginsKey, Value: string(b), UpdatedAt: time.Now()}).Error
}

// resolvePlugins returns the plugins attached to project, ready to deploy.
// A plugin whose definition was removed is skipped (and logged).
func resolvePlugins(db *gorm.DB, project *models.Project) []ResolvedPlugin {
	var attached []models.ProjectPlugin
	db.Where("project_id = ?", project.ID).Order("plugin_id").Find(&attached)
	if len(attached) == 0 {
		return nil
	}
	catalog := PluginCatalog(db)
	out := make([]ResolvedPlugin, 0, len(attached))
	for _, a := range attached {
		m, ok := catalog[a.PluginID]
		if !ok {
			log.Printf("⚠️ %s has plugin %s attached, but no such plugin exists any more; skipping it", project.Name, a.PluginID)
			continue
		}
		r := m.Resolve(project, a.Vars)
		r.PreferredHostPort = a.HostPort
		out = append(out, r)
	}
	return out
}

// PluginService serves the plugin catalog and attaches plugins to projects.
type PluginService struct {
	db *gorm.DB
	// redeploy restarts a project with its current settings (and plugins).
	redeploy func(*models.Project) error
	// routesChanged regenerates the Hub's routes (public plugins' domains).
	routesChanged func()
}

func NewPluginService(db *gorm.DB, redeploy func(*models.Project) error) *PluginService {
	return &PluginService{db: db, redeploy: redeploy}
}

func (s *PluginService) SetRoutesChangedHook(fn func()) { s.routesChanged = fn }

// checkRoute validates a public plugin's domain and path and that nobody
// else serves them.
func (s *PluginService) checkRoute(m PluginManifest, domain, path, exceptPlugin string) error {
	if !m.Public {
		if domain != "" || path != "" {
			return fmt.Errorf("%s isn't a public plugin, so it can't have a domain", m.Name)
		}
		return nil
	}
	if err := models.ValidateProjectConfig("", domain, nil, nil); err != nil {
		return err
	}
	if err := models.ValidateRoutePath(path); err != nil {
		return err
	}
	if other := routeTakenBy(s.db, domain, path, "", exceptPlugin); other != "" {
		return fmt.Errorf("%s%s is already served by %s", domain, pathOrRoot(path), other)
	}
	return nil
}

type pluginInfo struct {
	PluginManifest
	AttachedTo []string `json:"attached_to"` // project names
}

// List handles GET /plugins: the catalog, with the projects using each.
func (s *PluginService) List(c *gin.Context) {
	var attached []models.ProjectPlugin
	s.db.Find(&attached)
	names := map[string]string{}
	var projects []models.Project
	s.db.Select("id", "name").Find(&projects)
	for _, p := range projects {
		names[p.ID] = p.Name
	}
	users := map[string][]string{}
	for _, a := range attached {
		if n, ok := names[a.ProjectID]; ok {
			users[a.PluginID] = append(users[a.PluginID], n)
		}
	}
	out := []pluginInfo{}
	for _, m := range PluginCatalog(s.db) {
		u := users[m.ID]
		sort.Strings(u)
		if u == nil {
			u = []string{}
		}
		out = append(out, pluginInfo{PluginManifest: m, AttachedTo: u})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Builtin != out[j].Builtin {
			return out[i].Builtin
		}
		return out[i].Name < out[j].Name
	})
	c.JSON(http.StatusOK, out)
}

// AddCustom handles POST /plugins/custom with a manifest (admin).
func (s *PluginService) AddCustom(c *gin.Context) {
	var m PluginManifest
	if err := c.ShouldBindJSON(&m); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid plugin JSON: " + err.Error()})
		return
	}
	m.Builtin = false
	if err := m.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, taken := PluginCatalog(s.db)[m.ID]; taken {
		c.JSON(http.StatusConflict, gin.H{"error": "a plugin with id " + m.ID + " already exists"})
		return
	}
	if err := saveCustomPlugins(s.db, append(customPlugins(s.db), m)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, m)
}

// RemoveCustom handles DELETE /plugins/custom/:id (admin). Refused while a
// project still uses it.
func (s *PluginService) RemoveCustom(c *gin.Context) {
	id := c.Param("id")
	var n int64
	s.db.Model(&models.ProjectPlugin{}).Where("plugin_id = ?", id).Count(&n)
	if n > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("%d project(s) still use this plugin; remove it from them first", n)})
		return
	}
	list := customPlugins(s.db)
	kept := list[:0]
	found := false
	for _, m := range list {
		if m.ID == id {
			found = true
			continue
		}
		kept = append(kept, m)
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "no custom plugin " + id})
		return
	}
	if err := saveCustomPlugins(s.db, kept); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "plugin removed"})
}

// ProjectPlugins handles GET /projects/:id/plugins.
func (s *PluginService) ProjectPlugins(c *gin.Context) {
	var attached []models.ProjectPlugin
	s.db.Where("project_id = ?", c.Param("id")).Order("plugin_id").Find(&attached)
	if attached == nil {
		attached = []models.ProjectPlugin{}
	}
	c.JSON(http.StatusOK, attached)
}

// Attach handles POST /projects/:id/plugins {"plugin_id", "vars"}. The
// project is redeployed so the plugin starts next to it.
func (s *PluginService) Attach(c *gin.Context) {
	var project models.Project
	if err := s.db.First(&project, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	var req struct {
		PluginID  string          `json:"plugin_id"`
		Vars      []models.EnvVar `json:"vars"`
		Domain    string          `json:"domain"`
		RoutePath string          `json:"route_path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m, ok := PluginCatalog(s.db)[req.PluginID]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "no plugin " + req.PluginID})
		return
	}
	var existing models.ProjectPlugin
	if s.db.First(&existing, "project_id = ? AND plugin_id = ?", project.ID, m.ID).Error == nil {
		c.JSON(http.StatusConflict, gin.H{"error": project.Name + " already has " + m.Name})
		return
	}
	vars, err := m.FillVars(req.Vars)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.checkRoute(m, req.Domain, req.RoutePath, ""); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	pp := models.ProjectPlugin{ID: uuid.New().String(), ProjectID: project.ID, PluginID: m.ID, Vars: vars,
		Domain: req.Domain, RoutePath: req.RoutePath, CreatedAt: time.Now()}
	if err := s.db.Create(&pp).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Printf("🧩 Attached plugin %s to %s", m.ID, project.Name)
	c.JSON(http.StatusCreated, gin.H{"plugin": pp, "redeploying": s.redeployNow(&project)})
}

// Update handles PUT /projects/:id/plugins/:plugin {"vars", "domain",
// "route_path"}: masked (unchanged) values keep their stored value. Changed
// settings redeploy the project; a changed route only updates the routes.
func (s *PluginService) Update(c *gin.Context) {
	var project models.Project
	if err := s.db.First(&project, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	var pp models.ProjectPlugin
	if err := s.db.First(&pp, "project_id = ? AND plugin_id = ?", project.ID, c.Param("plugin")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": project.Name + " doesn't have that plugin"})
		return
	}
	m, ok := PluginCatalog(s.db)[pp.PluginID]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "no plugin " + pp.PluginID})
		return
	}
	var req struct {
		Vars      []models.EnvVar `json:"vars"`
		Domain    *string         `json:"domain"`
		RoutePath *string         `json:"route_path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	varsChanged := false
	if req.Vars != nil {
		merged := models.MergeMasked(req.Vars, pp.Vars)
		// Keep settings that weren't sent; take the rest from the request.
		byKey := map[string]string{}
		for _, v := range pp.Vars {
			byKey[v.Key] = v.Value
		}
		for _, v := range merged {
			if byKey[v.Key] != v.Value {
				varsChanged = true
			}
			byKey[v.Key] = v.Value
		}
		given := make([]models.EnvVar, 0, len(byKey))
		for k, v := range byKey {
			given = append(given, models.EnvVar{Key: k, Value: v})
		}
		vars, err := m.FillVars(given)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		pp.Vars = vars
	}
	routeChanged := false
	if req.Domain != nil && *req.Domain != pp.Domain {
		pp.Domain, routeChanged = *req.Domain, true
	}
	if req.RoutePath != nil && *req.RoutePath != pp.RoutePath {
		pp.RoutePath, routeChanged = *req.RoutePath, true
	}
	if routeChanged {
		if err := s.checkRoute(m, pp.Domain, pp.RoutePath, pp.ID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	if err := s.db.Save(&pp).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	redeploying := false
	if varsChanged {
		redeploying = s.redeployNow(&project)
	}
	if routeChanged && s.routesChanged != nil {
		go s.routesChanged()
	}
	c.JSON(http.StatusOK, gin.H{"plugin": pp, "redeploying": redeploying})
}

// Detach handles DELETE /projects/:id/plugins/:plugin. The project is
// redeployed, which removes the plugin's container.
func (s *PluginService) Detach(c *gin.Context) {
	var project models.Project
	if err := s.db.First(&project, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	res := s.db.Where("project_id = ? AND plugin_id = ?", project.ID, c.Param("plugin")).Delete(&models.ProjectPlugin{})
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": project.Name + " doesn't have that plugin"})
		return
	}
	log.Printf("🧩 Removed plugin %s from %s", c.Param("plugin"), project.Name)
	if s.routesChanged != nil {
		go s.routesChanged()
	}
	c.JSON(http.StatusOK, gin.H{"message": "plugin removed", "redeploying": s.redeployNow(&project)})
}

// redeployNow restarts a deployed project so plugin changes take effect;
// false if it has nothing deployed yet (its first deploy will include them).
func (s *PluginService) redeployNow(p *models.Project) bool {
	if p.Image == "" || s.redeploy == nil {
		return false
	}
	if err := s.redeploy(p); err != nil {
		log.Printf("⚠️ Plugins of %s changed but it could not be redeployed yet: %v", p.Name, err)
		return false
	}
	return true
}
