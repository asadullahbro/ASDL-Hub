package services

import (
	"fmt"
	"strings"

	"github.com/asdl/hub/internal/models"
)

// RegistryTokenEnv is the job environment variable that carries the registry
// token, so the token never appears in the command text or the job logs.
const RegistryTokenEnv = "ASDL_REGISTRY_TOKEN"

// BuildDeployCommand returns the POSIX sh script a node runs to (re)start a
// project's container from image. The container gets the project's ports,
// env vars and volumes, and the script fails if the container does not stay up.
func BuildDeployCommand(project *models.Project, image string, registryLogin bool) string {
	name := shellQuote(project.Name)
	img := shellQuote(image)

	runArgs := []string{"docker run -d --name", name, "--restart unless-stopped"}
	for _, p := range project.PortMappings() {
		runArgs = append(runArgs, "-p", shellQuote(p))
	}
	for _, v := range project.Volumes {
		runArgs = append(runArgs, "-v", shellQuote(v))
	}
	for _, e := range project.EnvVars {
		runArgs = append(runArgs, "-e", shellQuote(e.Key+"="+e.Value))
	}
	runArgs = append(runArgs, img)

	var b strings.Builder
	b.WriteString("set -e\n")
	if registryLogin {
		fmt.Fprintf(&b, "printf '%%s' \"$%s\" | docker login ghcr.io -u x-access-token --password-stdin\n", RegistryTokenEnv)
	}
	fmt.Fprintf(&b, "docker pull %s\n", img)
	fmt.Fprintf(&b, "docker rm -f %s >/dev/null 2>&1 || true\n", name)
	b.WriteString(strings.Join(runArgs, " ") + "\n")
	// A container that crashes on boot still makes `docker run -d` succeed,
	// so give it a moment and check it is actually running.
	b.WriteString("sleep 5\n")
	fmt.Fprintf(&b, "if [ \"$(docker inspect -f '{{.State.Running}}' %s)\" != \"true\" ]; then\n", name)
	b.WriteString("  echo 'container exited after start:'\n")
	fmt.Fprintf(&b, "  docker logs --tail 50 %s\n", name)
	b.WriteString("  exit 1\n")
	b.WriteString("fi\n")
	fmt.Fprintf(&b, "echo 'container is running:' %s\n", name)
	return b.String()
}

// BuildStopCommand removes a project's container, succeeding if it is absent.
func BuildStopCommand(containerName string) string {
	return fmt.Sprintf("docker rm -f %s >/dev/null 2>&1 || true\n", shellQuote(containerName))
}

// shellQuote wraps s in single quotes for POSIX sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
