package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// A doctor check's outcome.
const (
	levelOK = iota
	levelInfo
	levelWarn
	levelFail
)

type finding struct {
	Section string   `json:"section"`
	Level   string   `json:"level"`
	Message string   `json:"message"`
	Fix     []string `json:"fix,omitempty"`
}

// report prints checks as they finish and counts them.
type report struct {
	e        *env
	section  string
	counts   [4]int
	findings []finding
}

var levelNames = [4]string{"ok", "info", "warning", "problem"}

func (r *report) start(section string) {
	r.section = section
	if !r.e.json {
		fmt.Fprintf(r.e.out, "\n%s\n", r.e.paint("1", section))
	}
}

func (r *report) add(level int, msg string, fix ...string) {
	r.counts[level]++
	r.findings = append(r.findings, finding{r.section, levelNames[level], msg, fix})
	if r.e.json {
		return
	}
	mark := [4]string{r.e.paint(green, "✓"), r.e.paint(dim, "·"), r.e.paint(yellow, "!"), r.e.paint(red, "✗")}[level]
	fmt.Fprintf(r.e.out, "  %s %s\n", mark, msg)
	for _, f := range fix {
		fmt.Fprintf(r.e.out, "      %s %s\n", r.e.paint(dim, "→"), f)
	}
}

func (r *report) ok(format string, a ...interface{})   { r.add(levelOK, fmt.Sprintf(format, a...)) }
func (r *report) info(format string, a ...interface{}) { r.add(levelInfo, fmt.Sprintf(format, a...)) }

// finish prints the summary; it fails (exit 1) if a problem was found.
func (r *report) finish() error {
	if r.e.json {
		if err := r.e.printJSON(r.findings); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(r.e.out, "\n%s, %s, %s\n", plural(r.counts[levelOK], "ok", "ok"), plural(r.counts[levelWarn], "warning", "warnings"), plural(r.counts[levelFail], "problem", "problems"))
	}
	if r.counts[levelFail] > 0 {
		return errSilent
	}
	return nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// errSilent makes the command exit 1 without printing anything more.
var errSilent = fmt.Errorf("")

// doctor checks the Hub and everything it runs, and says what to fix.
func (e *env) doctor() error {
	r := &report{e: e}
	if !e.json {
		fmt.Fprintf(e.out, "ASDL Hub doctor\n")
	}

	r.start("Hub")
	c, err := newClient()
	if err != nil {
		r.add(levelFail, err.Error())
		return r.finish()
	}
	var me struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := c.do("GET", "/auth/me", nil, &me); err != nil {
		r.add(levelFail, err.Error(), "Is the Hub running? On its server: systemctl status asdl-hub; journalctl -u asdl-hub -n 50")
		return r.finish()
	}
	r.ok("Hub answers at %s, signed in as %s (%s)", c.url, me.Username, me.Role)
	local := c.where == "this machine's Hub"

	var ver versionInfo
	if c.do("GET", "/system/version?refresh=1", nil, &ver) == nil {
		switch {
		case ver.CheckError != "":
			r.add(levelWarn, "Running "+ver.Current+"; can't check for updates: "+ver.CheckError, "The Hub server needs to reach api.github.com")
		case ver.UpdateAvailable:
			r.add(levelWarn, fmt.Sprintf("Running %s; %s is available", ver.Current, ver.Latest), "asdl-hub update install")
		default:
			r.ok("Running %s, the latest release", ver.Current)
		}
	}
	if local {
		e.doctorServer(r)
	}

	nodes, err := c.nodes()
	if err != nil {
		r.add(levelFail, "Can't list nodes: "+err.Error())
		return r.finish()
	}
	apps, _ := c.projects()
	e.doctorNodes(r, c, nodes, apps)
	e.doctorApps(r, nodes, apps)
	e.doctorDomains(r, c, apps, local)
	e.doctorJobs(r, c, nodes)
	if me.Role == "admin" {
		e.doctorNotifications(r, c)
	}
	return r.finish()
}

// doctorServer checks the machine the Hub runs on (only when run there).
func (e *env) doctorServer(r *report) {
	r.start("This server")
	if out, err := exec.Command("systemctl", "is-active", "asdl-hub").Output(); err == nil && strings.TrimSpace(string(out)) == "active" {
		r.ok("The asdl-hub service is running")
	} else {
		r.add(levelWarn, "systemd doesn't report asdl-hub as active", "systemctl status asdl-hub")
	}
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		r.add(levelFail, "nginx's config has an error: "+lastLine(string(out)), "sudo nginx -t, then fix or remove the site it names")
	} else {
		r.ok("nginx's config is valid")
	}
	if out, err := exec.Command("systemctl", "is-active", "nginx").Output(); err != nil || strings.TrimSpace(string(out)) != "active" {
		r.add(levelFail, "nginx isn't running, so no app is reachable", "sudo systemctl start nginx; journalctl -u nginx -n 30")
	}
	var st syscall.Statfs_t
	if syscall.Statfs("/", &st) == nil && st.Blocks > 0 {
		free := float64(st.Bavail) / float64(st.Blocks) * 100
		gbFree := float64(st.Bavail) * float64(st.Bsize) / (1 << 30)
		switch {
		case free < 5:
			r.add(levelFail, fmt.Sprintf("Disk almost full: %.1f GB (%.0f%%) free", gbFree, free), "Free space: sudo journalctl --vacuum-size=200M; sudo apt-get clean")
		case free < 15:
			r.add(levelWarn, fmt.Sprintf("Disk getting full: %.1f GB (%.0f%%) free", gbFree, free))
		default:
			r.ok("Disk: %.1f GB (%.0f%%) free", gbFree, free)
		}
	}
	if b, err := os.ReadFile("/var/log/asdl-hub-upgrade.log"); err == nil {
		log := string(b)
		if strings.Contains(log, "Upgrading ASDL Hub") && !strings.Contains(log, "finished at") {
			r.add(levelWarn, "The last update didn't finish", "Read /var/log/asdl-hub-upgrade.log, then try again: asdl-hub update install")
		}
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func (e *env) doctorNodes(r *report, c *client, nodes []node, apps []project) {
	r.start("Nodes")
	if len(nodes) == 0 {
		r.add(levelFail, "No nodes yet, so nothing can run", "Add one: https://docs.asdl.website/hub/add-a-node/")
		return
	}
	latestAgent := latestRelease("asadullahbro/asdl-agent")
	appsOn := map[string][]string{}
	for _, p := range apps {
		appsOn[p.NodeID] = append(appsOn[p.NodeID], p.Name)
	}
	online := 0
	for _, n := range nodes {
		if !n.Online {
			if len(appsOn[n.ID]) > 0 {
				r.add(levelFail, fmt.Sprintf("%s is offline (last seen %s) and still has apps: %s", n.Hostname, ago(n.LastHeartbeat), strings.Join(appsOn[n.ID], ", ")),
					"They move by themselves if they stop answering; check the node: asdl-agent doctor (on it)")
			} else {
				r.add(levelWarn, fmt.Sprintf("%s is offline (last seen %s)", n.Hostname, ago(n.LastHeartbeat)),
					"If the machine is on: run asdl-agent doctor on it. If it's gone for good, remove it in the dashboard.")
			}
			continue
		}
		online++
		var conn struct {
			WGHandshake *time.Time `json:"wg_handshake"`
			WGError     string     `json:"wg_error"`
		}
		_ = c.do("GET", "/nodes/"+n.ID+"/connection", nil, &conn)
		problems := []string{}
		var fixes []string
		if conn.WGHandshake != nil && !conn.WGHandshake.IsZero() && time.Since(*conn.WGHandshake) > 3*time.Minute {
			problems = append(problems, "WireGuard handshake "+ago(*conn.WGHandshake))
			fixes = append(fixes, "On "+n.Hostname+": sudo wg show; check the Hub's UDP port is open in its cloud firewall")
		}
		if n.Maintenance {
			problems = append(problems, "in maintenance")
			fixes = append(fixes, "End it when you're done: asdl-hub maintenance "+n.Hostname+" off")
		}
		if n.DiskTotal > 0 && float64(n.DiskTotal-n.DiskUsed)/float64(n.DiskTotal) < 0.1 {
			problems = append(problems, fmt.Sprintf("disk %s/%s GB used", gb(n.DiskUsed), gb(n.DiskTotal)))
			fixes = append(fixes, "On "+n.Hostname+": docker system prune (removes unused images)")
		}
		if latestAgent != "" && n.AgentVersion != "" && n.AgentVersion != latestAgent && strings.HasPrefix(n.AgentVersion, "v2") {
			problems = append(problems, "agent "+n.AgentVersion+", "+latestAgent+" is out")
			fixes = append(fixes, "On "+n.Hostname+": asdl-agent update install (or Settings → Agent update → Deploy agents)")
		}
		if len(problems) == 0 {
			r.ok("%s is online (agent %s, %d apps)", n.Hostname, orDash(n.AgentVersion), len(appsOn[n.ID]))
		} else {
			r.add(levelWarn, n.Hostname+" is online: "+strings.Join(problems, ", "), fixes...)
		}
	}
	if online == 1 && len(apps) > 0 {
		r.add(levelWarn, "Only one node is online, so apps have nowhere to fail over to")
	}
}

func (e *env) doctorApps(r *report, nodes []node, apps []project) {
	r.start("Apps")
	if len(apps) == 0 {
		r.info("No apps yet")
		return
	}
	names := nodeNames(nodes)
	for _, p := range apps {
		switch {
		case p.Status == "failed":
			r.add(levelFail, p.Name+" has failed: no node could run it",
				"See why: asdl-hub jobs, then asdl-hub job <id>; redeploy with asdl-hub deploy "+p.Name)
		case p.HealthStatus == "unhealthy":
			r.add(levelFail, p.Name+" is unhealthy on "+orDash(names[p.NodeID]), "asdl-hub logs "+p.Name)
		case p.HealthStatus == "degraded" || p.HealthStatus == "migrating":
			r.add(levelWarn, p.Name+" is "+p.HealthStatus+" on "+orDash(names[p.NodeID]), "asdl-hub logs "+p.Name)
		case p.Status == "deploying":
			r.info("%s is deploying", p.Name)
		case p.NodeID == "":
			r.info("%s isn't running anywhere yet (no image deployed)", p.Name)
		default:
			r.ok("%s is %s on %s", p.Name, p.HealthStatus, orDash(names[p.NodeID]))
		}
	}
}

// doctorDomains checks every app and public plugin domain resolves to the
// Hub and answers over HTTPS with a certificate that isn't about to expire.
func (e *env) doctorDomains(r *report, c *client, apps []project, local bool) {
	type site struct{ owner, domain, path string }
	var sites []site
	for _, p := range apps {
		if p.Domain != "" {
			sites = append(sites, site{p.Name, p.Domain, p.RoutePath})
		}
		var plugins []struct {
			PluginID  string `json:"plugin_id"`
			Domain    string `json:"domain"`
			RoutePath string `json:"route_path"`
		}
		_ = c.do("GET", "/projects/"+p.ID+"/plugins", nil, &plugins)
		for _, pl := range plugins {
			if pl.Domain != "" {
				sites = append(sites, site{p.Name + "'s " + pl.PluginID, pl.Domain, pl.RoutePath})
			}
		}
	}
	if len(sites) == 0 {
		return
	}
	r.start("Domains")
	hubHost := ""
	if local {
		hubHost = hostOf(envFileValue(filepath.Join(installDir(), ".env"), "PUBLIC_URL"))
	} else {
		hubHost = hostOf(c.url)
	}
	hubIPs := map[string]bool{}
	if hubHost != "" {
		if ip := net.ParseIP(hubHost); ip != nil {
			hubIPs[ip.String()] = true
		} else if addrs, err := net.LookupHost(hubHost); err == nil {
			for _, a := range addrs {
				hubIPs[a] = true
			}
		}
	}

	type result struct {
		level int
		msg   string
		fix   []string
	}
	results := make([]result, len(sites))
	var wg sync.WaitGroup
	for i, s := range sites {
		wg.Add(1)
		go func(i int, s site) {
			defer wg.Done()
			label := s.domain + strings.TrimSuffix(s.path, "/") + " (" + s.owner + ")"
			addrs, err := net.LookupHost(s.domain)
			if err != nil || len(addrs) == 0 {
				results[i] = result{levelFail, label + ": the domain doesn't resolve", []string{"Add a DNS A record for " + s.domain + " pointing at the Hub server"}}
				return
			}
			if len(hubIPs) > 0 {
				match := false
				for _, a := range addrs {
					if hubIPs[a] {
						match = true
					}
				}
				if !match {
					hub := make([]string, 0, len(hubIPs))
					for ip := range hubIPs {
						hub = append(hub, ip)
					}
					sort.Strings(hub)
					results[i] = result{levelFail, fmt.Sprintf("%s: points at %s, not the Hub (%s)", label, strings.Join(addrs, ", "), strings.Join(hub, ", ")),
						[]string{"Point " + s.domain + " at the Hub server (DNS A record " + hub[0] + "; proxy off if it's on Cloudflare)"}}
					return
				}
			}
			cl := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			resp, err := cl.Get("https://" + s.domain + s.path)
			if err != nil {
				fix := []string{"Look for certificate errors: journalctl -u asdl-hub | grep -i certificate"}
				results[i] = result{levelFail, label + ": HTTPS fails: " + unwrapURLError(err).Error(), fix}
				return
			}
			resp.Body.Close()
			left := time.Duration(0)
			if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
				left = time.Until(resp.TLS.PeerCertificates[0].NotAfter)
			}
			switch {
			case resp.StatusCode >= 500:
				results[i] = result{levelFail, fmt.Sprintf("%s: answers %d", label, resp.StatusCode), []string{"The app isn't answering through the Hub: asdl-hub logs " + strings.Split(s.owner, "'")[0]}}
			case left > 0 && left < 10*24*time.Hour:
				results[i] = result{levelWarn, fmt.Sprintf("%s: certificate expires in %d days", label, int(left.Hours()/24)), []string{"The Hub renews it by itself; if it doesn't, check journalctl -u asdl-hub | grep -i certificate"}}
			default:
				results[i] = result{levelOK, fmt.Sprintf("%s: HTTPS %d, certificate valid %d more days", label, resp.StatusCode, int(left.Hours()/24)), nil}
			}
		}(i, s)
	}
	wg.Wait()
	for _, res := range results {
		r.add(res.level, res.msg, res.fix...)
	}
}

func installDir() string {
	if d := os.Getenv("ASDL_HUB_DIR"); d != "" {
		return d
	}
	return DefaultInstallDir
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (e *env) doctorJobs(r *report, c *client, nodes []node) {
	r.start("Jobs")
	var res struct {
		Data []job `json:"data"`
	}
	if err := c.do("GET", "/jobs?page=1&limit=200", nil, &res); err != nil {
		r.add(levelWarn, "Can't list jobs: "+err.Error())
		return
	}
	names := nodeNames(nodes)
	online := map[string]bool{}
	for _, n := range nodes {
		online[n.ID] = n.Online
	}
	var failed []job
	stuck := 0
	for _, j := range res.Data {
		if time.Since(j.CreatedAt) > 24*time.Hour {
			continue
		}
		if j.Status == "failed" {
			failed = append(failed, j)
		}
		if j.Status == "pending" && time.Since(j.CreatedAt) > 10*time.Minute && online[j.NodeID] {
			stuck++
		}
	}
	if len(failed) == 0 {
		r.ok("No failed jobs in the last 24 hours")
	} else {
		last := failed[0]
		r.add(levelWarn, fmt.Sprintf("%d failed job(s) in the last 24 hours, the latest a %s on %s %s", len(failed), last.Type, orDash(names[last.NodeID]), ago(last.CreatedAt)),
			"asdl-hub job "+short(last.ID))
	}
	if stuck > 0 {
		r.add(levelWarn, fmt.Sprintf("%d job(s) waiting over 10 minutes for an online node", stuck), "The node's agent may be stuck: asdl-agent doctor (on it)")
	}
}

func (e *env) doctorNotifications(r *report, c *client) {
	r.start("Notifications")
	var list []channel
	if err := c.do("GET", "/notifications", nil, &list); err != nil {
		return
	}
	if len(list) == 0 {
		r.info("No notification channels: you won't hear about failures (Plugins → Notifications)")
		return
	}
	for _, ch := range list {
		switch {
		case !ch.Enabled:
			r.info("%s (%s) is switched off", ch.Name, ch.Type)
		case ch.LastError != "":
			r.add(levelWarn, fmt.Sprintf("%s (%s): last send failed: %s", ch.Name, ch.Type, truncate(ch.LastError, 100)),
				"Fix its settings, then: asdl-hub notify test \""+ch.Name+"\"")
		default:
			r.ok("%s (%s) works", ch.Name, ch.Type)
		}
	}
}

// latestRelease returns a GitHub repo's latest release tag, or "" (tests replace it).
var latestRelease = func(repo string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if json.NewDecoder(resp.Body).Decode(&rel) != nil {
		return ""
	}
	return rel.TagName
}
