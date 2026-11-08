package cli

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func withStdin(t *testing.T, terminal bool, input string) {
	t.Helper()
	oldInteractive, oldStdin := interactive, stdin
	interactive = func() bool { return terminal }
	stdin = bufio.NewReader(strings.NewReader(input))
	t.Cleanup(func() { interactive, stdin = oldInteractive, oldStdin })
}

// `echo n | asdl-hub move ai-chat` once moved the app: without a terminal, confirm said yes without reading.
func TestConfirm_WithoutATerminal(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"n\n", false},
		{"no\n", false},
		{"\n", false}, // Enter means yes only to a person at a terminal
		{"", false},   // nothing piped (cron, /dev/null): refuse
		{"y\n", true},
		{"yes\n", true},
	}
	for _, c := range cases {
		withStdin(t, false, c.input)
		var out bytes.Buffer
		if got := (&env{out: &out}).confirm("Move it?"); got != c.want {
			t.Errorf("piped %q: confirm = %v, want %v", c.input, got, c.want)
		}
		if c.input == "" && !strings.Contains(out.String(), "Add --yes") {
			t.Errorf("with nothing piped it should point at --yes:\n%s", out.String())
		}
	}
}

func TestConfirm_AtATerminalEnterIsYes(t *testing.T) {
	withStdin(t, true, "\n")
	if !(&env{out: &bytes.Buffer{}}).confirm("Move it?") {
		t.Error("Enter at a terminal should still mean yes")
	}
	withStdin(t, true, "n\n")
	if (&env{out: &bytes.Buffer{}}).confirm("Move it?") {
		t.Error("n at a terminal should mean no")
	}
}

func TestConfirm_YesFlag(t *testing.T) {
	withStdin(t, false, "")
	var out bytes.Buffer
	if !(&env{out: &out, yes: true}).confirm("Move it?") || !strings.Contains(out.String(), "yes (--yes)") {
		t.Errorf("--yes should answer yes and say so: %q", out.String())
	}
}
