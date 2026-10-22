package cli

import (
	"errors"
	"fmt"
	"net/url"
	"time"
)

// rollback puts an app's previous image back, after showing which one and asking.
func (e *env) rollback(c *client, name string) error {
	p, err := c.findApp(name)
	if err != nil {
		return err
	}
	var info struct {
		CurrentImage string `json:"current_image"`
		Previous     *struct {
			Image      string    `json:"image"`
			DeployedAt time.Time `json:"deployed_at"`
			Commit     string    `json:"commit"`
			CommitMsg  string    `json:"commit_msg"`
		} `json:"previous"`
	}
	base := "/projects/" + url.PathEscape(p.ID) + "/rollback"
	if err := c.do("GET", base, nil, &info); err != nil {
		return err
	}
	if info.Previous == nil {
		return fmt.Errorf("%s has only ever run one version, so there is nothing to roll back to", p.Name)
	}
	fmt.Fprintf(e.out, "%s runs     %s\n", p.Name, info.CurrentImage)
	fmt.Fprintf(e.out, "Roll back to  %s (deployed %s)\n", info.Previous.Image, ago(info.Previous.DeployedAt))
	if m := info.Previous.CommitMsg; m != "" {
		fmt.Fprintf(e.out, "              %s\n", m)
	}
	if !e.confirm("Roll back " + p.Name + "?") {
		return errors.New("cancelled")
	}
	var res struct {
		JobID string `json:"job_id"`
	}
	if err := c.do("POST", base, map[string]string{}, &res); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "Rolling back %s…\n", p.Name)
	if res.JobID == "" {
		fmt.Fprintln(e.out, "Started; follow it with `asdl-hub jobs`.")
		return nil
	}
	return e.follow(c, res.JobID, 10*time.Minute, false)
}
