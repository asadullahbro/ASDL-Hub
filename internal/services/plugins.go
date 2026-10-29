package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/asdl/hub/internal/models"
)

// A plugin is a companion container attached to a project: it runs on the
// same node as the project, in a private Docker network shared with it, and
// gives the project environment variables to reach it (e.g. REDIS_URL). It
// is (re)started with every deploy of the project, so it moves with the
// project on moves, failovers and maintenance, and is removed with it.
//
// Plugin data lives in the container and does not move between nodes, so
// plugins are for things that can start empty: caches, search, proxies.
type PluginManifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Image       string `json:"image"`
	// Port the project talks to inside the private network.
	Port    int               `json:"port"`
	Command []string          `json:"command,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	// Files are written on the node and mounted read-only at Path.
	Files []PluginFile `json:"files,omitempty"`
	Vars  []PluginVar  `json:"vars,omitempty"`
	// Provides are environment variables given to the project; they take
	// precedence over the project's own variables with the same name.
	Provides map[string]string `json:"provides"`
	// Public plugins also get a port on the node, and the Hub serves them at
	// the domain and path chosen when they're attached (like a project).
	Public  bool `json:"public,omitempty"`
	Builtin bool `json:"builtin,omitempty"`
	// Persist makes the plugin able to keep its data on the node's disk. It is
	// opt-in per attachment through the "persist" variable (default off), so a
	// plugin without it, or with it off, still starts empty.
	Persist *PluginPersist `json:"persist,omitempty"`
}

// PersistVarKey is the variable that switches persistence on for one attachment.
// SnapshotVarKey (needs persistence) also sends copies to the Hub, every
// SnapshotMinutesKey minutes, so the data survives the node itself failing.
const (
	PersistVarKey          = "persist"
	SnapshotVarKey         = "snapshot"
	SnapshotMinutesKey     = "snapshot_minutes"
	defaultSnapshotMinutes = 5
	maxSnapshotMinutes     = 1440
)

// PluginPersist describes the data a plugin can keep.
type PluginPersist struct {
	// DataPath inside the container; the node's data folder for this plugin is mounted there.
	DataPath string `json:"data_path"`
	// Command replaces the plugin's Command while persistence is on (e.g. to make the server save to DataPath).
	Command []string `json:"command"`
	// Before runs inside the running plugin (docker exec) right before a snapshot is taken, to make the data
	// consistent on disk (e.g. Redis SAVE). Empty means the folder is copied as it is.
	Before []string `json:"before,omitempty"`
}

// snapshotOn reports whether the attached values send snapshots to the Hub (only meaningful with persistence on).
func snapshotOn(values map[string]string) bool {
	switch strings.ToLower(strings.TrimSpace(values[SnapshotVarKey])) {
	case "on", "true", "yes", "1":
		return true
	}
	return false
}

// snapshotMinutes is the chosen interval, clamped to 1 minute..1 day.
func snapshotMinutes(values map[string]string) int {
	n, err := strconv.Atoi(strings.TrimSpace(values[SnapshotMinutesKey]))
	if err != nil || n < 1 {
		return defaultSnapshotMinutes
	}
	if n > maxSnapshotMinutes {
		return maxSnapshotMinutes
	}
	return n
}

// persistOn reports whether the attached values switch persistence on.
func persistOn(values map[string]string) bool {
	switch strings.ToLower(strings.TrimSpace(values[PersistVarKey])) {
	case "on", "true", "yes", "1":
		return true
	}
	return false
}

type PluginFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// PluginVar is a setting chosen when the plugin is attached. Values can be
// used in Command, Env, Files and Provides as {{var.KEY}}.
type PluginVar struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Default     string `json:"default,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Required    bool   `json:"required,omitempty"`
	GenerateLen int    `json:"generate,omitempty"` // random value of this length if left empty
}

// Templates may use {{var.KEY}}, {{host}} (the plugin's name in the private
// network) and {{port}}.
var pluginTemplateRe = regexp.MustCompile(`\{\{\s*(var\.[A-Za-z0-9_]+|host|port)\s*\}\}`)
var anyTemplateRe = regexp.MustCompile(`\{\{[^}]*\}\}`)

var builtinPlugins = []PluginManifest{
	{
		ID:          "redis",
		Name:        "Redis",
		Description: "A private Redis next to the app, protected with a generated password. Starts empty on each node; turn on \"persist\" to keep its data on the node's disk across restarts.",
		Image:       "redis:7-alpine",
		Port:        6379,
		Command:     []string{"redis-server", "--requirepass", "{{var.password}}", "--save", "", "--appendonly", "no"},
		Vars: []PluginVar{
			{Key: "password", Label: "Password", Secret: true, GenerateLen: 32},
			{Key: PersistVarKey, Label: "Keep data on this node's disk (on/off)", Default: "off"},
			{Key: SnapshotVarKey, Label: "Also send snapshots to the Hub, so the data survives the node failing (on/off, needs the setting above)", Default: "off"},
			{Key: SnapshotMinutesKey, Label: "Minutes between snapshots", Default: "5"},
		},
		Persist: &PluginPersist{DataPath: "/data", Command: []string{"redis-server", "--requirepass", "{{var.password}}",
			"--dir", "/data", "--save", "60", "1", "--appendonly", "no"},
			Before: []string{"redis-cli", "-a", "{{var.password}}", "--no-auth-warning", "SAVE"}},
		Provides: map[string]string{"REDIS_URL": "redis://:{{var.password}}@{{host}}:{{port}}/0"},
		Builtin:  true,
	},
	{
		ID:          "searxng",
		Name:        "SearXNG",
		Description: "A private metasearch engine next to the app, with JSON results enabled for API use.",
		Image:       "searxng/searxng:latest",
		Port:        8080,
		Files: []PluginFile{{Path: "/etc/searxng/settings.yml", Content: "use_default_settings: true\n" +
			"server:\n  secret_key: \"{{var.secret_key}}\"\n  limiter: false\n  image_proxy: false\n" +
			"search:\n  formats:\n    - html\n    - json\n"}},
		Vars:     []PluginVar{{Key: "secret_key", Label: "Secret key", Secret: true, GenerateLen: 48}},
		Provides: map[string]string{"SEARXNG_URL": "http://{{host}}:{{port}}"},
		Builtin:  true,
	},
}

// supabaseRest serves a Postgres database the way Supabase's REST API does
// (/rest/v1/ on a Supabase project), so Supabase clients work unchanged.
var supabaseRest = PluginManifest{
	ID:   "supabase-rest",
	Name: "Supabase REST API",
	Description: "PostgREST, the REST API Supabase serves at /rest/v1/, for a Postgres database " +
		"(e.g. a self-hosted Supabase). Public: attach it with a domain and path like /rest/v1/.",
	Image: "public.ecr.aws/supabase/postgrest:v14.3",
	Port:  3000,
	Env: map[string]string{
		"PGRST_DB_URI":               "{{var.db_uri}}",
		"PGRST_JWT_SECRET":           "{{var.jwt_secret}}",
		"PGRST_DB_SCHEMAS":           "{{var.schemas}}",
		"PGRST_DB_EXTRA_SEARCH_PATH": "public,extensions",
		"PGRST_DB_ANON_ROLE":         "anon",
		"PGRST_DB_MAX_ROWS":          "1000",
	},
	Vars: []PluginVar{
		{Key: "db_uri", Label: "Database URL (postgresql://authenticator:…@host:port/postgres, a mesh address)", Secret: true, Required: true},
		{Key: "jwt_secret", Label: "JWT secret (Supabase's jwt_secret)", Secret: true, Required: true},
		{Key: "schemas", Label: "Schemas", Default: "public,graphql_public"},
	},
	Provides: map[string]string{"SUPABASE_REST_URL": "http://{{host}}:{{port}}"},
	Public:   true,
	Builtin:  true,
}

func init() { builtinPlugins = append(builtinPlugins, supabaseRest) }

var (
	pluginIDRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,29}$`)
	pluginImageRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:@-]{0,254}$`)
	envNameRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Validate checks a manifest, including that every template refers to a
// declared variable.
func (m *PluginManifest) Validate() error {
	if !pluginIDRe.MatchString(m.ID) {
		return fmt.Errorf("id must be lowercase letters, digits and '-' (e.g. my-cache)")
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !pluginImageRe.MatchString(m.Image) {
		return fmt.Errorf("invalid image %q", m.Image)
	}
	if m.Port < 1 || m.Port > 65535 {
		return fmt.Errorf("port must be 1-65535")
	}
	if len(m.Provides) == 0 && !m.Public {
		return fmt.Errorf("provides must give the project at least one variable (how it reaches the plugin), unless the plugin is public")
	}
	vars := map[string]bool{}
	for _, v := range m.Vars {
		if !envNameRe.MatchString(v.Key) || vars[v.Key] {
			return fmt.Errorf("invalid or duplicate variable %q", v.Key)
		}
		if v.GenerateLen < 0 || v.GenerateLen > 256 {
			return fmt.Errorf("variable %s: generate must be 0-256", v.Key)
		}
		vars[v.Key] = true
	}
	check := func(where, s string) error {
		for _, t := range anyTemplateRe.FindAllString(s, -1) {
			sm := pluginTemplateRe.FindStringSubmatch(t)
			if sm == nil {
				return fmt.Errorf("%s: unknown template %s (use {{var.KEY}}, {{host}} or {{port}})", where, t)
			}
			if strings.HasPrefix(sm[1], "var.") && !vars[strings.TrimPrefix(sm[1], "var.")] {
				return fmt.Errorf("%s: %s is not a declared variable", where, t)
			}
		}
		return nil
	}
	for k, v := range m.Env {
		if !envNameRe.MatchString(k) {
			return fmt.Errorf("invalid env name %q", k)
		}
		if err := check("env."+k, v); err != nil {
			return err
		}
	}
	for k, v := range m.Provides {
		if !envNameRe.MatchString(k) {
			return fmt.Errorf("invalid provided variable name %q", k)
		}
		if err := check("provides."+k, v); err != nil {
			return err
		}
	}
	for i, a := range m.Command {
		if err := check(fmt.Sprintf("command[%d]", i), a); err != nil {
			return err
		}
	}
	if m.Persist != nil {
		if !path.IsAbs(m.Persist.DataPath) || path.Clean(m.Persist.DataPath) != m.Persist.DataPath || m.Persist.DataPath == "/" {
			return fmt.Errorf("persist.data_path %q must be an absolute, clean path below /", m.Persist.DataPath)
		}
		if !vars[PersistVarKey] {
			return fmt.Errorf("a plugin with persist must declare a %q variable (the on/off switch)", PersistVarKey)
		}
		if len(m.Persist.Command) == 0 {
			return fmt.Errorf("persist.command is required (how the plugin starts when it keeps data)")
		}
		for i, a := range m.Persist.Command {
			if err := check(fmt.Sprintf("persist.command[%d]", i), a); err != nil {
				return err
			}
		}
		for i, a := range m.Persist.Before {
			if err := check(fmt.Sprintf("persist.before[%d]", i), a); err != nil {
				return err
			}
		}
		if vars[SnapshotMinutesKey] && !vars[SnapshotVarKey] {
			return fmt.Errorf("%q needs the %q variable too", SnapshotMinutesKey, SnapshotVarKey)
		}
	}
	for _, f := range m.Files {
		if !path.IsAbs(f.Path) || path.Clean(f.Path) != f.Path || strings.Contains(f.Path, "..") {
			return fmt.Errorf("file path %q must be absolute and clean", f.Path)
		}
		if err := check("file "+f.Path, f.Content); err != nil {
			return err
		}
	}
	return nil
}

// FillVars returns the values for attaching m: given values, then defaults,
// then generated values; an error if a required one is missing.
func (m *PluginManifest) FillVars(given []models.EnvVar) ([]models.EnvVar, error) {
	in := map[string]string{}
	for _, v := range given {
		in[v.Key] = v.Value
	}
	out := make([]models.EnvVar, 0, len(m.Vars))
	for _, v := range m.Vars {
		val := in[v.Key]
		if val == "" {
			val = v.Default
		}
		if val == "" && v.GenerateLen > 0 {
			val = randomToken(v.GenerateLen)
		}
		if val == "" && v.Required {
			return nil, fmt.Errorf("%s is required", v.Label)
		}
		out = append(out, models.EnvVar{Key: v.Key, Value: val})
	}
	return out, nil
}

func randomToken(n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// ResolvedPlugin is a plugin ready to run next to one project.
type ResolvedPlugin struct {
	PluginID      string
	ContainerName string
	Alias         string // its name in the project's network
	Image         string
	Command       []string
	Env           [][2]string // sorted by name
	Files         []PluginFile
	Provides      [][2]string // sorted by name
	// Volumes are host:container mounts for data the plugin keeps (empty unless persistence is on).
	Volumes []string
	// Snapshot is set when the data is also sent to the Hub (persistence and snapshots both on).
	Snapshot *SnapshotSpec
	// Public plugins publish Port on the node, preferring PreferredHostPort
	// (the port they had before, so their route stays put).
	Public            bool
	Port              int
	PreferredHostPort int
	// Hash changes whenever anything that affects the container changes, so
	// an unchanged plugin isn't restarted by a redeploy.
	Hash string
}

// SnapshotSpec is what a node needs to take (and the deploy needs to restore) a plugin's snapshots.
type SnapshotSpec struct {
	DataDir  string   // host folder holding the plugin's data
	StateDir string   // host folder for the plugin's snapshot bookkeeping (which generation this node holds)
	Before   []string // run in the plugin before copying; may be empty
	Minutes  int
	// Filled in by the Hub for a deploy, not by Resolve: where to send and fetch snapshots, the token for that,
	// and (for a planned move) the final snapshot job the deploy should wait for.
	URL, Token, AfterJob string
}

// Resolve fills in m's templates for project with the attached values.
func (m *PluginManifest) Resolve(project *models.Project, vars []models.EnvVar) ResolvedPlugin {
	values := map[string]string{}
	for _, v := range vars {
		values[v.Key] = v.Value
	}
	sub := func(s string) string {
		return pluginTemplateRe.ReplaceAllStringFunc(s, func(t string) string {
			k := pluginTemplateRe.FindStringSubmatch(t)[1]
			switch {
			case k == "host":
				return m.ID
			case k == "port":
				return fmt.Sprint(m.Port)
			default:
				return values[strings.TrimPrefix(k, "var.")]
			}
		})
	}
	r := ResolvedPlugin{
		PluginID:      m.ID,
		ContainerName: project.Name + "-" + m.ID,
		Alias:         m.ID,
		Image:         m.Image,
		Public:        m.Public,
		Port:          m.Port,
	}
	command := m.Command
	if m.Persist != nil && persistOn(values) {
		command = m.Persist.Command
		dataDir := fmt.Sprintf("%s/%s/%s/data", PluginDir, project.ID, m.ID)
		r.Volumes = []string{dataDir + ":" + m.Persist.DataPath}
		if snapshotOn(values) {
			r.Snapshot = &SnapshotSpec{DataDir: dataDir, StateDir: fmt.Sprintf("%s/%s/%s", PluginDir, project.ID, m.ID), Minutes: snapshotMinutes(values)}
			for _, a := range m.Persist.Before {
				r.Snapshot.Before = append(r.Snapshot.Before, sub(a))
			}
		}
	}
	for _, a := range command {
		r.Command = append(r.Command, sub(a))
	}
	for _, f := range m.Files {
		r.Files = append(r.Files, PluginFile{Path: f.Path, Content: sub(f.Content)})
	}
	r.Env = sortedPairs(m.Env, sub)
	r.Provides = sortedPairs(m.Provides, sub)

	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%q\x00%q\x00%q\x00%v\x00%q", r.Image, r.Command, r.Env, r.Files, r.Public, r.Volumes)
	r.Hash = hex.EncodeToString(h.Sum(nil))[:16]
	return r
}

func sortedPairs(m map[string]string, sub func(string) string) [][2]string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([][2]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, [2]string{k, sub(m[k])})
	}
	return out
}
