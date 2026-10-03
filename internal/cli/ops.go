package cli

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// --- Things only possible on the Hub's own server ---

func needServer() (string, error) {
	dir := installDir()
	if _, err := os.Stat(filepath.Join(dir, "bin", "asdl-hub")); err != nil && !os.IsPermission(err) {
		return "", fmt.Errorf("this isn't a Hub server (no %s); run it on the Hub's server", dir)
	}
	if os.Geteuid() != 0 {
		return "", fmt.Errorf("this changes the Hub's server: run it with sudo")
	}
	return dir, nil
}

func run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// runLive runs a command with its output on the terminal (logs -f).
func runLive(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// nginx reload|test|routes
func (e *env) nginx(args []string) error {
	sub := "reload"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "reload":
		// The Hub writes the routes, checks them with nginx -t and reloads.
		c, err := newClient()
		if err != nil {
			return err
		}
		if err := c.do("POST", "/nginx/update", nil, nil); err != nil {
			return err
		}
		fmt.Fprintln(e.out, "Routes rewritten from the current apps, checked and loaded into nginx.")
		return nil
	case "test":
		if _, err := needServer(); err != nil {
			return err
		}
		out, err := run("nginx", "-t")
		fmt.Fprintln(e.out, out)
		if err != nil {
			return errSilent
		}
		return nil
	case "routes":
		if _, err := needServer(); err != nil {
			return err
		}
		dir := os.Getenv("NGINX_ROUTES_DIR")
		if dir == "" {
			dir = envFileValue(filepath.Join(installDir(), ".env"), "NGINX_ROUTES_DIR")
		}
		if dir == "" {
			dir = "/etc/nginx/asdl-hub.d"
		}
		files, _ := filepath.Glob(filepath.Join(dir, "*.conf"))
		if len(files) == 0 {
			return fmt.Errorf("no route files in %s", dir)
		}
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				return err
			}
			fmt.Fprintf(e.out, "# %s\n%s\n", f, strings.TrimRight(string(b), "\n"))
		}
		return nil
	}
	return usageError{"expected asdl-hub nginx reload|test|routes"}
}

// server status|restart|logs
func (e *env) server(args []string) error {
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "status":
		out, _ := run("systemctl", "status", "asdl-hub", "--no-pager", "--lines=0")
		fmt.Fprintln(e.out, out)
		return nil
	case "restart":
		if _, err := needServer(); err != nil {
			return err
		}
		return e.restartHub()
	case "logs":
		n, follow := "50", false
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "-f", "--follow":
				follow = true
			case "-n":
				if i+1 < len(args) {
					n = args[i+1]
					i++
				}
			}
		}
		a := []string{"-u", "asdl-hub", "-n", n, "--no-pager", "-o", "cat"}
		if follow {
			a = append(a, "-f")
		}
		return runLive("journalctl", a...)
	}
	return usageError{"expected asdl-hub server status|restart|logs [-f] [-n lines]"}
}

func (e *env) restartHub() error {
	fmt.Fprintln(e.out, "Restarting the Hub…")
	if out, err := run("systemctl", "restart", "asdl-hub"); err != nil {
		return fmt.Errorf("systemctl restart asdl-hub: %s", out)
	}
	port := envFileValue(filepath.Join(installDir(), ".env"), "SERVER_PORT")
	if port == "" {
		port = "8080"
	}
	cl := &http.Client{Timeout: 2 * time.Second}
	for i := 0; i < 30; i++ {
		time.Sleep(time.Second)
		if resp, err := cl.Get("http://127.0.0.1:" + port + "/health"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Fprintln(e.out, e.paint(green, "The Hub is back up."))
				return nil
			}
		}
	}
	return fmt.Errorf("the Hub didn't answer within 30 seconds: asdl-hub server logs")
}

// --- Hub settings in /opt/asdl-hub/.env ---

type setting struct {
	desc string
	// risk explains what breaks; such settings need --force.
	risk string
}

var settings = map[string]setting{
	"PUBLIC_URL":           {desc: "the dashboard's public address (links, OIDC audience)"},
	"LOG_LEVEL":            {desc: "debug logs every database query; empty for warnings and errors"},
	"CORS_ALLOWED_ORIGINS": {desc: "origins allowed to call the API from a browser, comma-separated"},
	"GIN_MODE":             {desc: "debug for request logging details"},
	"ACME_WEBROOT":         {desc: "folder for certificate challenges"},
	"CERT_HELPER":          {desc: "script that gets certificates"},
	"NGINX_ROUTES_DIR":     {desc: "where app routes are written"},
	"ADMIN_PASSWORD":       {desc: "the first admin's password; only used when the database is created"},
	"SERVER_PORT":          {desc: "local port the Hub listens on", risk: "nginx's dashboard site forwards to this port; update /etc/nginx/sites-available/asdl-hub to match, or the dashboard goes down"},
	"JWT_SECRET":           {desc: "signs logins and tokens", risk: "everyone is logged out and every permanent token stops working"},
	"SECRETS_KEY":          {desc: "encrypts app secrets", risk: "every stored secret (env vars, registry tokens, notification settings) becomes unreadable"},
	"DB_HOST":              {desc: "database host", risk: "the Hub won't start if it can't reach its database"},
	"DB_PORT":              {desc: "database port", risk: "the Hub won't start if it can't reach its database"},
	"DB_USER":              {desc: "database user", risk: "the Hub won't start if it can't reach its database"},
	"DB_PASSWORD":          {desc: "database password", risk: "change it in PostgreSQL first, or the Hub won't start"},
	"DB_NAME":              {desc: "database name", risk: "the Hub won't start if it can't reach its database"},
	"DB_SSLMODE":           {desc: "database TLS mode", risk: "the Hub won't start if it can't reach its database"},
	"VPN_NETWORKS":         {desc: "networks allowed to use the node API", risk: "nodes outside them can no longer send heartbeats or take jobs"},
	"WG_ENDPOINT":          {desc: "address nodes use to reach the Hub", risk: "new nodes get this address; existing nodes keep the old one"},
	"WG_HUB_PUBKEY":        {desc: "the Hub's WireGuard public key", risk: "it must match the WireGuard interface, or new nodes can't connect"},
}

func isSecretKey(k string) bool {
	if k == "WG_HUB_PUBKEY" {
		return false
	}
	for _, s := range []string{"PASSWORD", "SECRET", "TOKEN", "KEY"} {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// readEnvFile returns the file's lines and its KEY=value pairs.
func readEnvFile(path string) ([]string, map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	var lines []string
	vals := map[string]string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		lines = append(lines, line)
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if k, v, ok := strings.Cut(strings.TrimPrefix(t, "export "), "="); ok {
			vals[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return lines, vals, s.Err()
}

func quoteEnv(v string) string {
	if strings.ContainsAny(v, " #\"'\t") {
		return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	}
	return v
}

// writeEnvFile writes lines back atomically, keeping the owner and mode.
func writeEnvFile(path string, lines []string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), fi.Mode().Perm()); err != nil {
		return err
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(tmp, int(st.Uid), int(st.Gid))
	}
	// Keep the previous version next to it.
	_ = copyFile(path, path+".bak", fi.Mode().Perm())
	return os.Rename(tmp, path)
}

func copyFile(src, dst string, mode os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, mode)
}

// config [list] | get KEY | set KEY=VALUE... | unset KEY... [--restart] [--force] [--show]
func (e *env) config(args []string) error {
	var flags = map[string]bool{}
	var rest []string
	for _, a := range args {
		switch a {
		case "--restart", "--no-restart", "--force", "--show":
			flags[a] = true
		default:
			rest = append(rest, a)
		}
	}
	dir, err := needServer()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, ".env")
	lines, vals, err := readEnvFile(path)
	if err != nil {
		return err
	}
	sub := "list"
	if len(rest) > 0 {
		sub, rest = rest[0], rest[1:]
	}
	switch sub {
	case "list":
		keys := make([]string, 0, len(vals))
		for k := range vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		w := e.table()
		fmt.Fprintln(w, "SETTING\tVALUE\tWHAT IT IS")
		for _, k := range keys {
			v := vals[k]
			if isSecretKey(k) && !flags["--show"] && v != "" {
				v = "********"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", k, orDash(truncate(v, 60)), settings[k].desc)
		}
		w.Flush()
		fmt.Fprintf(e.out, "\nIn %s. Secrets are hidden; add --show to see them.\n", path)
		return nil
	case "get":
		if len(rest) != 1 {
			return usageError{"expected asdl-hub config get KEY"}
		}
		v, ok := vals[rest[0]]
		if !ok {
			return fmt.Errorf("%s isn't set", rest[0])
		}
		fmt.Fprintln(e.out, v)
		return nil
	case "set", "unset":
		existing := make([]string, 0, len(vals))
		for k := range vals {
			existing = append(existing, k)
		}
		valid := func(k string) error {
			if !envNameRe.MatchString(k) {
				return fmt.Errorf("%q isn't a setting name", k)
			}
			if s, known := settings[k]; known && s.risk != "" && !flags["--force"] {
				return fmt.Errorf("changing %s is risky: %s. Add --force if you're sure", k, s.risk)
			}
			if sub == "unset" {
				if _, ok := vals[k]; !ok {
					return fmt.Errorf("%s isn't set", k)
				}
			}
			return nil
		}
		changes, err := e.askChanges(sub, rest, existing, isSecretKey, "setting", valid)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			fmt.Fprintln(e.out, "Nothing changed.")
			return nil
		}
		for k := range changes {
			if _, known := settings[k]; !known && !strings.HasPrefix(k, "WG_") {
				fmt.Fprintf(e.out, "%s %s isn't a setting this Hub knows; it's saved anyway.\n", e.paint(yellow, "note:"), k)
			}
		}
		lines = applyEnvChanges(lines, changes)
		if err := writeEnvFile(path, lines); err != nil {
			return err
		}
		for k, v := range changes {
			if v == nil {
				fmt.Fprintf(e.out, "Removed %s.\n", k)
			} else if isSecretKey(k) {
				fmt.Fprintf(e.out, "Set %s.\n", k)
			} else {
				fmt.Fprintf(e.out, "Set %s=%s.\n", k, *v)
			}
		}
		fmt.Fprintf(e.out, "Saved %s (the previous version is in .env.bak).\n", path)
		if flags["--no-restart"] {
			fmt.Fprintln(e.out, "The Hub uses the new settings after a restart: sudo asdl-hub server restart")
			return nil
		}
		if !flags["--restart"] && term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprint(e.out, "Restart the Hub now to use them? [Y/n] ")
			var a string
			fmt.Fscanln(os.Stdin, &a)
			if a = strings.ToLower(strings.TrimSpace(a)); a != "" && a != "y" && a != "yes" {
				fmt.Fprintln(e.out, "Not restarted. Later: sudo asdl-hub server restart")
				return nil
			}
		} else if !flags["--restart"] {
			fmt.Fprintln(e.out, "The Hub uses the new settings after a restart: sudo asdl-hub server restart")
			return nil
		}
		return e.restartHub()
	}
	return usageError{"expected asdl-hub config [list|get|set|unset]"}
}

// applyEnvChanges sets or removes keys in .env lines, keeping everything else.
func applyEnvChanges(lines []string, changes map[string]*string) []string {
	done := map[string]bool{}
	out := make([]string, 0, len(lines)+len(changes))
	for _, line := range lines {
		t := strings.TrimPrefix(strings.TrimSpace(line), "export ")
		k, _, ok := strings.Cut(t, "=")
		k = strings.TrimSpace(k)
		if ok && !strings.HasPrefix(t, "#") {
			if v, change := changes[k]; change {
				done[k] = true
				if v != nil {
					out = append(out, k+"="+quoteEnv(*v))
				}
				continue
			}
		}
		out = append(out, line)
	}
	keys := make([]string, 0, len(changes))
	for k := range changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v := changes[k]; v != nil && !done[k] {
			out = append(out, k+"="+quoteEnv(*v))
		}
	}
	return out
}

// --- App settings ---

// app set <app> domain=... path=... image=... description=... ports=8080:80,...
func (e *env) appSet(c *client, name string, pairs []string) error {
	p, err := c.findApp(name)
	if err != nil {
		return err
	}
	if len(pairs) == 0 {
		return usageError{"expected asdl-hub app set <app> key=value ... (domain, path, image, description, ports)"}
	}
	body := map[string]interface{}{}
	for _, kv := range pairs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return usageError{fmt.Sprintf("%q: use key=value", kv)}
		}
		switch k {
		case "domain", "image", "description":
			if v == "" {
				return fmt.Errorf("%s can't be emptied from here", k)
			}
			body[k] = v
		case "path", "route_path":
			body["route_path"] = v
		case "ports":
			ports := []string{}
			for _, x := range strings.Split(v, ",") {
				if x = strings.TrimSpace(x); x != "" {
					ports = append(ports, x)
				}
			}
			body["ports"] = ports // empty: the Hub picks the port again
		case "node":
			n, err := c.findNode(v)
			if err != nil {
				return err
			}
			body["node_id"] = n.ID
		default:
			return usageError{fmt.Sprintf("unknown setting %q (domain, path, image, description, ports, node)", k)}
		}
	}
	if err := c.do("PUT", "/projects/"+p.ID, body, nil); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "Updated %s. Changes to the image, ports or node redeploy it; domain and path changes update the routes.\n", p.Name)
	return nil
}

type envVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (c *client) appEnv(p *project) ([]envVar, error) {
	var full struct {
		EnvVars []envVar `json:"env_vars"`
	}
	err := c.do("GET", "/projects/"+p.ID, nil, &full)
	return full.EnvVars, err
}

// env <app> | env set <app> KEY=VALUE ... | env unset <app> KEY ...
func (e *env) envCmd(c *client, args []string) error {
	if len(args) == 0 {
		return usageError{"expected asdl-hub env <app>, env set <app> KEY=VALUE ..., or env unset <app> KEY ..."}
	}
	sub := "list"
	if args[0] == "set" || args[0] == "unset" {
		sub, args = args[0], args[1:]
	}
	if len(args) == 0 {
		return usageError{"expected an app name"}
	}
	p, err := c.findApp(args[0])
	if err != nil {
		return err
	}
	vars, err := c.appEnv(p)
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		if e.json {
			return e.printJSON(vars)
		}
		if len(vars) == 0 {
			fmt.Fprintf(e.out, "%s has no environment variables.\n", p.Name)
			return nil
		}
		keys := make([]string, len(vars))
		for i, v := range vars {
			keys[i] = v.Key
		}
		sort.Strings(keys)
		fmt.Fprintf(e.out, "%s has %s (values are encrypted and never shown):\n", p.Name, plural(len(keys), "environment variable", "environment variables"))
		for _, k := range keys {
			fmt.Fprintln(e.out, "  "+k)
		}
		return nil
	case "set", "unset":
		names := make([]string, len(vars))
		have := map[string]bool{}
		for i, v := range vars {
			names[i] = v.Key
			have[v.Key] = true
		}
		valid := func(k string) error {
			if !envNameRe.MatchString(k) {
				return fmt.Errorf("%q isn't a valid variable name (letters, digits and _)", k)
			}
			if sub == "unset" && !have[k] {
				return fmt.Errorf("%s has no %s", p.Name, k)
			}
			return nil
		}
		// Every value is a secret: the Hub encrypts them all.
		changes, err := e.askChanges(sub, args[1:], names, func(string) bool { return true }, "variable", valid)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			fmt.Fprintln(e.out, "Nothing changed.")
			return nil
		}
		if !e.confirm(fmt.Sprintf("%s for %s, then redeploy it?", strings.ToUpper(changeNames(changes)[:1])+changeNames(changes)[1:], p.Name)) {
			fmt.Fprintln(e.out, "Nothing changed.")
			return nil
		}
		// The API returns values masked; sending a masked value keeps it.
		var out []envVar
		seen := map[string]bool{}
		for _, v := range vars {
			seen[v.Key] = true
			if ch, ok := changes[v.Key]; ok {
				if ch != nil {
					out = append(out, envVar{v.Key, *ch})
				}
				continue
			}
			out = append(out, v)
		}
		missing := []string{}
		for k, ch := range changes {
			if !seen[k] {
				if ch == nil {
					missing = append(missing, k)
				} else {
					out = append(out, envVar{k, *ch})
				}
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("%s has no %s", p.Name, strings.Join(missing, ", "))
		}
		if out == nil {
			out = []envVar{}
		}
		if err := c.do("PUT", "/projects/"+p.ID, map[string]interface{}{"env_vars": out}, nil); err != nil {
			return err
		}
		fmt.Fprintf(e.out, "Saved. %s is redeploying with the new environment.\n", p.Name)
		return nil
	}
	return nil
}
