package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

type node struct {
	ID               string     `json:"id"`
	Hostname         string     `json:"hostname"`
	VPNIP            string     `json:"vpn_ip"`
	Online           bool       `json:"online"`
	Maintenance      bool       `json:"maintenance"`
	AgentVersion     string     `json:"agent_version"`
	CPUCores         int        `json:"cpu_cores"`
	MemoryTotal      int64      `json:"memory_total"`
	MemoryUsed       int64      `json:"memory_used"`
	DiskTotal        int64      `json:"disk_total"`
	DiskUsed         int64      `json:"disk_used"`
	LastHeartbeat    time.Time  `json:"last_heartbeat"`
	PingLatency      float64    `json:"ping_latency"`
	HealthScore      float64    `json:"health_score"`
	MaintenanceSince *time.Time `json:"maintenance_since"`
}

type project struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Domain       string    `json:"domain"`
	RoutePath    string    `json:"route_path"`
	Repository   string    `json:"repository"`
	NodeID       string    `json:"node_id"`
	Status       string    `json:"status"`
	HealthStatus string    `json:"health_status"`
	Image        string    `json:"image"`
	Ports        []string  `json:"ports"`
	LastDeployed time.Time `json:"last_deployed"`
	EnvVars      []struct {
		Key string `json:"key"`
	} `json:"env_vars"`
}

type job struct {
	ID          string     `json:"id"`
	NodeID      string     `json:"node_id"`
	Type        string     `json:"type"`
	Status      string     `json:"status"`
	ExitCode    int        `json:"exit_code"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	Logs        string     `json:"logs"`
}

func (e *env) printJSON(v interface{}) error {
	enc := json.NewEncoder(e.out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func (c *client) nodes() ([]node, error) {
	var out []node
	return out, c.do("GET", "/nodes", nil, &out)
}

func (c *client) projects() ([]project, error) {
	var out struct {
		Data []project `json:"data"`
	}
	return out.Data, c.do("GET", "/projects?page=1&limit=500", nil, &out)
}

// findApp finds an app by name or ID (or a unique ID prefix).
func (c *client) findApp(name string) (*project, error) {
	list, err := c.projects()
	if err != nil {
		return nil, err
	}
	var prefix []project
	for i := range list {
		if list[i].Name == name || list[i].ID == name {
			return &list[i], nil
		}
		if len(name) >= 4 && strings.HasPrefix(list[i].ID, name) {
			prefix = append(prefix, list[i])
		}
	}
	if len(prefix) == 1 {
		return &prefix[0], nil
	}
	names := make([]string, len(list))
	for i, p := range list {
		names[i] = p.Name
	}
	return nil, fmt.Errorf("no app called %q (apps: %s)", name, strings.Join(names, ", "))
}

func (c *client) findNode(name string) (*node, error) {
	list, err := c.nodes()
	if err != nil {
		return nil, err
	}
	var loose []node
	for i := range list {
		n := list[i]
		if n.Hostname == name || n.ID == name || n.VPNIP == name {
			return &list[i], nil
		}
		if strings.EqualFold(n.Hostname, name) || strings.HasPrefix(strings.ToLower(n.Hostname), strings.ToLower(name)+".") ||
			(len(name) >= 4 && strings.HasPrefix(n.ID, name)) {
			loose = append(loose, n)
		}
	}
	if len(loose) == 1 {
		return &loose[0], nil
	}
	names := make([]string, len(list))
	for i, n := range list {
		names[i] = n.Hostname
	}
	return nil, fmt.Errorf("no node called %q (nodes: %s)", name, strings.Join(names, ", "))
}

func nodeNames(list []node) map[string]string {
	m := map[string]string{}
	for _, n := range list {
		m[n.ID] = n.Hostname
	}
	return m
}

func (e *env) nodeState(n node) string {
	switch {
	case !n.Online:
		return e.paint(red, "offline")
	case n.Maintenance:
		return e.paint(yellow, "maintenance")
	default:
		return e.paint(green, "online")
	}
}

func (e *env) health(h string) string {
	switch h {
	case "healthy":
		return e.paint(green, h)
	case "unhealthy":
		return e.paint(red, h)
	case "degraded", "migrating":
		return e.paint(yellow, h)
	}
	return e.paint(dim, h)
}

func gb(b int64) string {
	return fmt.Sprintf("%.1f", float64(b)/(1<<30))
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func appAddress(p project) string {
	if p.Domain == "" {
		return "-"
	}
	return p.Domain + strings.TrimSuffix(p.RoutePath, "/")
}

// --- commands ---

func (e *env) status(c *client) error {
	var stats map[string]interface{}
	if err := c.do("GET", "/stats?since="+url.QueryEscape(midnight().Format(time.RFC3339)), nil, &stats); err != nil {
		return err
	}
	var ver struct {
		Current         string `json:"current"`
		Latest          string `json:"latest"`
		UpdateAvailable bool   `json:"update_available"`
	}
	_ = c.do("GET", "/system/version", nil, &ver)
	if e.json {
		return e.printJSON(map[string]interface{}{"stats": stats, "version": ver, "hub": c.url})
	}
	num := func(k string) int { f, _ := stats[k].(float64); return int(f) }
	update := e.paint(green, "up to date")
	if ver.UpdateAvailable {
		update = e.paint(yellow, ver.Latest+" available: asdl-hub update install")
	}
	w := e.table()
	fmt.Fprintf(w, "Hub\t%s\t%s (%s)\n", c.url, ver.Current, update)
	fmt.Fprintf(w, "Nodes\t%d/%d online\t\n", num("onlineNodes"), num("nodes"))
	apps := fmt.Sprintf("%d healthy", num("healthyProjects"))
	if u := num("unhealthyProjects"); u > 0 {
		apps += ", " + e.paint(red, fmt.Sprintf("%d unhealthy", u))
	}
	fmt.Fprintf(w, "Apps\t%d\t%s\n", num("projects"), apps)
	jobs := fmt.Sprintf("%d succeeded", num("success"))
	if f := num("failed"); f > 0 {
		jobs += ", " + e.paint(red, fmt.Sprintf("%d failed", f))
	}
	if r := num("running") + num("pending"); r > 0 {
		jobs += fmt.Sprintf(", %d running or waiting", r)
	}
	fmt.Fprintf(w, "Jobs today\t%d\t%s\n", num("jobs"), jobs)
	return w.Flush()
}

func midnight() time.Time {
	y, m, d := time.Now().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

func (e *env) nodes(c *client) error {
	list, err := c.nodes()
	if err != nil {
		return err
	}
	if e.json {
		return e.printJSON(list)
	}
	apps, _ := c.projects()
	count := map[string]int{}
	for _, p := range apps {
		count[p.NodeID]++
	}
	w := e.table()
	fmt.Fprintln(w, "NODE\tSTATE\tADDRESS\tAGENT\tMEMORY GB\tDISK GB\tAPPS\tLAST SEEN")
	for _, n := range list {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s/%s\t%s/%s\t%d\t%s\n", n.Hostname, e.nodeState(n), n.VPNIP, orDash(n.AgentVersion),
			gb(n.MemoryUsed), gb(n.MemoryTotal), gb(n.DiskUsed), gb(n.DiskTotal), count[n.ID], ago(n.LastHeartbeat))
	}
	return w.Flush()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (e *env) apps(c *client) error {
	list, err := c.projects()
	if err != nil {
		return err
	}
	if e.json {
		return e.printJSON(list)
	}
	nodes, _ := c.nodes()
	names := nodeNames(nodes)
	w := e.table()
	fmt.Fprintln(w, "APP\tNODE\tSTATUS\tHEALTH\tADDRESS\tDEPLOYED")
	for _, p := range list {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", p.Name, orDash(names[p.NodeID]), p.Status, e.health(p.HealthStatus),
			appAddress(p), ago(p.LastDeployed))
	}
	return w.Flush()
}

func (e *env) app(c *client, name string) error {
	p, err := c.findApp(name)
	if err != nil {
		return err
	}
	var plugins []struct {
		PluginID  string `json:"plugin_id"`
		Domain    string `json:"domain"`
		RoutePath string `json:"route_path"`
		HostPort  int    `json:"host_port"`
	}
	_ = c.do("GET", "/projects/"+p.ID+"/plugins", nil, &plugins)
	if e.json {
		return e.printJSON(map[string]interface{}{"app": p, "plugins": plugins})
	}
	nodes, _ := c.nodes()
	names := nodeNames(nodes)
	w := e.table()
	fmt.Fprintf(w, "App\t%s\n", p.Name)
	fmt.Fprintf(w, "ID\t%s\n", p.ID)
	fmt.Fprintf(w, "Node\t%s\n", orDash(names[p.NodeID]))
	fmt.Fprintf(w, "Status\t%s, %s\n", p.Status, e.health(p.HealthStatus))
	fmt.Fprintf(w, "Address\t%s\n", appAddress(*p))
	fmt.Fprintf(w, "Image\t%s\n", orDash(p.Image))
	fmt.Fprintf(w, "Ports\t%s\n", orDash(strings.Join(p.Ports, ", ")))
	fmt.Fprintf(w, "Repository\t%s\n", orDash(p.Repository))
	fmt.Fprintf(w, "Deployed\t%s\n", ago(p.LastDeployed))
	keys := make([]string, len(p.EnvVars))
	for i, v := range p.EnvVars {
		keys[i] = v.Key
	}
	sort.Strings(keys)
	fmt.Fprintf(w, "Env vars\t%s\n", orDash(strings.Join(keys, ", ")))
	for i, pl := range plugins {
		label := ""
		if i == 0 {
			label = "Plugins"
		}
		extra := ""
		if pl.Domain != "" {
			extra = " (" + pl.Domain + strings.TrimSuffix(pl.RoutePath, "/") + ")"
		}
		fmt.Fprintf(w, "%s\t%s%s\n", label, pl.PluginID, extra)
	}
	return w.Flush()
}

func (e *env) deploy(c *client, name string) error {
	p, err := c.findApp(name)
	if err != nil {
		return err
	}
	var res struct {
		JobID string `json:"job_id"`
	}
	if err := c.do("POST", "/projects/"+p.ID+"/redeploy", nil, &res); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "Deploying %s…\n", p.Name)
	return e.follow(c, res.JobID, 10*time.Minute, false)
}

func (e *env) move(c *client, app, to string) error {
	p, err := c.findApp(app)
	if err != nil {
		return err
	}
	n, err := c.findNode(to)
	if err != nil {
		return err
	}
	if n.ID == p.NodeID {
		fmt.Fprintf(e.out, "%s already runs on %s.\n", p.Name, n.Hostname)
		return nil
	}
	var res map[string]interface{}
	if err := c.do("POST", "/migrations", map[string]string{"project_id": p.ID, "target_node_id": n.ID}, &res); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "Moving %s to %s…\n", p.Name, n.Hostname)
	jobID, _ := res["start_job_id"].(string)
	if jobID == "" {
		if m, ok := res["migration"].(map[string]interface{}); ok {
			jobID, _ = m["job_id"].(string)
		}
	}
	if jobID == "" {
		fmt.Fprintln(e.out, "Started; follow it with `asdl-hub jobs`.")
		return nil
	}
	return e.follow(c, jobID, 10*time.Minute, false)
}

func (e *env) restart(c *client, name string) error {
	p, err := c.findApp(name)
	if err != nil {
		return err
	}
	if p.NodeID == "" {
		return fmt.Errorf("%s isn't running on any node", p.Name)
	}
	var res struct {
		JobID string `json:"job_id"`
	}
	if err := c.do("POST", "/nodes/"+p.NodeID+"/containers/"+url.PathEscape(p.Name)+"/restart", nil, &res); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "Restarting %s…\n", p.Name)
	return e.follow(c, res.JobID, 2*time.Minute, false)
}

func (e *env) logs(c *client, name string, lines int) error {
	p, err := c.findApp(name)
	if err != nil {
		return err
	}
	if p.NodeID == "" {
		return fmt.Errorf("%s isn't running on any node", p.Name)
	}
	var res struct {
		JobID string `json:"job_id"`
	}
	if err := c.do("POST", fmt.Sprintf("/nodes/%s/containers/%s/logs?lines=%d", p.NodeID, url.PathEscape(p.Name), lines), nil, &res); err != nil {
		return err
	}
	return e.follow(c, res.JobID, time.Minute, true)
}

// follow waits for a job and prints its output (quiet: only the output).
func (e *env) follow(c *client, jobID string, timeout time.Duration, quiet bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(1500 * time.Millisecond)
		var j job
		if err := c.do("GET", "/jobs/"+jobID, nil, &j); err != nil {
			return err
		}
		switch j.Status {
		case "completed", "failed", "cancelled":
			var l struct {
				Logs string `json:"logs"`
			}
			_ = c.do("GET", "/jobs/"+jobID+"/logs", nil, &l)
			out := JobOutput(l.Logs)
			if quiet {
				if out != "" {
					fmt.Fprintln(e.out, out)
				}
			} else if j.Status != "completed" && out != "" {
				fmt.Fprintln(e.out, out)
			}
			if j.Status != "completed" {
				return fmt.Errorf("job %s %s (see `asdl-hub job %s`)", short(jobID), j.Status, short(jobID))
			}
			if !quiet {
				fmt.Fprintln(e.out, e.paint(green, "Done."))
			}
			return nil
		}
	}
	return fmt.Errorf("still running after %s; follow it with `asdl-hub job %s`", timeout, short(jobID))
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

var jobOutRe = regexp.MustCompile(`(?s)\nSTDOUT:\n(.*?)(?:\nSTDERR:\n(.*))?$`)

// JobOutput returns what a job printed: the agent's final STDOUT/STDERR
// copy, or else the streamed part between its header and footer.
func JobOutput(logs string) string {
	if i := strings.Index(logs, "\nCompleted at:"); i >= 0 {
		if m := jobOutRe.FindStringSubmatch(logs[i:]); m != nil {
			parts := []string{}
			for _, s := range m[1:] {
				if s = strings.TrimRight(s, "\n "); s != "" {
					parts = append(parts, s)
				}
			}
			return strings.Join(parts, "\n")
		}
	}
	body := logs
	if i := strings.Index(body, "====================================\n\n"); i >= 0 {
		body = body[i+len("====================================\n\n"):]
	}
	if i := strings.Index(body, "\n====================================\nCompleted at:"); i >= 0 {
		body = body[:i]
	}
	return strings.TrimRight(body, "\n ")
}

func (e *env) maintenance(c *client, name string, on bool) error {
	n, err := c.findNode(name)
	if err != nil {
		return err
	}
	var res struct {
		Moving []string `json:"moving"`
		Stays  []string `json:"stays"`
	}
	if err := c.do("PUT", "/nodes/"+n.ID+"/maintenance", map[string]bool{"enabled": on}, &res); err != nil {
		return err
	}
	if e.json {
		return e.printJSON(res)
	}
	if !on {
		fmt.Fprintf(e.out, "%s is out of maintenance and takes apps again.\n", n.Hostname)
		return nil
	}
	fmt.Fprintf(e.out, "%s is in maintenance: it takes no new apps.\n", n.Hostname)
	for _, m := range res.Moving {
		fmt.Fprintf(e.out, "  moving  %s\n", m)
	}
	for _, s := range res.Stays {
		fmt.Fprintf(e.out, "  %s    %s (no other node can take it)\n", e.paint(yellow, "stays"), s)
	}
	return nil
}

func (e *env) jobs(c *client, n int) error {
	var res struct {
		Data []job `json:"data"`
	}
	if err := c.do("GET", fmt.Sprintf("/jobs?page=1&limit=%d", n), nil, &res); err != nil {
		return err
	}
	if e.json {
		return e.printJSON(res.Data)
	}
	nodes, _ := c.nodes()
	names := nodeNames(nodes)
	w := e.table()
	fmt.Fprintln(w, "JOB\tTYPE\tSTATUS\tNODE\tCREATED\tTOOK")
	for _, j := range res.Data {
		took := "-"
		if j.StartedAt != nil && j.CompletedAt != nil {
			took = j.CompletedAt.Sub(*j.StartedAt).Round(100 * time.Millisecond).String()
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", short(j.ID), j.Type, e.jobStatus(j.Status), orDash(names[j.NodeID]), ago(j.CreatedAt), took)
	}
	return w.Flush()
}

func (e *env) jobStatus(s string) string {
	switch s {
	case "completed":
		return e.paint(green, s)
	case "failed", "cancelled":
		return e.paint(red, s)
	}
	return e.paint(yellow, s)
}

func (e *env) job(c *client, id string) error {
	// Accept the short IDs `jobs` prints.
	if len(id) < 36 {
		var res struct {
			Data []job `json:"data"`
		}
		if err := c.do("GET", "/jobs?page=1&limit=200", nil, &res); err != nil {
			return err
		}
		for _, j := range res.Data {
			if strings.HasPrefix(j.ID, id) {
				id = j.ID
				break
			}
		}
	}
	var j job
	if err := c.do("GET", "/jobs/"+id, nil, &j); err != nil {
		return err
	}
	var l struct {
		Logs string `json:"logs"`
	}
	_ = c.do("GET", "/jobs/"+id+"/logs", nil, &l)
	if e.json {
		j.Logs = l.Logs
		return e.printJSON(j)
	}
	nodes, _ := c.nodes()
	w := e.table()
	fmt.Fprintf(w, "Job\t%s\n", j.ID)
	fmt.Fprintf(w, "Type\t%s\n", j.Type)
	fmt.Fprintf(w, "Status\t%s (exit code %d)\n", e.jobStatus(j.Status), j.ExitCode)
	fmt.Fprintf(w, "Node\t%s\n", orDash(nodeNames(nodes)[j.NodeID]))
	fmt.Fprintf(w, "Created\t%s\n", j.CreatedAt.Local().Format("2006-01-02 15:04:05"))
	w.Flush()
	if out := JobOutput(l.Logs); out != "" {
		fmt.Fprintln(e.out)
		fmt.Fprintln(e.out, out)
	}
	return nil
}

type channel struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Events     []string   `json:"events"`
	Enabled    bool       `json:"enabled"`
	LastSentAt *time.Time `json:"last_sent_at"`
	LastError  string     `json:"last_error"`
}

func (e *env) notifyList(c *client) error {
	var list []channel
	if err := c.do("GET", "/notifications", nil, &list); err != nil {
		return err
	}
	if e.json {
		return e.printJSON(list)
	}
	if len(list) == 0 {
		fmt.Fprintln(e.out, "No notification channels. Add one in the dashboard: Plugins → Notifications.")
		return nil
	}
	w := e.table()
	fmt.Fprintln(w, "CHANNEL\tPLUGIN\tON\tEVENTS\tLAST SENT\tLAST ERROR")
	for _, ch := range list {
		on := e.paint(green, "yes")
		if !ch.Enabled {
			on = e.paint(dim, "no ")
		}
		last := "never"
		if ch.LastSentAt != nil {
			last = ago(*ch.LastSentAt)
		}
		errText := "-"
		if ch.LastError != "" {
			errText = e.paint(red, truncate(ch.LastError, 50))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n", ch.Name, ch.Type, on, len(ch.Events), last, errText)
	}
	return w.Flush()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func (e *env) notifyTest(c *client, name string) error {
	var list []channel
	if err := c.do("GET", "/notifications", nil, &list); err != nil {
		return err
	}
	for _, ch := range list {
		if strings.EqualFold(ch.Name, name) || ch.ID == name {
			if err := c.do("POST", "/notifications/"+ch.ID+"/test", nil, nil); err != nil {
				return fmt.Errorf("%s: %v", ch.Name, err)
			}
			fmt.Fprintf(e.out, "Test sent to %s.\n", ch.Name)
			return nil
		}
	}
	return fmt.Errorf("no notification channel called %q (see `asdl-hub notify`)", name)
}

type versionInfo struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
	CanUpdate       bool   `json:"can_update"`
	ReleaseURL      string `json:"release_url"`
	CheckError      string `json:"check_error"`
	Upgrading       string `json:"upgrading"`
}

func (e *env) updateStatus(c *client) error {
	var v versionInfo
	if err := c.do("GET", "/system/version?refresh=1", nil, &v); err != nil {
		return err
	}
	if e.json {
		return e.printJSON(v)
	}
	switch {
	case v.CheckError != "":
		fmt.Fprintf(e.out, "Running %s; couldn't check for updates: %s\n", v.Current, v.CheckError)
	case v.UpdateAvailable:
		fmt.Fprintf(e.out, "%s is available (running %s): %s\nInstall it with `asdl-hub update install`.\n", v.Latest, v.Current, v.ReleaseURL)
	default:
		fmt.Fprintf(e.out, "Running %s, the latest release.\n", v.Current)
	}
	return nil
}

func (e *env) updateInstall(c *client) error {
	var v versionInfo
	if err := c.do("GET", "/system/version?refresh=1", nil, &v); err != nil {
		return err
	}
	if !v.UpdateAvailable {
		fmt.Fprintf(e.out, "Running %s, the latest release.\n", v.Current)
		return nil
	}
	if err := c.do("POST", "/system/update", nil, nil); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "Installing %s (the Hub restarts; this takes a minute)…\n", v.Latest)
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)
		var now versionInfo
		if c.do("GET", "/system/version", nil, &now) == nil && now.Current == v.Latest {
			fmt.Fprintln(e.out, e.paint(green, "Now running "+now.Current+"."))
			return nil
		}
	}
	return fmt.Errorf("the Hub isn't on %s after 5 minutes; check `journalctl -u asdl-hub` on its server", v.Latest)
}

func (e *env) whoami(c *client) error {
	var me struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := c.do("GET", "/auth/me", nil, &me); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "%s (%s) on %s, using %s\n", me.Username, me.Role, c.url, c.where)
	return nil
}
