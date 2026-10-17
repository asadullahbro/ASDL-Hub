package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// run executes install.sh with a fake `uname` (so macOS can be tested from
// Linux), no terminal, and returns what it printed and whether it succeeded.
func run(t *testing.T, uname string, env ...string) (string, bool) {
	t.Helper()
	bin := t.TempDir()
	fake := "#!/bin/sh\ncase \"$1\" in -s) echo " + uname + ";; -m) echo x86_64;; *) /usr/bin/uname \"$@\";; esac\n"
	if err := os.WriteFile(filepath.Join(bin, "uname"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "install.sh")
	cmd.Env = append([]string{"PATH=" + bin + ":" + os.Getenv("PATH"), "HOME=" + t.TempDir()}, env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // no controlling terminal: /dev/tty can't be opened
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

func TestInstall_TheHubIsLinuxOnly(t *testing.T) {
	out, ok := run(t, "Darwin", "ASDL_INSTALL=hub")
	if ok || !strings.Contains(out, "runs on Linux only") || !strings.Contains(out, "macOS") {
		t.Errorf("asking for the Hub on macOS: ok=%v\n%s", ok, out)
	}
}

func TestInstall_MacShowsTheHubAsUnavailableAndTakesTheCommandLine(t *testing.T) {
	// An unreachable release makes the download fail fast after the choice has been made.
	out, _ := run(t, "Darwin", "ASDL_CLI_VERSION=v0.0.0-none")
	for _, want := range []string{"Linux only, so not available on macOS", "Command line", "Installing the command line"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestInstall_RejectsAnUnknownChoice(t *testing.T) {
	out, ok := run(t, "Linux", "ASDL_INSTALL=both")
	if ok || !strings.Contains(out, "use hub or cli") {
		t.Errorf("ok=%v\n%s", ok, out)
	}
}

func TestInstall_WithoutATerminalItStillMeansTheHub(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the Hub path continues past the root check when run as root")
	}
	out, ok := run(t, "Linux")
	if ok || !strings.Contains(out, "Run with sudo.") {
		t.Errorf("automation on Linux should go down the Hub path (which needs root): ok=%v\n%s", ok, out)
	}
	if strings.Contains(out, "What would you like to install") {
		t.Errorf("asked a question with nobody to answer:\n%s", out)
	}
}

// detectDBMode runs install.sh's detect_db_mode against an install folder with the given .env
// (nil means a fresh machine), and returns the mode it chose, or the failure message.
func detectDBMode(t *testing.T, envFile *string, asked string) (mode string, ok bool) {
	t.Helper()
	dir := t.TempDir()
	if envFile != nil {
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(*envFile), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := `
info(){ :; }
die(){ echo "DIE: $*"; exit 1; }
eval "$(sed -n '/^detect_db_mode() {/,/^}/p' install.sh)"
INSTALL_DIR="` + dir + `"; DB_MODE=""
detect_db_mode
echo "MODE=$DB_MODE"
`
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(), "ASDL_DB="+asked)
	out, err := cmd.CombinedOutput()
	s := string(out)
	if err != nil {
		return s, false
	}
	return strings.TrimSpace(strings.TrimPrefix(s, "MODE=")), true
}

func TestInstall_DatabaseMode(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name   string
		env    *string
		asked  string
		want   string // the mode, or text the failure must contain
		wantOK bool
	}{
		{"a new Hub runs its own database", nil, "", "embedded", true},
		{"a new Hub can still ask for the system's PostgreSQL", nil, "external", "external", true},
		{"an installed Hub with the system's PostgreSQL keeps it", str("DB_HOST=127.0.0.1\nDB_PORT=5432\n"), "", "external", true},
		{"an installed Hub with its own database keeps it", str("DB_EMBEDDED=true\nDB_PORT=54329\n"), "", "embedded", true},
		{"an installed external Hub is never switched to an empty embedded database", str("DB_HOST=127.0.0.1\n"), "embedded", "empty database", false},
		{"an unknown choice is refused", nil, "sqlite", "must be 'embedded' or 'external'", false},
	}
	for _, c := range cases {
		got, ok := detectDBMode(t, c.env, c.asked)
		if ok != c.wantOK || (c.wantOK && got != c.want) || (!c.wantOK && !strings.Contains(got, c.want)) {
			t.Errorf("%s: got %q (ok=%v), want %q (ok=%v)", c.name, got, ok, c.want, c.wantOK)
		}
	}
}
