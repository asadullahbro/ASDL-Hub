package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// placementOption is one node an app could move to (GET /projects/:id/placement, best first).
type placementOption struct {
	NodeID       string   `json:"node_id"`
	Hostname     string   `json:"hostname"`
	Score        float64  `json:"score"`
	Uptime7d     *float64 `json:"uptime_7d"`
	OutagesToday int      `json:"outages_24h"`
	FreeMemory   *int     `json:"free_memory_percent"`
	FreeDisk     *int     `json:"free_disk_percent"`
	Headroom     bool     `json:"headroom"`
	Master       bool     `json:"master"`
}

func percentCell(p *int) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%d%%", *p)
}

// notes says in a few words why a node is, or is not, a good choice.
func (o placementOption) notes() string {
	var n []string
	if o.Master {
		n = append(n, "master")
	}
	if !o.Headroom {
		n = append(n, "low on memory or disk")
	}
	if o.OutagesToday > 0 {
		n = append(n, fmt.Sprintf("%d outage(s) today", o.OutagesToday))
	}
	return strings.Join(n, ", ")
}

// suggestNode shows where the app could go, best first, and returns the node to use once the user agrees to the
// best one. It returns an error (so nothing moves) when there is no node to go to or the user declines.
func (e *env) suggestNode(c *client, p *project) (string, error) {
	var res struct {
		Options []placementOption `json:"options"`
	}
	if err := c.do("GET", "/projects/"+url.PathEscape(p.ID)+"/placement", nil, &res); err != nil {
		return "", err
	}
	if len(res.Options) == 0 {
		return "", fmt.Errorf("no other node is online and available to take %s", p.Name)
	}
	w := e.table()
	fmt.Fprintln(w, "NODE\tSCORE\tUPTIME 7D\tFREE MEM\tFREE DISK\tNOTES")
	for i, o := range res.Options {
		name := o.Hostname
		if i == 0 {
			name += " *"
		}
		fmt.Fprintf(w, "%s\t%.0f\t%s\t%s\t%s\t%s\n", name, o.Score, uptimeCell(nodeUptime{Uptime7d: o.Uptime7d}),
			percentCell(o.FreeMemory), percentCell(o.FreeDisk), o.notes())
	}
	if err := w.Flush(); err != nil {
		return "", err
	}
	best := res.Options[0]
	fmt.Fprintln(e.out)
	if !e.confirm(fmt.Sprintf("Move %s to %s (best match, marked *)?", p.Name, best.Hostname)) {
		return "", errors.New("cancelled; name a node to choose one yourself: asdl-hub move " + p.Name + " <node>")
	}
	return best.NodeID, nil
}
