package services

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/asdl/hub/internal/models"
)

// RegistryTokenEnv is the job environment variable that carries the registry
// token, so the token never appears in the command text or the job logs.
const RegistryTokenEnv = "ASDL_REGISTRY_TOKEN"

// envPrefix namespaces project env vars in the job environment, so a project
// variable such as PATH can't change how the deploy script itself runs.
const envPrefix = "ASDL_ENV_"

// PortMarker prefixes the log line in which an auto-port deploy reports the
// mapping it used, e.g. "ASDL_PORT_MAPPING=20001:8000".
const PortMarker = "ASDL_PORT_MAPPING="

var portMarkerRe = regexp.MustCompile(`(?m)^` + PortMarker + `(\d+:\d+)\s*$`)

// autoPortAttempts is how many consecutive node ports an auto-port deploy
// tries before giving up.
const autoPortAttempts = 50

// DeploySpec is everything the node needs to (re)start a project's container.
type DeploySpec struct {
	Project       *models.Project
	Image         string
	RegistryLogin bool
	// PreferredHostPort is where an auto-port deploy starts looking.
	PreferredHostPort int
}

// BuildDeployCommand returns the POSIX sh script a node runs to (re)start a
// project's container, and the job environment it needs. Secret values are
// only in the environment; the script refers to them by name.
func BuildDeployCommand(spec DeploySpec) (string, []string) {
	p := spec.Project
	name := shellQuote(p.Name)
	img := shellQuote(spec.Image)

	var env []string
	opts := []string{"-d", "--name", name, "--restart unless-stopped"}
	for _, v := range p.Volumes {
		opts = append(opts, "-v", shellQuote(v))
	}
	for _, e := range p.EnvVars {
		env = append(env, envPrefix+e.Key+"="+e.Value)
		// Double quotes expand the variable exactly once, so values with
		// spaces, quotes or $ reach the container unchanged.
		opts = append(opts, "-e", fmt.Sprintf(`"%s=$%s%s"`, e.Key, envPrefix, e.Key))
	}

	var b strings.Builder
	b.WriteString("set -e\n")
	if spec.RegistryLogin {
		fmt.Fprintf(&b, "printf '%%s' \"$%s\" | docker login ghcr.io -u x-access-token --password-stdin\n", RegistryTokenEnv)
	}
	fmt.Fprintf(&b, "docker pull %s\n", img)
	fmt.Fprintf(&b, "docker rm -f %s >/dev/null 2>&1 || true\n", name)

	if p.AutoPort || len(p.Ports) == 0 {
		writeAutoPortRun(&b, name, img, strings.Join(opts, " "), spec.PreferredHostPort)
	} else {
		for _, m := range p.Ports {
			opts = append(opts, "-p", shellQuote(m))
		}
		fmt.Fprintf(&b, "docker run %s %s\n", strings.Join(opts, " "), img)
	}

	// A container that crashes on boot still makes `docker run -d` succeed,
	// so give it a moment and check it is actually running.
	b.WriteString("sleep 5\n")
	fmt.Fprintf(&b, "if [ \"$(docker inspect -f '{{.State.Running}}' %s)\" != \"true\" ]; then\n", name)
	b.WriteString("  echo 'container exited after start:'\n")
	fmt.Fprintf(&b, "  docker logs --tail 50 %s\n", name)
	b.WriteString("  exit 1\n")
	b.WriteString("fi\n")
	fmt.Fprintf(&b, "echo 'container is running:' %s\n", name)
	return b.String(), env
}

// writeAutoPortRun emits a docker run that publishes the image's first
// exposed TCP port (8000 if none) on the first free node port from
// preferred on, then reports the mapping with PortMarker.
func writeAutoPortRun(b *strings.Builder, name, img, opts string, preferred int) {
	if preferred <= 0 {
		preferred = firstAutoPort
	}
	fmt.Fprintf(b, "CPORT=$(docker image inspect -f '{{range $p, $_ := .Config.ExposedPorts}}{{$p}} {{end}}' %s | tr ' ' '\\n' | grep '/tcp$' | head -n1 | cut -d/ -f1)\n", img)
	b.WriteString("CPORT=${CPORT:-8000}\n")
	fmt.Fprintf(b, "HPORT=%d\n", preferred)
	b.WriteString("TRIES=0\n")
	b.WriteString("while :; do\n")
	fmt.Fprintf(b, "  if OUT=$(docker run %s -p \"$HPORT:$CPORT\" %s 2>&1); then break; fi\n", opts, img)
	b.WriteString("  case \"$OUT\" in\n")
	b.WriteString("    *\"port is already allocated\"*|*\"address already in use\"*)\n")
	fmt.Fprintf(b, "      docker rm -f %s >/dev/null 2>&1 || true\n", name)
	b.WriteString("      TRIES=$((TRIES + 1))\n")
	fmt.Fprintf(b, "      if [ \"$TRIES\" -ge %d ]; then echo \"$OUT\"; echo 'no free port found'; exit 1; fi\n", autoPortAttempts)
	b.WriteString("      HPORT=$((HPORT + 1)) ;;\n")
	b.WriteString("    *) echo \"$OUT\"; exit 1 ;;\n")
	b.WriteString("  esac\n")
	b.WriteString("done\n")
	fmt.Fprintf(b, "echo \"%s$HPORT:$CPORT\"\n", PortMarker)
}

// ParsePortMarker returns the last port mapping an auto-port deploy reported
// in its logs.
func ParsePortMarker(logs string) (string, bool) {
	m := portMarkerRe.FindAllStringSubmatch(logs, -1)
	if len(m) == 0 {
		return "", false
	}
	return m[len(m)-1][1], true
}

// BuildStopCommand removes a project's container, succeeding if it is absent.
func BuildStopCommand(containerName string) string {
	return fmt.Sprintf("docker rm -f %s >/dev/null 2>&1 || true\n", shellQuote(containerName))
}

// shellQuote wraps s in single quotes for POSIX sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
