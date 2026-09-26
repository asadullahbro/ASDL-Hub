package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asdl/hub/internal/models"
)

// runWithFakeDocker runs script under sh with a fake docker that logs its
// arguments (one per line) and reports the container as running or not.
func runWithFakeDocker(t *testing.T, script string, running bool, env ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	logFile := filepath.Join(dir, "calls")
	state := "true"
	if !running {
		state = "false"
	}
	fake := `#!/bin/sh
{ echo "CALL"; for a in "$@"; do echo "$a"; done; } >> "` + logFile + `"
if [ "$1" = login ]; then cat >> "` + logFile + `"; echo; fi
if [ "$1" = inspect ]; then echo ` + state + `; fi
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(fake), 0755); err != nil {
		t.Fatal(err)
	}
	// Stub sleep so the test doesn't wait.
	if err := os.WriteFile(filepath.Join(dir, "sleep"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append([]string{"PATH=" + dir + ":" + os.Getenv("PATH")}, env...)
	_, err := cmd.CombinedOutput()
	calls, _ := os.ReadFile(logFile)
	return string(calls), err
}

func TestBuildDeployCommand_RunsContainerWithProjectConfig(t *testing.T) {
	project := &models.Project{
		Name:    "api",
		Ports:   []string{"9000:8000"},
		Volumes: []string{"/srv/api data:/data"},
		EnvVars: []models.EnvVar{{Key: "GREETING", Value: `it's "quoted" $HOME`}},
	}
	script := BuildDeployCommand(project, "ghcr.io/o/api:abc", true)

	if strings.Contains(script, "s3cret") {
		t.Fatal("token must not be embedded in the command")
	}
	calls, err := runWithFakeDocker(t, script, true, RegistryTokenEnv+"=s3cret")
	if err != nil {
		t.Fatalf("script failed: %v\ncalls:\n%s", err, calls)
	}

	want := strings.Join([]string{
		"CALL", "run", "-d", "--name", "api", "--restart", "unless-stopped",
		"-p", "9000:8000", "-v", "/srv/api data:/data", "-e", `GREETING=it's "quoted" $HOME`,
		"ghcr.io/o/api:abc",
	}, "\n")
	if !strings.Contains(calls, want) {
		t.Errorf("docker run args not as expected.\nwant:\n%s\ngot:\n%s", want, calls)
	}
	if !strings.Contains(calls, "login\nghcr.io\n-u\nx-access-token\n--password-stdin\ns3cret") {
		t.Errorf("expected docker login with the token from the environment, got:\n%s", calls)
	}
}

func TestBuildDeployCommand_DefaultPortWhenNoneConfigured(t *testing.T) {
	calls, err := runWithFakeDocker(t, BuildDeployCommand(&models.Project{Name: "api"}, "img", false), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(calls, "-p\n8000:8000\n") {
		t.Errorf("expected default port mapping, got:\n%s", calls)
	}
	if strings.Contains(calls, "login") {
		t.Error("no registry login expected")
	}
}

func TestBuildDeployCommand_FailsWhenContainerExits(t *testing.T) {
	_, err := runWithFakeDocker(t, BuildDeployCommand(&models.Project{Name: "api"}, "img", false), false)
	if err == nil {
		t.Fatal("expected the script to fail when the container is not running")
	}
}
