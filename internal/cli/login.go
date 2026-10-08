package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"
)

// login signs in to a Hub and saves the credentials. By default it opens the
// Hub's authorise page in the browser, where the user signs in (with
// two-factor if it's on) and approves this machine; --password asks for the
// username and password here instead. Admins get a permanent token named
// after this machine (revocable in Settings → Tokens); other users a session
// that lasts a day.
func (e *env) login(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	token := fs.String("token", "", "a permanent token from Settings → Tokens")
	user := fs.String("user", "", "username")
	usePassword := fs.Bool("password", false, "sign in with a username and password here instead of in the browser")
	noBrowser := fs.Bool("no-browser", false, "print the authorise page's address instead of opening it")
	pos, err := parseWithPositional(fs, args, 0, "")
	if err != nil {
		return err
	}
	in := bufio.NewReader(os.Stdin)
	hubURL := ""
	if len(pos) > 0 {
		hubURL = pos[0]
	} else if hubURL, err = prompt(in, "Hub URL (e.g. https://hub.example.com): "); err != nil {
		return err
	}
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	if !strings.Contains(hubURL, "://") {
		hubURL = "https://" + hubURL
	}
	c := &client{url: hubURL, http: &http.Client{Timeout: 30 * time.Second}}

	if *token != "" {
		c.token = *token
		return e.saveLogin(c, "")
	}

	if !*usePassword && *user == "" {
		return e.browserLogin(c, *noBrowser)
	}

	if *user == "" {
		if *user, err = prompt(in, "Username: "); err != nil {
			return err
		}
	}
	fmt.Fprint(os.Stderr, "Password: ")
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		// Not a terminal: read a line (e.g. piped in).
		line, rerr := in.ReadString('\n')
		if rerr != nil && line == "" {
			return errors.New("couldn't read the password")
		}
		pw = []byte(strings.TrimRight(line, "\r\n"))
	}

	var res struct {
		MFARequired bool   `json:"mfa_required"`
		MFAToken    string `json:"mfa_token"`
		Token       string `json:"token"`
		User        struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	if err := c.do("POST", "/auth/login", map[string]string{"username": *user, "password": string(pw)}, &res); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
			return errors.New("wrong username or password")
		}
		return err
	}

	// Two-factor is on for this user: the password was right, now the code.
	if res.MFARequired {
		code, err := prompt(in, "Two-factor code (or a recovery code): ")
		if err != nil {
			return err
		}
		res.MFARequired = false
		if err := c.do("POST", "/auth/2fa/login", map[string]string{"mfa_token": res.MFAToken, "code": code}, &res); err != nil {
			var ae *apiError
			if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
				return errors.New("wrong or expired code; run asdl-hub login again")
			}
			return err
		}
	}
	if res.Token == "" {
		return errors.New("the Hub signed you in without a token")
	}
	c.token = res.Token

	if res.User.Role == "admin" {
		host, _ := os.Hostname()
		var tok struct {
			Token string `json:"token"`
		}
		name := fmt.Sprintf("asdl-hub CLI (%s@%s)", res.User.Username, host)
		if err := c.do("POST", "/settings/tokens", map[string]string{"name": name, "password": string(pw)}, &tok); err == nil && tok.Token != "" {
			c.token = tok.Token
			return e.saveLogin(c, res.User.Username+", token \""+name+"\" (revoke it in Settings → Tokens)")
		}
	}
	return e.saveLogin(c, res.User.Username+", for 24 hours")
}

// cliPollEvery is how often the browser login asks the Hub whether the user
// has approved it.
var cliPollEvery = 2 * time.Second

// browserLogin has the user approve this machine on the Hub's authorise page.
func (e *env) browserLogin(c *client, noBrowser bool) error {
	host, _ := os.Hostname()
	var start struct {
		Code       string `json:"code"`
		PollSecret string `json:"poll_secret"`
		ExpiresIn  int    `json:"expires_in"`
		Path       string `json:"path"`
	}
	if err := c.do("POST", "/auth/cli/start", map[string]string{"machine": host}, &start); err != nil {
		return err
	}
	page := strings.TrimRight(c.url, "/") + start.Path
	fmt.Fprintf(os.Stderr, "Open this page and approve the login:\n\n  %s\n\nIts code should read %s. Waiting…\n", page, start.Code)
	if !noBrowser {
		openBrowser(page)
	}

	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(cliPollEvery)
		var res struct {
			Status string `json:"status"`
			Token  string `json:"token"`
			User   struct {
				Username string `json:"username"`
				Role     string `json:"role"`
			} `json:"user"`
		}
		err := c.do("POST", "/auth/cli/poll", map[string]string{"code": start.Code, "poll_secret": start.PollSecret}, &res)
		var ae *apiError
		switch {
		case errors.As(err, &ae) && ae.Status == http.StatusGone:
			return errors.New("the login was turned down or timed out; run asdl-hub login again")
		case err != nil:
			return err
		case res.Status != "approved" || res.Token == "":
			continue // still waiting
		}
		c.token = res.Token
		if res.User.Role == "admin" {
			name := fmt.Sprintf("asdl-hub CLI (%s@%s)", res.User.Username, host)
			return e.saveLogin(c, res.User.Username+", token \""+name+"\" (revoke it in Settings → Tokens)")
		}
		return e.saveLogin(c, res.User.Username+", for 24 hours")
	}
	return errors.New("nobody approved the login in time; run asdl-hub login again")
}

// openBrowser tries to open url in the user's browser. It says nothing if it
// can't (no desktop, an SSH session): the address is already printed.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return
		}
		cmd = exec.Command("xdg-open", url)
	}
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}

func prompt(in *bufio.Reader, q string) (string, error) {
	fmt.Fprint(os.Stderr, q)
	s, err := in.ReadString('\n')
	if err != nil && s == "" {
		return "", errors.New("no answer")
	}
	return strings.TrimSpace(s), nil
}

func (e *env) saveLogin(c *client, who string) error {
	var me struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := c.do("GET", "/auth/me", nil, &me); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(savedLogin{URL: c.url, Token: c.token, User: me.Username}, "", "  ")
	p := loginPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		return err
	}
	if who == "" {
		who = me.Username
	}
	fmt.Fprintf(e.out, "Logged in to %s as %s (%s).\nSaved in %s.\n", c.url, who, me.Role, p)
	return nil
}

// logout forgets the saved login and revokes its token if it was one this
// command created.
func (e *env) logout() error {
	p := loginPath()
	b, err := os.ReadFile(p)
	if err != nil {
		fmt.Fprintln(e.out, "Not logged in.")
		return nil
	}
	var l savedLogin
	_ = json.Unmarshal(b, &l)
	if l.Token != "" && l.URL != "" {
		c := &client{url: l.URL, token: l.Token, http: &http.Client{Timeout: 15 * time.Second}}
		var tokens []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			TokenHint string `json:"token_hint"`
		}
		if c.do("GET", "/settings/tokens", nil, &tokens) == nil && len(l.Token) > 8 {
			for _, t := range tokens {
				if strings.HasPrefix(t.Name, "asdl-hub CLI") && t.TokenHint == "..."+l.Token[len(l.Token)-8:] {
					if c.do("DELETE", "/settings/tokens/"+t.ID, nil, nil) == nil {
						fmt.Fprintf(e.out, "Revoked the token %q.\n", t.Name)
					}
				}
			}
		}
	}
	if err := os.Remove(p); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "Logged out of %s.\n", l.URL)
	return nil
}
