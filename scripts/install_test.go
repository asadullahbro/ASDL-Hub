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
