package services

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/asdl/hub/internal/models"
)

// Signing the command line in through the browser. The CLI asks for a login
// request and shows its code; the user opens the dashboard's authorise page
// (signing in there, with two-factor if it's on) and approves the code; the
// CLI, which has been polling with a secret only it knows, receives the token.
// The password and the authenticator code never reach the terminal.
//
// Requests live in memory for a few minutes, so a Hub restart cancels any in
// flight; the CLI just asks again.

const (
	cliRequestLife    = 5 * time.Minute
	cliStartsPerMin   = 10
	cliMaxPending     = 500
	cliCodeAlphabet   = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I
	cliCodeLength     = 8
	cliMachineNameMax = 64
)

type cliRequest struct {
	machine    string
	ip         string
	created    time.Time
	secretHash [32]byte
	status     string // pending, approved, denied
	token      string
	user       *models.User
}

type CLIAuth struct {
	mu       sync.Mutex
	now      func() time.Time
	requests map[string]*cliRequest
	starts   map[string][]time.Time
	auth     *AuthService
	settings *SettingsService
}

func NewCLIAuth(auth *AuthService, settings *SettingsService) *CLIAuth {
	return &CLIAuth{
		now:      time.Now,
		requests: map[string]*cliRequest{},
		starts:   map[string][]time.Time{},
		auth:     auth,
		settings: settings,
	}
}

// NormalizeCLICode accepts the code however it was typed: any case, with or
// without the dash.
func NormalizeCLICode(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
}

// FormatCLICode is how a code is shown: ABCD-EFGH.
func FormatCLICode(code string) string {
	if len(code) == cliCodeLength {
		return code[:4] + "-" + code[4:]
	}
	return code
}

func (c *CLIAuth) prune() {
	cutoff := c.now().Add(-cliRequestLife)
	for k, r := range c.requests {
		if r.created.Before(cutoff) {
			delete(c.requests, k)
		}
	}
}

func hashSecret(secret string) [32]byte { return sha256.Sum256([]byte(secret)) }

// Start makes a login request for a CLI on a machine, called from ip. It
// returns the code to show and the secret the CLI polls with.
func (c *CLIAuth) Start(machine, ip string) (code, secret string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.prune()

	recent := c.starts[ip][:0]
	for _, t := range c.starts[ip] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= cliStartsPerMin {
		c.starts[ip] = recent
		return "", "", errors.New("too many login requests; wait a minute")
	}
	if len(c.requests) >= cliMaxPending {
		return "", "", errors.New("too many login requests in flight; try again shortly")
	}
	c.starts[ip] = append(recent, now)

	raw := make([]byte, cliCodeLength)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	for i := range raw {
		raw[i] = cliCodeAlphabet[int(raw[i])%len(cliCodeAlphabet)]
	}
	code = string(raw)
	if _, taken := c.requests[code]; taken {
		return "", "", errors.New("try again")
	}
	sec := make([]byte, 24)
	if _, err := rand.Read(sec); err != nil {
		return "", "", err
	}
	secret = hex.EncodeToString(sec)

	machine = strings.TrimSpace(machine)
	if machine == "" {
		machine = "unknown machine"
	}
	if len(machine) > cliMachineNameMax {
		machine = machine[:cliMachineNameMax]
	}
	c.requests[code] = &cliRequest{machine: machine, ip: ip, created: now, secretHash: hashSecret(secret), status: "pending"}
	return code, secret, nil
}

// CLIRequestInfo is what the authorise page shows about a request.
type CLIRequestInfo struct {
	Machine   string    `json:"machine"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Status    string    `json:"status"`
}

func (c *CLIAuth) get(code string) (*cliRequest, bool) {
	r, ok := c.requests[NormalizeCLICode(code)]
	if !ok || c.now().Sub(r.created) > cliRequestLife {
		return nil, false
	}
	return r, true
}

// Info describes a pending request for the authorise page.
func (c *CLIAuth) Info(code string) (CLIRequestInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.get(code)
	if !ok {
		return CLIRequestInfo{}, false
	}
	return CLIRequestInfo{Machine: r.machine, IP: r.ip, CreatedAt: r.created, ExpiresAt: r.created.Add(cliRequestLife), Status: r.status}, true
}

// Approve signs the CLI in as user. Admins get a permanent token named after
// the machine (revocable in Settings → Tokens, and by asdl-hub logout); others
// a day-long session, as the password login gave them.
func (c *CLIAuth) Approve(code string, user *models.User) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.get(code)
	if !ok {
		return errors.New("that login request has expired; run asdl-hub login again")
	}
	if r.status != "pending" {
		return errors.New("that login request was already answered")
	}
	var token string
	var err error
	if user.Role == models.RoleAdmin {
		name := fmt.Sprintf("asdl-hub CLI (%s@%s)", user.Username, r.machine)
		token, _, err = c.settings.GeneratePermanentToken(name, user.ID)
	} else {
		token, err = c.auth.sessionToken(user)
	}
	if err != nil {
		return err
	}
	r.status, r.token, r.user = "approved", token, user
	return nil
}

// Deny turns the request down.
func (c *CLIAuth) Deny(code string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.get(code)
	if !ok || r.status != "pending" {
		return errors.New("that login request is gone")
	}
	r.status = "denied"
	return nil
}

// Poll is what the CLI asks. The token is handed over once and forgotten.
// status is pending, approved, denied or expired; unknown means the code and
// secret don't match any request.
func (c *CLIAuth) Poll(code, secret string) (status, token string, user *models.User) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := NormalizeCLICode(code)
	r, ok := c.requests[key]
	want := hashSecret(secret)
	if !ok || subtle.ConstantTimeCompare(r.secretHash[:], want[:]) != 1 {
		return "unknown", "", nil
	}
	if c.now().Sub(r.created) > cliRequestLife {
		delete(c.requests, key)
		return "expired", "", nil
	}
	switch r.status {
	case "approved":
		delete(c.requests, key)
		return "approved", r.token, r.user
	case "denied":
		delete(c.requests, key)
		return "denied", "", nil
	}
	return "pending", "", nil
}
