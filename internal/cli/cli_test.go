package cli

import "testing"

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
