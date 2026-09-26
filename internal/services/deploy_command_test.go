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
// arguments (one per line). Host ports listed in busy fail like docker does
// when a port is taken; the image exposes 3000/tcp; the container is reported
// running unless crashed is set.
func runWithFakeDocker(t *testing.T, script string, env []string, busy []string, crashed bool) (calls, output string, err error) {
	t.Helper()
	dir := t.TempDir()
	logFile := filepath.Join(dir, "calls")
	state := "true"
	if crashed {
		state = "false"
	}
	busyCase := "__none__"
	if len(busy) > 0 {
		busyCase = strings.Join(busy, "|")
	}
	fake := `#!/bin/sh
{ echo "CALL"; for a in "$@"; do echo "$a"; done; } >> "` + logFile + `"
case "$1" in
  login) cat >> "` + logFile + `"; echo >> "` + logFile + `" ;;
  inspect) echo ` + state + ` ;;
  image) echo "3000/tcp " ;;
  run)
    prev=""
    for a in "$@"; do
      if [ "$prev" = "-p" ]; then
        case "${a%%:*}" in ` + busyCase + `) echo "Bind for 0.0.0.0:${a%%:*} failed: port is already allocated" >&2; exit 125 ;; esac
      fi
      prev="$a"
    done ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(fake), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sleep"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append([]string{"PATH=" + dir + ":" + os.Getenv("PATH")}, env...)
	out, err := cmd.CombinedOutput()
	logged, _ := os.ReadFile(logFile)
	return string(logged), string(out), err
}

func TestBuildDeployCommand_ExplicitPortsAndSecrets(t *testing.T) {
	project := &models.Project{
		ID:      "p-1",
		Name:    "api",
		Ports:   []string{"9000:8000"},
		Volumes: []string{"/srv/api data:/data"},
		EnvVars: models.SecretEnvVars{{Key: "GREETING", Value: `it's "quoted" $HOME`}, {Key: "PATH", Value: "/nope"}},
	}
	script, env := BuildDeployCommand(DeploySpec{Project: project, Image: "ghcr.io/o/api:abc", RegistryLogin: true})
	env = append(env, RegistryTokenEnv+"=s3cret")

	if strings.Contains(script, "s3cret") || strings.Contains(script, "quoted") {
		t.Fatal("secret values must not be embedded in the command")
	}
	calls, out, err := runWithFakeDocker(t, script, env, nil, false)
	if err != nil {
		t.Fatalf("script failed: %v\n%s\ncalls:\n%s", err, out, calls)
	}
	want := strings.Join([]string{
		"CALL", "run", "-d", "--name", "api", "--restart", "unless-stopped",
		"--label", "asdl.managed=true", "--label", "asdl.project=p-1",
		"-v", "/srv/api data:/data", "-e", `GREETING=it's "quoted" $HOME`, "-e", "PATH=/nope",
		"-p", "9000:8000", "ghcr.io/o/api:abc",
	}, "\n")
	if !strings.Contains(calls, want) {
		t.Errorf("docker run args not as expected.\nwant:\n%s\ngot:\n%s", want, calls)
	}
	if !strings.Contains(calls, "login\nghcr.io\n-u\nx-access-token\n--password-stdin\ns3cret") {
		t.Errorf("expected docker login with the token from the environment, got:\n%s", calls)
	}
}

func TestBuildDeployCommand_AutoPortSkipsBusyPorts(t *testing.T) {
	project := &models.Project{Name: "api", AutoPort: true}
	script, env := BuildDeployCommand(DeploySpec{Project: project, Image: "img", PreferredHostPort: 20000})

	calls, out, err := runWithFakeDocker(t, script, env, []string{"20000", "20001"}, false)
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	if !strings.Contains(calls, "-p\n20002:3000\n") {
		t.Errorf("expected the first free port with the image's exposed port, got:\n%s", calls)
	}
	if mapping, ok := ParsePortMarker(out); !ok || mapping != "20002:3000" {
		t.Errorf("port marker = %q, %v; output:\n%s", mapping, ok, out)
	}
}

func TestBuildDeployCommand_FailsWhenContainerExits(t *testing.T) {
	script, env := BuildDeployCommand(DeploySpec{Project: &models.Project{Name: "api"}, Image: "img"})
	if _, _, err := runWithFakeDocker(t, script, env, nil, true); err == nil {
		t.Fatal("expected the script to fail when the container is not running")
	}
}

func TestParsePortMarker_IgnoresCommandEcho(t *testing.T) {
	logs := "Command: set -e\necho \"ASDL_PORT_MAPPING=$HPORT:$CPORT\"\n...\nASDL_PORT_MAPPING=20001:8000\n"
	if m, ok := ParsePortMarker(logs); !ok || m != "20001:8000" {
		t.Errorf("got %q, %v", m, ok)
	}
}
