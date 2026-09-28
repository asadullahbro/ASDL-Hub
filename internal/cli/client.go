// Package cli is the asdl-hub command line: it runs the Hub's commands
// (status, apps, logs, deploys, maintenance...) against a Hub's API, either
// this machine's Hub or one you logged in to.
package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// On the Hub's own server the Hub writes an admin token here for the CLI,
// readable only by the Hub's user (so: sudo asdl-hub ...).
const (
	DefaultInstallDir = "/opt/asdl-hub"
	LocalTokenFile    = ".cli-token"
)

// savedLogin is ~/.config/asdl-hub/cli.json.
type savedLogin struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	User  string `json:"user,omitempty"`
}

func loginPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "asdl-hub", "cli.json")
}

type client struct {
	url   string
	token string
	// where tells the user which Hub and credentials are in use.
	where string
	http  *http.Client
}

var errNoHub = errors.New("no Hub to talk to: run `asdl-hub login <hub url>`, or on the Hub's server use `sudo asdl-hub ...`")

// newClient finds a Hub and credentials: $ASDL_HUB_URL and $ASDL_HUB_TOKEN,
// then a saved login, then this machine's Hub.
func newClient() (*client, error) {
	c := &client{http: &http.Client{Timeout: 30 * time.Second}}
	if u, t := os.Getenv("ASDL_HUB_URL"), os.Getenv("ASDL_HUB_TOKEN"); u != "" && t != "" {
		c.url, c.token, c.where = u, t, "$ASDL_HUB_URL"
		return c, nil
	}
	if b, err := os.ReadFile(loginPath()); err == nil {
		var l savedLogin
		if json.Unmarshal(b, &l) == nil && l.URL != "" && l.Token != "" {
			c.url, c.token, c.where = l.URL, l.Token, "login in "+loginPath()
			return c, nil
		}
	}
	dir := os.Getenv("ASDL_HUB_DIR")
	if dir == "" {
		dir = DefaultInstallDir
	}
	tok, err := os.ReadFile(filepath.Join(dir, LocalTokenFile))
	if err == nil && len(bytes.TrimSpace(tok)) > 0 {
		port := envFileValue(filepath.Join(dir, ".env"), "SERVER_PORT")
		if port == "" {
			port = "8080"
		}
		c.url, c.token, c.where = "http://127.0.0.1:"+port, string(bytes.TrimSpace(tok)), "this machine's Hub"
		return c, nil
	}
	if os.IsPermission(err) {
		return nil, fmt.Errorf("this is a Hub server, but reading its CLI token needs root: run `sudo asdl-hub %s`", strings.Join(os.Args[1:], " "))
	}
	return nil, errNoHub
}

// envFileValue reads KEY=value from a .env file ("" if it can't).
func envFileValue(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(s.Text()), "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

type apiError struct {
	Status int
	Msg    string
}

func (e *apiError) Error() string { return e.Msg }

// do calls the API; out (if not nil) receives the JSON answer.
func (c *client) do(method, path string, body, out interface{}) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, strings.TrimRight(c.url, "/")+"/api/v1"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "asdl-hub-cli")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("can't reach the Hub at %s: %v", c.url, unwrapURLError(err))
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(data))
		}
		if resp.StatusCode == http.StatusUnauthorized {
			e.Error = "the Hub didn't accept your login (" + e.Error + "); run `asdl-hub login` again"
		}
		return &apiError{resp.StatusCode, e.Error}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
