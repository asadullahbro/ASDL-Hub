// internal/monitor/monitor.go - Add Docker detection
func (m *Monitor) detectCapabilities() []string {
    caps := []string{"bash"}

    // Check for Docker
    if _, err := os.Stat("/var/run/docker.sock"); err == nil {
        caps = append(caps, "docker")
    }
    if err := exec.Command("docker", "--version").Run(); err == nil {
        if !contains(caps, "docker") {
            caps = append(caps, "docker")
        }
    }

    // Check for Git
    if err := exec.Command("git", "--version").Run(); err == nil {
        caps = append(caps, "git")
    }

    // Check for Python
    if err := exec.Command("python3", "--version").Run(); err == nil {
        caps = append(caps, "python")
    }

    // Check for Node
    if err := exec.Command("node", "--version").Run(); err == nil {
        caps = append(caps, "node")
    }

    // Check for Go
    if err := exec.Command("go", "version").Run(); err == nil {
        caps = append(caps, "go")
    }

    return caps
}

func contains(slice []string, item string) bool {
    for _, s := range slice {
        if s == item {
            return true
        }
    }
    return false
}