package cli

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Snapshots keep a copy of a plugin's data (e.g. Redis) on the Hub, so the data survives the node failing or a move.

type snapItem struct {
	ID         string    `json:"id"`
	Generation int64     `json:"generation"`
	Trigger    string    `json:"trigger"`
	SizeBytes  int64     `json:"size_bytes"`
	CreatedAt  time.Time `json:"created_at"`
}

type snapPlugin struct {
	PluginID        string     `json:"plugin_id"`
	Persist         bool       `json:"persist"`
	Snapshots       bool       `json:"snapshots"`
	IntervalMinutes int        `json:"interval_minutes"`
	LastSnapshotAt  *time.Time `json:"last_snapshot_at"`
	Healthy         bool       `json:"healthy"`
	Items           []snapItem `json:"items"`
}

type snapList struct {
	Enabled bool         `json:"enabled"`
	Keep    int          `json:"keep"`
	MaxMB   int          `json:"max_mb"`
	Plugins []snapPlugin `json:"plugins"`
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func (e *env) loadSnapshots(c *client, app string) (*project, *snapList, error) {
	p, err := c.findApp(app)
	if err != nil {
		return nil, nil, err
	}
	var l snapList
	if err := c.do("GET", "/projects/"+url.PathEscape(p.ID)+"/snapshots", nil, &l); err != nil {
		return nil, nil, err
	}
	return p, &l, nil
}

// pick chooses the plugin to act on: the named one, or the only one that can keep data.
func pickSnapPlugin(l *snapList, name string) (*snapPlugin, error) {
	if len(l.Plugins) == 0 {
		return nil, errors.New("this app has no plugin that can keep its data (attach Redis first)")
	}
	if name == "" {
		if len(l.Plugins) == 1 {
			return &l.Plugins[0], nil
		}
		names := ""
		for _, p := range l.Plugins {
			names += " " + p.PluginID
		}
		return nil, fmt.Errorf("this app has several plugins that keep data; name one:%s", names)
	}
	for i := range l.Plugins {
		if l.Plugins[i].PluginID == name {
			return &l.Plugins[i], nil
		}
	}
	return nil, fmt.Errorf("this app has no plugin %q that can keep data", name)
}

// snapshots lists an app's snapshots.
func (e *env) snapshots(c *client, app string) error {
	_, l, err := e.loadSnapshots(c, app)
	if err != nil {
		return err
	}
	if e.json {
		return e.printJSON(l)
	}
	if !l.Enabled {
		fmt.Fprintln(e.out, "Snapshots are switched off on this Hub (set WG_HUB_IP or SNAPSHOT_URL on the Hub server to turn them on).")
	}
	if len(l.Plugins) == 0 {
		fmt.Fprintln(e.out, "This app has no plugin that can keep its data.")
		return nil
	}
	for _, p := range l.Plugins {
		state := "off (data starts empty on every node)"
		switch {
		case p.Snapshots && p.Healthy:
			state = fmt.Sprintf("on, every %d min", p.IntervalMinutes)
		case p.Snapshots:
			state = fmt.Sprintf("on, every %d min, BEHIND: no recent snapshot", p.IntervalMinutes)
		case p.Persist:
			state = "data kept on this node's disk only (not sent to the Hub)"
		}
		fmt.Fprintf(e.out, "%s  %s\n", p.PluginID, state)
		if len(p.Items) == 0 {
			fmt.Fprintln(e.out, "  no snapshots yet")
			continue
		}
		w := e.table()
		fmt.Fprintln(w, "  GEN\tTAKEN\tSIZE\tWHY")
		for _, s := range p.Items {
			fmt.Fprintf(w, "  %d\t%s\t%s\t%s\n", s.Generation, ago(s.CreatedAt), humanSize(s.SizeBytes), s.Trigger)
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}
	fmt.Fprintf(e.out, "The Hub keeps the newest %d per plugin, up to %d MB each.\n", l.Keep, l.MaxMB)
	return nil
}

// snapshotCommand handles `asdl-hub snapshot now|on|off|restore|delete ...`.
func (e *env) snapshotCommand(c *client, args []string) error {
	if len(args) == 0 {
		return usageError{"expected asdl-hub snapshot now|on|off|restore|delete <app> ... (see `asdl-hub help`)"}
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("snapshot "+sub, flag.ContinueOnError)
	minutes := fs.Int("minutes", 5, "minutes between snapshots")
	del := fs.Bool("delete", false, "also delete the stored snapshots")
	pos, err := parseWithPositional(fs, rest, 1, "an app name")
	if err != nil {
		return err
	}
	app, extra := pos[0], pos[1:]
	p, l, err := e.loadSnapshots(c, app)
	if err != nil {
		return err
	}
	base := "/projects/" + url.PathEscape(p.ID)

	switch sub {
	case "on":
		name := ""
		if len(extra) > 0 {
			name = extra[0]
		}
		pl, err := pickSnapPlugin(l, name)
		if err != nil {
			return err
		}
		if !l.Enabled {
			return errors.New("snapshots are switched off on this Hub (set WG_HUB_IP or SNAPSHOT_URL on the Hub server)")
		}
		if !pl.Persist {
			fmt.Fprintf(e.out, "Turning this on makes %s keep its data on disk, which restarts it with an empty data folder: what it holds in memory now is lost once.\n", pl.PluginID)
			if !e.confirm("Turn on snapshots for " + p.Name + "/" + pl.PluginID + "?") {
				return errors.New("cancelled")
			}
		}
		if err := e.setSnapshotVars(c, p.ID, pl.PluginID, "on", "on", strconv.Itoa(*minutes)); err != nil {
			return err
		}
		fmt.Fprintf(e.out, "Snapshots on for %s/%s, every %d min. The app redeploys; the first snapshot follows shortly.\n", p.Name, pl.PluginID, *minutes)
		return nil

	case "off":
		name := ""
		if len(extra) > 0 {
			name = extra[0]
		}
		pl, err := pickSnapPlugin(l, name)
		if err != nil {
			return err
		}
		if err := e.setSnapshotVars(c, p.ID, pl.PluginID, "", "off", ""); err != nil {
			return err
		}
		fmt.Fprintf(e.out, "Snapshots off for %s/%s. The data stays on the node's disk.\n", p.Name, pl.PluginID)
		if *del {
			if err := c.do("DELETE", base+"/snapshots?plugin_id="+url.QueryEscape(pl.PluginID), nil, nil); err != nil {
				return err
			}
			fmt.Fprintln(e.out, "Stored snapshots deleted.")
		} else if len(pl.Items) > 0 {
			fmt.Fprintf(e.out, "%d stored snapshot(s) were kept; delete them with `asdl-hub snapshot off %s --delete`.\n", len(pl.Items), app)
		}
		return nil

	case "now":
		name := ""
		if len(extra) > 0 {
			name = extra[0]
		}
		pl, err := pickSnapPlugin(l, name)
		if err != nil {
			return err
		}
		var res struct {
			JobID string `json:"job_id"`
		}
		if err := c.do("POST", base+"/snapshots", map[string]string{"plugin_id": pl.PluginID}, &res); err != nil {
			return err
		}
		fmt.Fprintf(e.out, "Snapshot of %s/%s queued", p.Name, pl.PluginID)
		if res.JobID != "" {
			fmt.Fprintf(e.out, " (job %s; follow it with `asdl-hub job %s`)", res.JobID[:8], res.JobID)
		}
		fmt.Fprintln(e.out, ".")
		return nil

	case "restore", "delete":
		if len(extra) == 0 {
			return usageError{"expected asdl-hub snapshot " + sub + " <app> <generation> [plugin]"}
		}
		gen, err := strconv.ParseInt(extra[0], 10, 64)
		if err != nil {
			return fmt.Errorf("%q is not a generation number (see `asdl-hub snapshots %s`)", extra[0], app)
		}
		name := ""
		if len(extra) > 1 {
			name = extra[1]
		}
		pl, err := pickSnapPlugin(l, name)
		if err != nil {
			return err
		}
		var item *snapItem
		for i := range pl.Items {
			if pl.Items[i].Generation == gen {
				item = &pl.Items[i]
			}
		}
		if item == nil {
			return fmt.Errorf("%s/%s has no snapshot with generation %d (see `asdl-hub snapshots %s`)", p.Name, pl.PluginID, gen, app)
		}
		if sub == "delete" {
			if !e.confirm(fmt.Sprintf("Delete snapshot %d of %s/%s?", gen, p.Name, pl.PluginID)) {
				return errors.New("cancelled")
			}
			if err := c.do("DELETE", base+"/snapshots/"+url.PathEscape(item.ID), nil, nil); err != nil {
				return err
			}
			fmt.Fprintln(e.out, "Deleted.")
			return nil
		}
		fmt.Fprintf(e.out, "Restore snapshot %d of %s/%s, taken %s (%s).\nThe data it holds now is replaced, and the app redeploys.\n", gen, p.Name, pl.PluginID, ago(item.CreatedAt), humanSize(item.SizeBytes))
		if !e.confirm("Restore it?") {
			return errors.New("cancelled")
		}
		var res struct {
			Generation  int64 `json:"generation"`
			Redeploying bool  `json:"redeploying"`
		}
		if err := c.do("POST", base+"/snapshots/"+url.PathEscape(item.ID)+"/restore", map[string]string{}, &res); err != nil {
			return err
		}
		fmt.Fprintf(e.out, "Restoring as generation %d; %s is redeploying.\n", res.Generation, p.Name)
		return nil
	}
	return usageError{"unknown snapshot command " + sub + " (now, on, off, restore, delete)"}
}

// setSnapshotVars changes a plugin's persist/snapshot settings; empty strings are left alone.
func (e *env) setSnapshotVars(c *client, projectID, plugin, persist, snapshot, minutes string) error {
	var vars []map[string]string
	add := func(k, v string) {
		if v != "" {
			vars = append(vars, map[string]string{"key": k, "value": v})
		}
	}
	add("persist", persist)
	add("snapshot", snapshot)
	add("snapshot_minutes", minutes)
	return c.do("PUT", "/projects/"+url.PathEscape(projectID)+"/plugins/"+url.PathEscape(plugin), map[string]interface{}{"vars": vars}, nil)
}
