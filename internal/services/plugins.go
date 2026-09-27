package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"sort"
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
	Builtin  bool              `json:"builtin,omitempty"`
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
		Description: "A private Redis cache next to the app, protected with a generated password. Starts empty on each node.",
		Image:       "redis:7-alpine",
		Port:        6379,
		Command:     []string{"redis-server", "--requirepass", "{{var.password}}", "--save", "", "--appendonly", "no"},
		Vars:        []PluginVar{{Key: "password", Label: "Password", Secret: true, GenerateLen: 32}},
		Provides:    map[string]string{"REDIS_URL": "redis://:{{var.password}}@{{host}}:{{port}}/0"},
		Builtin:     true,
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
	if len(m.Provides) == 0 {
		return fmt.Errorf("provides must give the project at least one variable (how it reaches the plugin)")
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
	// Hash changes whenever anything that affects the container changes, so
	// an unchanged plugin isn't restarted by a redeploy.
	Hash string
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
	}
	for _, a := range m.Command {
		r.Command = append(r.Command, sub(a))
	}
	for _, f := range m.Files {
		r.Files = append(r.Files, PluginFile{Path: f.Path, Content: sub(f.Content)})
	}
	r.Env = sortedPairs(m.Env, sub)
	r.Provides = sortedPairs(m.Provides, sub)

	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%q\x00%q\x00%q", r.Image, r.Command, r.Env, r.Files)
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
