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
	// Plugins run next to the project in its private network.
	Plugins []ResolvedPlugin
}

// PluginDir is where a node keeps the files plugins mount, per project.
var PluginDir = "/var/lib/asdl/plugins"

// projectNetwork is the private Docker network a project shares with its
// plugins on a node.
func projectNetwork(p *models.Project) string { return "asdl-" + p.Name }

// BuildDeployCommand returns the POSIX sh script a node runs to (re)start a
// project's container, and the job environment it needs. Secret values are
// only in the environment; the script refers to them by name.
func BuildDeployCommand(spec DeploySpec) (string, []string) {
	p := spec.Project
	name := shellQuote(p.Name)
	img := shellQuote(spec.Image)

	var env []string
	// Labels let the node's agent tell the Hub's containers from others.
	opts := []string{"-d", "--name", name, "--restart unless-stopped",
		"--label", "asdl.managed=true", "--label", "asdl.project=" + shellQuote(p.ID)}
	for _, v := range p.Volumes {
		opts = append(opts, "-v", shellQuote(v))
	}
	// Variables provided by plugins win over the project's own.
	provided := map[string]bool{}
	if len(spec.Plugins) > 0 {
		opts = append(opts, "--network", shellQuote(projectNetwork(p)))
		for _, pl := range spec.Plugins {
			for _, kv := range pl.Provides {
				if provided[kv[0]] {
					continue
				}
				provided[kv[0]] = true
				env = append(env, pluginProvidePrefix+kv[0]+"="+kv[1])
				opts = append(opts, "-e", fmt.Sprintf(`"%s=$%s%s"`, kv[0], pluginProvidePrefix, kv[0]))
			}
		}
	}
	for _, e := range p.EnvVars {
		if provided[e.Key] {
			continue
		}
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
	env = append(env, writePlugins(&b, p, spec.Plugins)...)
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

const pluginProvidePrefix = "ASDL_PLUGIN_PROVIDES_"

// writePlugins emits the part of a deploy that runs the project's plugins:
// its private network, removal of plugins no longer attached, and each
// plugin's container (left alone if nothing about it changed). Every value
// from a plugin's settings goes through the job environment, never the
// script. It returns those environment entries.
func writePlugins(b *strings.Builder, p *models.Project, plugins []ResolvedPlugin) []string {
	var env []string
	keep := make([]string, 0, len(plugins))
	for _, pl := range plugins {
		keep = append(keep, pl.ContainerName)
	}
	b.WriteString("# Plugins attached to this project run next to it.\n")
	fmt.Fprintf(b, "for c in $(docker ps -aq --filter label=asdl.companion-of=%s); do\n", p.ID)
	b.WriteString("  n=$(docker inspect -f '{{.Name}}' \"$c\" | sed 's#^/##')\n")
	fmt.Fprintf(b, "  case \" %s \" in *\" $n \"*) ;; *) echo \"removing plugin $n\"; docker rm -f \"$c\" >/dev/null ;; esac\n", strings.Join(keep, " "))
	b.WriteString("done\n")
	if len(plugins) == 0 {
		return nil
	}
	net := shellQuote(projectNetwork(p))
	fmt.Fprintf(b, "docker network inspect %s >/dev/null 2>&1 || docker network create --label asdl.project=%s %s >/dev/null\n", net, p.ID, net)
	for i, pl := range plugins {
		cn := shellQuote(pl.ContainerName)
		fmt.Fprintf(b, "if [ \"$(docker inspect -f '{{index .Config.Labels \"asdl.plugin-hash\"}}' %s 2>/dev/null)\" = %s ] && [ \"$(docker inspect -f '{{.State.Running}}' %s 2>/dev/null)\" = true ]; then\n", cn, shellQuote(pl.Hash), cn)
		fmt.Fprintf(b, "  echo 'plugin %s unchanged'\n", pl.PluginID)
		b.WriteString("else\n")
		fmt.Fprintf(b, "  docker pull %s\n", shellQuote(pl.Image))
		fmt.Fprintf(b, "  docker rm -f %s >/dev/null 2>&1 || true\n", cn)
		opts := []string{"-d", "--name", cn, "--restart unless-stopped", "--network", net, "--network-alias", shellQuote(pl.Alias),
			"--label", "asdl.managed=true", "--label", "asdl.project=" + p.ID, "--label", "asdl.companion-of=" + p.ID,
			"--label", "asdl.plugin=" + shellQuote(pl.PluginID), "--label", "asdl.plugin-hash=" + shellQuote(pl.Hash)}
		if len(pl.Files) > 0 {
			dir := fmt.Sprintf("%s/%s/%s", PluginDir, p.ID, pl.PluginID)
			fmt.Fprintf(b, "  mkdir -p %s\n", shellQuote(dir))
			for j, f := range pl.Files {
				v := fmt.Sprintf("ASDL_PLUGIN_%d_F%d", i, j)
				env = append(env, v+"="+f.Content)
				file := fmt.Sprintf("%s/file%d", dir, j)
				fmt.Fprintf(b, "  printf '%%s' \"$%s\" > %s && chmod 644 %s\n", v, shellQuote(file), shellQuote(file))
				opts = append(opts, "-v", shellQuote(file+":"+f.Path+":ro"))
			}
		}
		for j, kv := range pl.Env {
			v := fmt.Sprintf("ASDL_PLUGIN_%d_E%d", i, j)
			env = append(env, v+"="+kv[1])
			opts = append(opts, "-e", fmt.Sprintf(`"%s=$%s"`, kv[0], v))
		}
		args := make([]string, 0, len(pl.Command))
		for j, a := range pl.Command {
			v := fmt.Sprintf("ASDL_PLUGIN_%d_C%d", i, j)
			env = append(env, v+"="+a)
			args = append(args, fmt.Sprintf(`"$%s"`, v))
		}
		fmt.Fprintf(b, "  docker run %s %s %s\n", strings.Join(opts, " "), shellQuote(pl.Image), strings.Join(args, " "))
		b.WriteString("  sleep 3\n")
		fmt.Fprintf(b, "  if [ \"$(docker inspect -f '{{.State.Running}}' %s)\" != true ]; then echo 'plugin %s exited after start:'; docker logs --tail 30 %s; exit 1; fi\n", cn, pl.PluginID, cn)
		fmt.Fprintf(b, "  echo 'plugin %s is running'\n", pl.PluginID)
		b.WriteString("fi\n")
	}
	return env
}

// BuildRemoveProjectCommand removes a project's container, its plugins, its
// private network and its plugin files from a node; it succeeds if any of
// them are already gone. Used on the old node after a move, and on delete.
func BuildRemoveProjectCommand(p *models.Project) string {
	var b strings.Builder
	b.WriteString(BuildStopCommand(p.Name))
	fmt.Fprintf(&b, "for c in $(docker ps -aq --filter label=asdl.companion-of=%s); do docker rm -f \"$c\" >/dev/null 2>&1 || true; done\n", p.ID)
	fmt.Fprintf(&b, "docker network rm %s >/dev/null 2>&1 || true\n", shellQuote(projectNetwork(p)))
	fmt.Fprintf(&b, "rm -rf %s\n", shellQuote(PluginDir+"/"+p.ID))
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
