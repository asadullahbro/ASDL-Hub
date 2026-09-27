package services

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/asdl/hub/internal/models"
)

func TestBuiltinPluginsAreValid(t *testing.T) {
	for _, m := range builtinPlugins {
		if err := m.Validate(); err != nil {
			t.Errorf("%s: %v", m.ID, err)
		}
	}
}

func TestPluginManifestValidation(t *testing.T) {
	good := PluginManifest{ID: "cache", Name: "Cache", Image: "redis:7", Port: 6379,
		Vars: []PluginVar{{Key: "pw"}}, Provides: map[string]string{"CACHE_URL": "redis://:{{var.pw}}@{{host}}:{{port}}"}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(m *PluginManifest){
		"undeclared var":   func(m *PluginManifest) { m.Provides = map[string]string{"X": "{{var.nope}}"} },
		"unknown template": func(m *PluginManifest) { m.Command = []string{"{{node_ip}}"} },
		"relative file":    func(m *PluginManifest) { m.Files = []PluginFile{{Path: "etc/x", Content: "a"}} },
		"dotdot file":      func(m *PluginManifest) { m.Files = []PluginFile{{Path: "/etc/../root/x", Content: "a"}} },
		"no provides":      func(m *PluginManifest) { m.Provides = nil },
		"bad id":           func(m *PluginManifest) { m.ID = "Bad ID" },
		"image with space": func(m *PluginManifest) { m.Image = "redis 7" },
	}
	for name, mutate := range bad {
		m := good
		m.Provides = map[string]string{"CACHE_URL": "redis://{{host}}"}
		mutate(&m)
		if m.Validate() == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestDeployWithPlugin_RunsItNextToTheAppAndHandsItTheURL(t *testing.T) {
	PluginDir = t.TempDir()
	project := &models.Project{ID: "p-1", Name: "bot", Ports: []string{"9000:8000"},
		EnvVars: models.SecretEnvVars{{Key: "REDIS_URL", Value: "redis://localhost:6379"}, {Key: "TOKEN", Value: "t"}}}
	var redis PluginManifest
	for _, m := range builtinPlugins {
		if m.ID == "redis" {
			redis = m
		}
	}
	rp := redis.Resolve(project, []models.EnvVar{{Key: "password", Value: "s3cretpw"}})
	script, env := BuildDeployCommand(DeploySpec{Project: project, Image: "ghcr.io/o/bot:v1", Plugins: []ResolvedPlugin{rp}})

	if strings.Contains(script, "s3cretpw") {
		t.Fatal("plugin secrets must not be in the script")
	}
	calls, out, err := runWithFakeDocker(t, script, env, nil, false)
	if err != nil {
		t.Fatalf("script failed: %v\n%s\n%s", err, out, calls)
	}
	// The plugin container: private network, alias, labels, resolved command.
	for _, want := range []string{
		"run\n-d\n--name\nbot-redis\n--restart\nunless-stopped\n--network\nasdl-bot\n--network-alias\nredis\n",
		"--label\nasdl.companion-of=p-1\n",
		"redis:7-alpine\nredis-server\n--requirepass\ns3cretpw\n--save\n\n--appendonly\nno",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %q in docker calls:\n%s", want, calls)
		}
	}
	// The app joins the network and gets the plugin's URL instead of its own.
	if !strings.Contains(calls, "--network\nasdl-bot\n-e\nREDIS_URL=redis://:s3cretpw@redis:6379/0\n") {
		t.Errorf("app should get the plugin's REDIS_URL on its network:\n%s", calls)
	}
	if strings.Contains(calls, "REDIS_URL=redis://localhost:6379") {
		t.Error("the plugin's REDIS_URL must replace the project's own")
	}
	if !strings.Contains(calls, "TOKEN=t") {
		t.Error("the project's other variables must still be passed")
	}
}

func TestDeployWithPlugin_WritesItsFiles(t *testing.T) {
	PluginDir = t.TempDir()
	project := &models.Project{ID: "p-2", Name: "bot", Ports: []string{"9000:8000"}}
	var sx PluginManifest
	for _, m := range builtinPlugins {
		if m.ID == "searxng" {
			sx = m
		}
	}
	rp := sx.Resolve(project, []models.EnvVar{{Key: "secret_key", Value: "k3y"}})
	script, env := BuildDeployCommand(DeploySpec{Project: project, Image: "img", Plugins: []ResolvedPlugin{rp}})
	calls, out, err := runWithFakeDocker(t, script, env, nil, false)
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	b, err := os.ReadFile(filepath.Join(PluginDir, "p-2", "searxng", "file0"))
	if err != nil || !strings.Contains(string(b), `secret_key: "k3y"`) || !strings.Contains(string(b), "- json") {
		t.Fatalf("settings file = %q, %v", b, err)
	}
	if !strings.Contains(calls, filepath.Join(PluginDir, "p-2", "searxng", "file0")+":/etc/searxng/settings.yml:ro") {
		t.Errorf("file must be mounted read-only:\n%s", calls)
	}
	if !strings.Contains(calls, "SEARXNG_URL=http://searxng:8080") {
		t.Errorf("app should get SEARXNG_URL:\n%s", calls)
	}
}

func TestRemoveProjectCommand_TakesPluginsAlong(t *testing.T) {
	cmd := BuildRemoveProjectCommand(&models.Project{ID: "p-1", Name: "bot"})
	for _, want := range []string{"docker rm -f 'bot'", "label=asdl.companion-of=p-1", "docker network rm 'asdl-bot'", "rm -rf '" + PluginDir + "/p-1'"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("missing %q in:\n%s", want, cmd)
		}
	}
}

func TestAttachPlugin_GeneratesSecretsAndRedeploys(t *testing.T) {
	_, _, db := newFailoverEnv(t)
	db.AutoMigrate(&models.ProjectPlugin{})
	db.Model(&models.Project{}).Where("id = ?", "p1").Update("node_id", "good")
	redeployed := 0
	svc := NewPluginService(db, func(*models.Project) error { redeployed++; return nil })

	c, w := newTestContext(http.MethodPost, "/projects/p1/plugins", `{"plugin_id":"redis"}`)
	c.Params = gin.Params{{Key: "id", Value: "p1"}}
	svc.Attach(c)
	if w.Code != http.StatusCreated || redeployed != 1 {
		t.Fatalf("attach: %d %s (redeployed %d)", w.Code, w.Body.String(), redeployed)
	}
	if strings.Contains(w.Body.String(), `"password"`) && !strings.Contains(w.Body.String(), "********") {
		t.Errorf("the generated password must be masked in the response: %s", w.Body.String())
	}
	var pp models.ProjectPlugin
	db.First(&pp, "project_id = ?", "p1")
	if len(pp.Vars) != 1 || len(pp.Vars[0].Value) != 32 {
		t.Fatalf("expected a generated 32-char password, got %+v", pp.Vars)
	}

	// The deployer now includes it.
	p := loadProject(db)
	if got := resolvePlugins(db, &p); len(got) != 1 || got[0].ContainerName != "api-redis" {
		t.Fatalf("resolvePlugins = %+v", got)
	}

	c, w = newTestContext(http.MethodDelete, "/projects/p1/plugins/redis", "")
	c.Params = gin.Params{{Key: "id", Value: "p1"}, {Key: "plugin", Value: "redis"}}
	svc.Detach(c)
	if w.Code != http.StatusOK || redeployed != 2 {
		t.Fatalf("detach: %d (redeployed %d)", w.Code, redeployed)
	}
	if got := resolvePlugins(db, &p); len(got) != 0 {
		t.Errorf("plugin still attached: %+v", got)
	}
}

func TestPublicPlugin_PublishesAPortAndReportsIt(t *testing.T) {
	PluginDir = t.TempDir()
	project := &models.Project{ID: "p-3", Name: "bot", Ports: []string{"9000:8000"}}
	rp := supabaseRest.Resolve(project, []models.EnvVar{{Key: "db_uri", Value: "postgresql://a:b@10.0.0.4:54322/postgres"}, {Key: "jwt_secret", Value: "jw"}, {Key: "schemas", Value: "public"}})
	rp.PreferredHostPort = 20005
	script, env := BuildDeployCommand(DeploySpec{Project: project, Image: "img", Plugins: []ResolvedPlugin{rp}})
	if strings.Contains(script, "jw") || strings.Contains(script, "10.0.0.4") {
		t.Fatal("plugin settings must not be in the script")
	}
	calls, out, err := runWithFakeDocker(t, script, env, []string{"20005"}, false)
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	if !strings.Contains(calls, "-p\n20006:3000\n") {
		t.Errorf("busy 20005 should move the plugin to 20006:\n%s", calls)
	}
	if got := ParsePluginPorts(out); got["supabase-rest"] != 20006 {
		t.Errorf("reported ports = %v, want supabase-rest:20006\n%s", got, out)
	}
	if !strings.Contains(calls, "PGRST_JWT_SECRET=jw") {
		t.Errorf("env not passed:\n%s", calls)
	}
}

func TestPublicPlugin_IsRoutedToItsProjectsNode(t *testing.T) {
	_, _, db := newFailoverEnv(t)
	db.Model(&models.Project{}).Where("id = ?", "p1").Updates(map[string]interface{}{"node_id": "good", "status": "running"})
	db.Create(&models.ProjectPlugin{ID: "pp1", ProjectID: "p1", PluginID: "supabase-rest", Domain: "db.example.com", RoutePath: "/rest/v1/", HostPort: 20007})
	n := &NginxService{db: db, routesDir: t.TempDir(), certs: map[string]bool{}, certFailed: map[string]time.Time{}}
	routes, err := n.routes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Domain != "db.example.com" || len(routes[0].Locations) != 1 ||
		routes[0].Locations[0] != (RouteLocation{Path: "/rest/v1/", NodeIP: "10.0.0.3", Port: "20007"}) {
		t.Fatalf("routes = %+v", routes)
	}

	// The route can't be taken twice.
	svc := NewPluginService(db, func(*models.Project) error { return nil })
	if err := svc.checkRoute(supabaseRest, "db.example.com", "/rest/v1/", ""); err == nil {
		t.Error("a second plugin on the same domain+path must be refused")
	}
	ps := NewProjectService(db, NewDeployer(db))
	if other := ps.routeTakenBy("db.example.com", "/rest/v1/", ""); other == "" {
		t.Error("a project must not take a plugin's route")
	}
}

func TestUpdatePlugin_OnlyRedeploysWhenSettingsChange(t *testing.T) {
	_, _, db := newFailoverEnv(t)
	db.Model(&models.Project{}).Where("id = ?", "p1").Update("node_id", "good")
	db.Create(&models.ProjectPlugin{ID: "pp1", ProjectID: "p1", PluginID: "supabase-rest",
		Vars: models.SecretEnvVars{{Key: "db_uri", Value: "postgresql://a@x:1/p"}, {Key: "jwt_secret", Value: "s"}, {Key: "schemas", Value: "public"}}})
	redeployed := 0
	svc := NewPluginService(db, func(*models.Project) error { redeployed++; return nil })
	put := func(body string) int {
		c, w := newTestContext(http.MethodPut, "/projects/p1/plugins/supabase-rest", body)
		c.Params = gin.Params{{Key: "id", Value: "p1"}, {Key: "plugin", Value: "supabase-rest"}}
		svc.Update(c)
		return w.Code
	}
	if code := put(`{"vars":[{"key":"db_uri","value":"********"}]}`); code != http.StatusOK || redeployed != 0 {
		t.Fatalf("masked value: %d, redeployed %d; want 200 and no redeploy", code, redeployed)
	}
	if code := put(`{"vars":[{"key":"db_uri","value":"postgresql://a@y:1/p"}]}`); code != http.StatusOK || redeployed != 1 {
		t.Fatalf("new db_uri: %d, redeployed %d; want a redeploy", code, redeployed)
	}
	var pp models.ProjectPlugin
	db.First(&pp, "id = ?", "pp1")
	got := map[string]string{}
	for _, v := range pp.Vars {
		got[v.Key] = v.Value
	}
	if got["db_uri"] != "postgresql://a@y:1/p" || got["jwt_secret"] != "s" {
		t.Errorf("vars = %v; the untouched secret must be kept", got)
	}
}
