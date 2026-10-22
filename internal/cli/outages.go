package cli

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// outageRow is one entry of a node's outage history (GET /nodes/:id/details). Older Hubs leave the cause and
// link readings out.
type outageRow struct {
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at"`
	Seconds     int64      `json:"seconds"`
	Cause       string     `json:"cause"`
	WiFiAtStart int        `json:"wifi_at_start"`
	PingAtStart float64    `json:"ping_at_start"`
}

// causeLabel is the cause in words, "-" when the Hub could not tell (an outage from before causes were recorded).
func causeLabel(cause string) string {
	switch cause {
	case "reboot":
		return "restarted"
	case "weak_link":
		return "weak link"
	case "connection":
		return "connection"
	}
	return "-"
}

// outagePattern says when outages tend to start, if they cluster: the busiest three hours of the day (in loc)
// holding at least half of at least four outages. Empty when there is no pattern to report.
func outagePattern(outages []outageRow, loc *time.Location) string {
	if len(outages) < 4 {
		return ""
	}
	var perHour [24]int
	for _, o := range outages {
		perHour[o.StartedAt.In(loc).Hour()]++
	}
	best, bestStart := 0, 0
	for h := 0; h < 24; h++ {
		if perHour[h] == 0 { // a window starts on an hour that has an outage, so it is as tight as it can be
			continue
		}
		n := perHour[h] + perHour[(h+1)%24] + perHour[(h+2)%24]
		if n > best {
			best, bestStart = n, h
		}
	}
	if best*2 < len(outages) {
		return ""
	}
	return fmt.Sprintf("%d of %d outages began between %02d:00 and %02d:00", best, len(outages), bestStart, (bestStart+3)%24)
}

// causeSummary counts outages by cause, most common first: "2 restarted, 1 weak link".
func causeSummary(outages []outageRow) string {
	counts := map[string]int{}
	for _, o := range outages {
		if o.EndedAt != nil && o.Cause != "" {
			counts[causeLabel(o.Cause)]++
		}
	}
	labels := make([]string, 0, len(counts))
	for l := range counts {
		labels = append(labels, l)
	}
	sort.Slice(labels, func(i, j int) bool {
		if counts[labels[i]] != counts[labels[j]] {
			return counts[labels[i]] > counts[labels[j]]
		}
		return labels[i] < labels[j]
	})
	parts := make([]string, len(labels))
	for i, l := range labels {
		parts[i] = fmt.Sprintf("%d %s", counts[l], l)
	}
	return strings.Join(parts, ", ")
}

// outages shows a node's recent outages with their likely cause, and any pattern in when they happen.
func (e *env) outages(c *client, name string) error {
	n, err := c.findNode(name)
	if err != nil {
		return err
	}
	var d struct {
		Outages []outageRow `json:"outages"`
	}
	if err := c.do("GET", "/nodes/"+url.PathEscape(n.ID)+"/details", nil, &d); err != nil {
		return err
	}
	if e.json {
		return e.printJSON(d.Outages)
	}
	if len(d.Outages) == 0 {
		fmt.Fprintf(e.out, "No outages recorded for %s.\n", n.Hostname)
		return nil
	}
	w := e.table()
	fmt.Fprintln(w, "STARTED\tLASTED\tLIKELY CAUSE\tWIFI\tPING")
	for _, o := range d.Outages {
		lasted := "ongoing, " + duration(time.Since(o.StartedAt))
		if o.EndedAt != nil {
			lasted = duration(time.Duration(o.Seconds) * time.Second)
		}
		wifi, ping := "-", "-"
		if o.WiFiAtStart > 0 {
			wifi = fmt.Sprintf("%d%%", o.WiFiAtStart)
		}
		if o.PingAtStart > 0 {
			ping = fmt.Sprintf("%.0f ms", o.PingAtStart)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", o.StartedAt.Local().Format("Jan 2 15:04"), lasted, causeLabel(o.Cause), wifi, ping)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(e.out)
	if s := causeSummary(d.Outages); s != "" {
		fmt.Fprintf(e.out, "Causes: %s\n", s)
	}
	if p := outagePattern(d.Outages, time.Local); p != "" {
		fmt.Fprintf(e.out, "Pattern: %s\n", p)
	}
	fmt.Fprintln(e.out, "WIFI and PING are the node's last readings before it went quiet. \"restarted\" means the machine's uptime")
	fmt.Fprintln(e.out, "was shorter than the outage; \"weak link\" and \"connection\" mean it stayed up.")
	return nil
}
