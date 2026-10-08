package services

import (
	"strings"
	"testing"
	"time"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

func newCLIAuthFixture(t *testing.T) (*CLIAuth, *time.Time, *models.User, *models.User) {
	t.Helper()
	db := testutil.NewDB(t, &models.User{}, &models.PermanentToken{})
	auth := NewAuthService(db, "test-secret")
	cli := NewCLIAuth(auth, NewSettingsService(db, auth, "test-secret"))
	now := time.Now()
	cli.now = func() time.Time { return now }
	admin := &models.User{ID: "a1", Username: "root", Email: "r@x", Password: "x", Role: models.RoleAdmin}
	viewer := &models.User{ID: "v1", Username: "vic", Email: "v@x", Password: "x", Role: models.RoleViewer}
	db.Create(admin)
	db.Create(viewer)
	return cli, &now, admin, viewer
}

func TestCLIAuth_ApprovalHandsOverTheTokenOnce(t *testing.T) {
	cli, _, admin, _ := newCLIAuthFixture(t)
	code, secret, err := cli.Start("laptop", "198.51.100.7")
	if err != nil {
		t.Fatal(err)
	}
	if st, tok, _ := cli.Poll(code, secret); st != "pending" || tok != "" {
		t.Fatalf("before approval: %q %q", st, tok)
	}
	// The wrong secret gets nothing, even for a real code.
	if st, _, _ := cli.Poll(code, "not-the-secret"); st != "unknown" {
		t.Errorf("wrong secret: %q", st)
	}
	if err := cli.Approve(strings.ToLower(FormatCLICode(code)), admin); err != nil {
		t.Fatalf("approve (typed in lower case with a dash): %v", err)
	}
	st, tok, user := cli.Poll(code, secret)
	if st != "approved" || tok == "" || user.ID != "a1" {
		t.Fatalf("after approval: %q %q %v", st, tok, user)
	}
	// Once only.
	if st, tok, _ := cli.Poll(code, secret); st != "unknown" || tok != "" {
		t.Errorf("token handed out twice: %q %q", st, tok)
	}
}

func TestCLIAuth_AdminsGetARevocableTokenOthersASession(t *testing.T) {
	cli, _, admin, viewer := newCLIAuthFixture(t)

	code, secret, _ := cli.Start("build-box", "198.51.100.7")
	cli.Approve(code, admin)
	_, adminTok, _ := cli.Poll(code, secret)
	if _, err := cli.auth.ValidateToken(adminTok); err != nil {
		t.Errorf("the admin's token doesn't work: %v", err)
	}
	var tokens []models.PermanentToken
	cli.settings.db.Find(&tokens)
	if len(tokens) != 1 || tokens[0].Name != "asdl-hub CLI (root@build-box)" {
		t.Errorf("expected one token named after the user and machine, got %+v", tokens)
	}
	// Revoking it (what asdl-hub logout does) ends it.
	cli.settings.RevokePermanentToken(tokens[0].ID)
	if _, err := cli.auth.ValidateToken(adminTok); err == nil {
		t.Error("a revoked token still works")
	}

	code, secret, _ = cli.Start("laptop", "198.51.100.8")
	cli.Approve(code, viewer)
	_, viewerTok, _ := cli.Poll(code, secret)
	if _, err := cli.auth.ValidateToken(viewerTok); err != nil {
		t.Errorf("the viewer's session doesn't work: %v", err)
	}
	cli.settings.db.Find(&tokens)
	if len(tokens) != 0 {
		t.Errorf("a non-admin got a permanent token: %+v", tokens)
	}
}

func TestCLIAuth_ExpiresAndCanBeDenied(t *testing.T) {
	cli, now, admin, _ := newCLIAuthFixture(t)

	code, secret, _ := cli.Start("laptop", "198.51.100.7")
	*now = now.Add(cliRequestLife + time.Second)
	if err := cli.Approve(code, admin); err == nil {
		t.Error("an expired request was approved")
	}
	if st, _, _ := cli.Poll(code, secret); st != "expired" {
		t.Errorf("expired request polls as %q", st)
	}

	code, secret, _ = cli.Start("laptop", "198.51.100.7")
	if err := cli.Deny(code); err != nil {
		t.Fatal(err)
	}
	if err := cli.Approve(code, admin); err == nil {
		t.Error("a denied request was approved afterwards")
	}
	if st, tok, _ := cli.Poll(code, secret); st != "denied" || tok != "" {
		t.Errorf("denied request polls as %q", st)
	}
}

func TestCLIAuth_LimitsRequestsPerAddressAndLooksUpInfo(t *testing.T) {
	cli, now, _, _ := newCLIAuthFixture(t)
	var code string
	for i := 0; i < cliStartsPerMin; i++ {
		c, _, err := cli.Start("m", "203.0.113.5")
		if err != nil {
			t.Fatalf("request %d refused: %v", i, err)
		}
		code = c
	}
	if _, _, err := cli.Start("m", "203.0.113.5"); err == nil {
		t.Error("an address can start unlimited requests")
	}
	if _, _, err := cli.Start("m", "203.0.113.6"); err != nil {
		t.Errorf("another address was affected: %v", err)
	}
	info, ok := cli.Info(code)
	if !ok || info.Machine != "m" || info.IP != "203.0.113.5" || info.Status != "pending" {
		t.Errorf("info: %+v %v", info, ok)
	}
	*now = now.Add(time.Minute + time.Second)
	if _, _, err := cli.Start("m", "203.0.113.5"); err != nil {
		t.Errorf("still limited a minute later: %v", err)
	}
	if _, ok := cli.Info("ZZZZ-ZZZZ"); ok {
		t.Error("info for a code that doesn't exist")
	}
}
