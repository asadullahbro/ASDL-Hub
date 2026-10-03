package cli

import (
	"strings"
	"testing"
)

func TestJobOutput(t *testing.T) {
	logs := "Job abc\n====================================\n\nline 1\nline 2\n====================================\nCompleted at: x\nExit Code: 0\nDuration: 1s\nSTDOUT:\nline 1\nline 2\nSTDERR:\nwarn\n"
	if got := JobOutput(logs); got != "line 1\nline 2\nwarn" {
		t.Errorf("got %q", got)
	}
	if got := JobOutput("Job abc\n====================================\n\nstreaming\n"); got != "streaming" {
		t.Errorf("running job: got %q", got)
	}
}

func TestIsCommand(t *testing.T) {
	if IsCommand([]string{"serve"}) {
		t.Error("serve should run the server")
	}
	if !IsCommand([]string{"status"}) {
		t.Error("status is a command")
	}
}

func TestApplyEnvChanges(t *testing.T) {
	lines := []string{"# Hub settings", "PUBLIC_URL=https://old.example.com", "", "LOG_LEVEL=debug", "SERVER_PORT=8080"}
	v1, v2 := "https://hub.example.com", "a b"
	got := applyEnvChanges(lines, map[string]*string{"PUBLIC_URL": &v1, "LOG_LEVEL": nil, "NEW_ONE": &v2})
	want := []string{"# Hub settings", "PUBLIC_URL=https://hub.example.com", "", "SERVER_PORT=8080", `NEW_ONE="a b"`}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestIsSecretKey(t *testing.T) {
	for k, want := range map[string]bool{"DB_PASSWORD": true, "JWT_SECRET": true, "SECRETS_KEY": true, "WG_HUB_PUBKEY": false, "PUBLIC_URL": false} {
		if isSecretKey(k) != want {
			t.Errorf("%s: %v", k, !want)
		}
	}
}
