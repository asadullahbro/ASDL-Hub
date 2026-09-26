package models

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	portMappingRe = regexp.MustCompile(`^(?:(\d{1,3}(?:\.\d{1,3}){3}):)?(\d{1,5}):(\d{1,5})(?:/(?:tcp|udp))?$`)
	envKeyRe      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// Docker's own rule for container names, which project names become.
	projectNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	domainRe      = regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+)$`)
)

// ParsePortMapping splits a docker-style "[ip:]host:container[/proto]" mapping
// and returns the host and container ports. Both must be in 1-65535.
func ParsePortMapping(mapping string) (host, container string, err error) {
	m := portMappingRe.FindStringSubmatch(strings.TrimSpace(mapping))
	if m == nil {
		return "", "", fmt.Errorf("invalid port mapping %q: expected host:container, e.g. 8080:8000", mapping)
	}
	hostN, _ := strconv.Atoi(m[2])
	containerN, _ := strconv.Atoi(m[3])
	if hostN < 1 || hostN > 65535 || containerN < 1 || containerN > 65535 {
		return "", "", fmt.Errorf("invalid port mapping %q: ports must be between 1 and 65535", mapping)
	}
	// Re-format so "08080" and "8080" are the same port everywhere.
	return strconv.Itoa(hostN), strconv.Itoa(containerN), nil
}

// ValidateProjectConfig checks the user-supplied parts of a project that end
// up in a docker run command or an nginx config.
func ValidateProjectConfig(name, domain string, ports []string, envVars []EnvVar) error {
	if name != "" && !projectNameRe.MatchString(name) {
		return fmt.Errorf("invalid project name %q: use letters, digits, '.', '_' or '-'", name)
	}
	if domain != "" && (len(domain) > 253 || !domainRe.MatchString(domain)) {
		return fmt.Errorf("invalid domain %q", domain)
	}
	for _, p := range ports {
		if _, _, err := ParsePortMapping(p); err != nil {
			return err
		}
	}
	for _, e := range envVars {
		if !envKeyRe.MatchString(e.Key) {
			return fmt.Errorf("invalid environment variable name %q", e.Key)
		}
	}
	return nil
}
