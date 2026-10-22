package cli

import (
	"fmt"
	"strings"
)

// statusPage shows whether the status page is public, or turns that on or off (admins only).
func (e *env) statusPage(c *client, setting string) error {
	var res struct {
		Public bool `json:"public"`
	}
	switch setting {
	case "":
		if err := c.do("GET", "/settings/status-page", nil, &res); err != nil {
			return err
		}
	case "on", "off":
		if err := c.do("PUT", "/settings/status-page", map[string]bool{"public": setting == "on"}, &res); err != nil {
			return err
		}
	default:
		return usageError{"expected asdl-hub status-page [on|off]"}
	}
	if e.json {
		return e.printJSON(res)
	}
	page := strings.TrimRight(c.url, "/") + "/status/"
	if res.Public {
		fmt.Fprintf(e.out, "The status page is public: %s\n", page)
		fmt.Fprintln(e.out, "Visitors see only apps that have a domain: their name, state and uptime. Never nodes or addresses.")
	} else {
		fmt.Fprintf(e.out, "The status page is private (logged-in users only): %s\n", page)
		fmt.Fprintln(e.out, "Turn it on for everyone with: asdl-hub status-page on")
	}
	return nil
}
