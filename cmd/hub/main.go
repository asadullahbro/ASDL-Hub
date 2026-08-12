package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/asdl/hub/internal/api/handlers"
	"github.com/asdl/hub/internal/api/middleware"
	"github.com/asdl/hub/internal/db"
	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/services"
)

type Config struct {
	Server struct {
		Port        int      `yaml:"port"`
		VPNNetworks []string `yaml:"vpn_networks"`
	} `yaml:"server"`
	Database struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		User     string `yaml:"user"`
		Password string `yaml:"password"`
		Name     string `yaml:"name"`
		SSLMode  string `yaml:"sslmode"`
	} `yaml:"database"`
}

func generateInstallScript() string {
	hubDomain := os.Getenv("HUB_DOMAIN")
	hubPort := os.Getenv("SERVER_PORT")

	var hubURL string

	if hubDomain != "" {
		hubURL = "https://" + hubDomain
	} else {
		// fallback to VPN IP with port
		hubURL = fmt.Sprintf("http://%s:%s", os.Getenv("WG_HUB_IP"), hubPort)
	}
	script := fmt.Sprintf(`#!/bin/bash
        
set -e

HUB_URL="%s"

echo "╔══════════════════════════════════════╗"
echo "║        ASDL Hub Node Enrollment      ║"
echo "╚══════════════════════════════════════╝"
echo ""

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 1: Detect OS
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
detect_os() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    if [ "$ARCH" = "x86_64" ]; then ARCH="amd64"; fi
    if [ "$ARCH" = "aarch64" ]; then ARCH="arm64"; fi

    HOSTNAME=$(hostname)

    case "$OS" in
        linux)
            SSH_USER=$(logname 2>/dev/null \
                || echo "$SUDO_USER" \
                || echo "$USER" \
                || whoami)
            ;;
        darwin)
            SSH_USER=$(logname 2>/dev/null \
                || stat -f '%Su' /dev/console 2>/dev/null \
                || echo "$SUDO_USER" \
                || echo "$USER" \
                || whoami)
            ;;
        *)
            echo "❌ Unsupported OS: $OS"
            echo "   Supported: linux, darwin (macOS)"
            exit 1
            ;;
    esac

    if [ -z "$SSH_USER" ] || [ "$SSH_USER" = "root" ]; then
        echo "❌ Could not determine the actual user. Do not run as root."
        exit 1
    fi

    echo "📋 Detected:"
    echo "   OS: $OS"
    echo "   Arch: $ARCH"
    echo "   Hostname: $HOSTNAME"
    echo "   SSH User: $SSH_USER"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 2: Check privileges
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 2: Check privileges
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
check_privileges() {
    case "$OS" in
        linux)
            if [ "$EUID" -ne 0 ]; then
                echo "❌ Please run as root (sudo bash)"
                exit 1
            fi
            ;;
        darwin)
            if [ "$EUID" -eq 0 ]; then
                echo "❌ Do not run as root on macOS. Run without sudo:"
                echo "   curl -fsSL ${HUB_URL}/install | bash"
                exit 1
            fi
            if ! sudo -v 2>/dev/null; then
                echo "❌ This script requires sudo access on macOS."
                exit 1
            fi
            ;;
    esac
}
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 3: Collect enrollment info
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
collect_info() {
    read -p "Enter enrollment token: " ENROLLMENT_TOKEN </dev/tty

    echo ""
    echo "📋 Enrollment info:"
    echo "   Token: [hidden]"
    echo "   Hub URL: $HUB_URL"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 4: Install dependencies
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
install_dependencies() {
    echo ""
    echo "📦 Installing dependencies..."

    case "$OS" in
        linux)
            if command -v apt-get &>/dev/null; then
                apt-get update -qq && apt-get install -y -qq wireguard wireguard-tools curl jq
            elif command -v dnf &>/dev/null; then
                dnf install -y -q wireguard-tools curl jq
            elif command -v yum &>/dev/null; then
                yum install -y -q wireguard-tools curl jq
            else
                echo "❌ No supported package manager found (apt, dnf, yum)"
                exit 1
            fi
            ;;
        darwin)
            if ! command -v brew &>/dev/null; then
                echo "❌ Homebrew required. Install from https://brew.sh"
                exit 1
            fi
            BREW_INSTALL=()
            command -v wg   &>/dev/null || BREW_INSTALL+=("wireguard-tools")
            command -v curl &>/dev/null || BREW_INSTALL+=("curl")
            command -v jq   &>/dev/null || BREW_INSTALL+=("jq")
            if [ ${#BREW_INSTALL[@]} -gt 0 ]; then
                sudo -u "$SSH_USER" brew install "${BREW_INSTALL[@]}" < /dev/null
            fi
            ;;
    esac

    echo "✅ Dependencies installed"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 5: Generate WireGuard keys
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
generate_wireguard_keys() {
    echo ""
    echo "🔑 Generating WireGuard keypair..."
    WG_PRIVATE_KEY=$(wg genkey)
    WG_PUBLIC_KEY=$(echo "$WG_PRIVATE_KEY" | wg pubkey)
    echo "   Public key: $WG_PUBLIC_KEY"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 6: Gather system info
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
gather_system_info() {
    echo ""
    echo "📊 Gathering system information..."

    case "$OS" in
        linux)
            CPU=$(nproc)
            MEMORY=$(free -b | awk '/Mem:/{print $2}')
            DISK=$(df -B1 / | awk 'NR==2{print $2}')
            ;;
        darwin)
            CPU=$(sysctl -n hw.ncpu)
            MEMORY=$(sysctl -n hw.memsize)
            DISK=$(df -B1 / | awk 'NR==2{print $2}')
            ;;
    esac

    echo "   CPU: $CPU cores"
    echo "   Memory: $((MEMORY / 1024 / 1024 / 1024)) GB"
    echo "   Disk: $((DISK / 1024 / 1024 / 1024)) GB"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 7: Enroll with Hub
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
enroll_with_hub() {
    echo ""
    echo "🤝 Enrolling with hub..."

    RESPONSE=$(curl -fsSL -X POST "${HUB_URL}/api/v1/enrollment/enroll" \
        -H "Content-Type: application/json" \
        -d "{
            \"token\": \"${ENROLLMENT_TOKEN}\",
            \"hostname\": \"${HOSTNAME}\",
            \"wireguard_public_key\": \"${WG_PUBLIC_KEY}\",
            \"os\": \"${OS}\",
            \"arch\": \"${ARCH}\",
            \"cpu\": ${CPU},
            \"memory_total\": ${MEMORY},
            \"disk_total\": ${DISK},
            \"capabilities\": [\"docker\"],
            \"ssh_user\": \"${SSH_USER}\"
        }")

    NODE_ID=$(echo "$RESPONSE" | jq -r '.node_id')
    ASSIGNED_IP=$(echo "$RESPONSE" | jq -r '.assigned_ip')
    HUB_WG_PUBKEY=$(echo "$RESPONSE" | jq -r '.hub_wireguard_public_key')
    HUB_WG_ENDPOINT=$(echo "$RESPONSE" | jq -r '.hub_wireguard_endpoint')
    SSH_PUBLIC_KEY=$(echo "$RESPONSE" | jq -r '.ssh_public_key')
    HUB_VPN_IP=$(echo "$RESPONSE" | jq -r '.hub_vpn_ip')
    HUB_PORT=$(echo "$RESPONSE" | jq -r '.hub_port')

    if [ "$NODE_ID" = "null" ] || [ -z "$NODE_ID" ]; then
        echo "❌ Enrollment failed. Response was:"
        echo "$RESPONSE"
        exit 1
    fi

    echo "✅ Enrolled!"
    echo "   Node ID: $NODE_ID"
    echo "   Assigned IP: $ASSIGNED_IP"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 8: Add SSH key
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
add_ssh_key() {
    if [ -n "$SSH_PUBLIC_KEY" ] && [ "$SSH_PUBLIC_KEY" != "null" ]; then
        echo ""
        echo "🔑 Adding SSH key for terminal access..."
        SSH_HOME=$(eval echo "~${SSH_USER}")
        mkdir -p "${SSH_HOME}/.ssh"
        echo "$SSH_PUBLIC_KEY" >> "${SSH_HOME}/.ssh/authorized_keys"
        chmod 700 "${SSH_HOME}/.ssh"
        chmod 600 "${SSH_HOME}/.ssh/authorized_keys"
        chown -R "${SSH_USER}:${SSH_USER}" "${SSH_HOME}/.ssh" 2>/dev/null || true
        echo "✅ SSH key added for user: $SSH_USER"
    fi
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 9: Download agent
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
download_agent() {
    echo ""
    echo "📥 Downloading ASDL agent..."

    case "$OS" in
        linux)
            BINARY="asdl-agent-linux"
            curl -fsSL "https://github.com/asadullahbro/asdl-agent/releases/latest/download/${BINARY}" \
                -o /usr/local/bin/asdl-agent
            chmod +x /usr/local/bin/asdl-agent
            ;;
        darwin)
            BINARY="asdl-agent-mac"
            if [ "$ARCH" = "arm64" ]; then BINARY="asdl-agent-mac-arm64"; fi
            curl -fsSL "https://github.com/asadullahbro/asdl-agent/releases/latest/download/${BINARY}" \
                -o /tmp/asdl-agent
            sudo mv /tmp/asdl-agent /usr/local/bin/asdl-agent
            sudo chmod +x /usr/local/bin/asdl-agent
            ;;
    esac

    echo "✅ Agent downloaded to /usr/local/bin/asdl-agent"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 10: Save agent config
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
save_agent_config() {
    echo ""
    echo "💾 Saving agent configuration..."

    # Agent always communicates over the WireGuard mesh — never the public URL
    case "$OS" in
        linux)
            mkdir -p /etc/asdl
            cat > /etc/asdl/agent.conf << EOF
hub_url: http://${HUB_VPN_IP}:${HUB_PORT}
node_id: ${NODE_ID}
vpn_ip: ${ASSIGNED_IP}
enrolled: true
interval: 30s
work_dir: /tmp/asdl
max_jobs: 5
EOF
            chmod 600 /etc/asdl/agent.conf
            ;;
        darwin)
            sudo mkdir -p /usr/local/etc/asdl
            sudo tee /usr/local/etc/asdl/agent.conf > /dev/null << EOF
hub_url: http://${HUB_VPN_IP}:${HUB_PORT}
node_id: ${NODE_ID}
vpn_ip: ${ASSIGNED_IP}
enrolled: true
interval: 30s
work_dir: /tmp/asdl
max_jobs: 5
EOF
            sudo chmod 600 /usr/local/etc/asdl/agent.conf
            ;;
    esac

    echo "✅ Agent config saved"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 11: Install service (systemd / launchd)
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
install_service() {
    echo ""
    echo "⚙️  Installing service..."

    case "$OS" in
        linux)
            cat > /etc/systemd/system/asdl-agent.service << EOF
[Unit]
Description=ASDL Agent
After=network.target wg-quick@asdl0.service
Wants=wg-quick@asdl0.service

[Service]
Type=simple
ExecStart=/usr/local/bin/asdl-agent -config /etc/asdl/agent.conf
Restart=always
RestartSec=10
User=root

[Install]
WantedBy=multi-user.target
EOF
            systemctl daemon-reload
            ;;
        darwin)
            PLIST_PATH="/Library/LaunchDaemons/website.asdl.agent.plist"
            sudo tee "$PLIST_PATH" > /dev/null << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>website.asdl.agent</string>

    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/asdl-agent</string>
        <string>-config</string>
        <string>/usr/local/etc/asdl/agent.conf</string>
    </array>

    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/var/log/asdl-agent.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/asdl-agent.err</string>
</dict>
</plist>
EOF
            sudo chown root:wheel "$PLIST_PATH"
            sudo chmod 644 "$PLIST_PATH"
            ;;
    esac

    echo "✅ Service definition installed"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 12: Write WireGuard config
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
write_wireguard_config() {
    echo ""
    echo "🔧 Writing WireGuard configuration..."

    case "$OS" in
        linux)
            mkdir -p /etc/wireguard
            cat > /etc/wireguard/asdl0.conf << EOF
[Interface]
PrivateKey = ${WG_PRIVATE_KEY}
Address = ${ASSIGNED_IP}/24

[Peer]
PublicKey = ${HUB_WG_PUBKEY}
Endpoint = ${HUB_WG_ENDPOINT}
AllowedIPs = 10.100.0.0/24
PersistentKeepalive = 25
EOF
            chmod 600 /etc/wireguard/asdl0.conf
            ;;
        darwin)
            sudo mkdir -p /usr/local/etc/wireguard
            sudo tee /usr/local/etc/wireguard/asdl0.conf > /dev/null << EOF
[Interface]
PrivateKey = ${WG_PRIVATE_KEY}
Address = ${ASSIGNED_IP}/24

[Peer]
PublicKey = ${HUB_WG_PUBKEY}
Endpoint = ${HUB_WG_ENDPOINT}
AllowedIPs = 10.100.0.0/24
PersistentKeepalive = 25
EOF
            sudo chmod 600 /usr/local/etc/wireguard/asdl0.conf
            ;;
    esac

    echo "✅ WireGuard configuration saved"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 13: Bring up WireGuard
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
start_wireguard() {
    echo ""
    echo "🔄 Starting WireGuard..."

    case "$OS" in
        linux)
            systemctl enable wg-quick@asdl0 2>/dev/null || true
            systemctl start wg-quick@asdl0
            ;;
        darwin)
            # Remove any conflicting route before bringing tunnel up
            sudo route delete -net 10.100.0.0/24 2>/dev/null || true
            sudo wg-quick down /usr/local/etc/wireguard/asdl0.conf 2>/dev/null || true
            sudo wg-quick up /usr/local/etc/wireguard/asdl0.conf
            # Verify route was added
            if ! netstat -rn | grep -q utun; then
                echo "⚠️  Route not added by wg-quick, adding manually..."
                TUNNEL=$(sudo wg show asdl0 2>/dev/null | grep -o 'utun[0-9]*' | head -1)
                if [ -n "$TUNNEL" ]; then
                    sudo route add -net 10.100.0.0/24 -interface "$TUNNEL" 2>/dev/null || true
                fi
            fi
            ;;
    esac

    echo "✅ WireGuard started — IP: $ASSIGNED_IP"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 14: Start service
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
start_service() {
    echo ""
    echo "🚀 Starting service..."

    case "$OS" in
        linux)
            systemctl enable asdl-agent
            systemctl start asdl-agent
            ;;
        darwin)
            PLIST_PATH="/Library/LaunchDaemons/website.asdl.agent.plist"
            sudo launchctl bootout system/website.asdl.agent 2>/dev/null || true
            sudo launchctl bootstrap system "$PLIST_PATH"
            ;;
    esac

    echo "✅ Service started"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Main execution
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

detect_os
check_privileges
collect_info
install_dependencies
generate_wireguard_keys
gather_system_info
enroll_with_hub
add_ssh_key
download_agent
save_agent_config
install_service
write_wireguard_config
start_wireguard
start_service

echo ""
echo "╔══════════════════════════════════════╗"
echo "║         Enrollment Complete! ✅       ║"
echo "╚══════════════════════════════════════╝"
echo ""
echo "   Node ID:  ${NODE_ID}"
echo "   VPN IP:   ${ASSIGNED_IP}"
echo "   Hub:      http://${HUB_VPN_IP}:${HUB_PORT}"
echo "   OS:       ${OS}"
echo ""
echo "Node will appear in your dashboard shortly."
`, hubURL)
	return script
}

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using environment variables")
	}

	cfg := loadConfig()

	// Build PostgreSQL DSN
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.Name,
		cfg.Database.SSLMode,
	)

	database, err := db.Init(dsn)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Initialize auth first
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "change-me-in-production"
		log.Println("⚠️  WARNING: Using default JWT_SECRET. Set this in production!")
	}
	authService := services.NewAuthService(database, jwtSecret)
	authHandlers := handlers.NewAuthHandlers(authService)

	// Node service — manages node registration, heartbeats, and offline detection
	nodeService := services.NewNodeService(database)
	nodeService.StartOfflineSweeper()

	// Nginx service — generates and reloads nginx config from running projects
	nginxService := services.NewNginxService(database)

	// Job service — handles job queue (claim/complete lifecycle for agents)
	// DeploymentService injected after init to avoid circular dependency
	jobService := services.NewJobService(database)

	// Deployment service — creates deployments and dispatches deploy jobs
	deploymentService := services.NewDeploymentService(database, jobService, nodeService)
	jobService.SetDeploymentService(deploymentService)

	// Container service — manages raw container operations
	containerService := services.NewContainerService(database)

	// Project service — tracks running projects and their health state
	projectService := services.NewProjectService(database)

	// Migration service — handles container migrations between nodes
	migrationService := services.NewMigrationService(database, jobService, nginxService)
	migrationService.StartMigrationSweeper()

	// Health service — periodic health checks with auto-failover on unhealthy projects
	healthService := services.NewHealthService(database, migrationService, nginxService)
	healthService.StartHealthChecker()

	// Settings service and handler
	settingsService := services.NewSettingsService(database, authService, jwtSecret)
	settingsHandlers := handlers.NewSettingsHandlers(settingsService)

	// enrollment and wireguard services
	wireGuardService := services.NewWireGuardService(database)
	enrollmentService := services.NewEnrollmentService(database, wireGuardService, jwtSecret, cfg.Server.Port)
	enrollmentHandlers := handlers.NewEnrollmentHandlers(enrollmentService, wireGuardService)

	// terminal service and handlers
	terminalService := services.NewTerminalService(database, enrollmentService)
	terminalHandlers := handlers.NewTerminalHandlers(terminalService, authService)
	// Start the migration enforcer goroutine
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for range ticker.C {
			masterID := settingsService.GetMasterNodeID()
			if masterID != "" {
				migrationService.EnforceMasterNode(masterID)
			}
		}
	}()

	router := gin.Default()

	// CORS middleware
	router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		c.Header("Access-Control-Allow-Credentials", "true")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "healthy",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	// Auth routes (public - no auth required)
	auth := router.Group("/api/v1/auth")
	{
		auth.POST("/login", authHandlers.Login)
	}

	// PUBLIC API ROUTES - No authentication required
	public := router.Group("/api/v1")
	{
		// Public — agent calls this during enrollment
		public.POST("/enrollment/enroll", enrollmentHandlers.Enroll)

		// Install script
		router.GET("/install", func(c *gin.Context) {
			script := generateInstallScript()
			c.Header("Content-Type", "text/plain")
			c.String(http.StatusOK, script)
		})

		// Agent registration and heartbeats
		public.POST("/nodes", func(c *gin.Context) {
			clientIP := c.ClientIP()
			c.Set("vpn_ip", clientIP)
			nodeService.Register(c)
		})
		public.GET("/status", func(c *gin.Context) {
			var nodes []models.Node
			database.Order("online DESC, vpn_ip ASC").Find(&nodes)

			type NodeStatus struct {
				Hostname    string  `json:"hostname"`
				Online      bool    `json:"online"`
				PingLatency float64 `json:"ping_latency"`
			}

			result := make([]NodeStatus, len(nodes))
			for i, n := range nodes {
				result[i] = NodeStatus{
					Hostname:    n.Hostname,
					Online:      n.Online,
					PingLatency: n.PingLatency,
				}
			}

			onlineCount := 0
			for _, n := range nodes {
				if n.Online {
					onlineCount++
				}
			}

			c.JSON(http.StatusOK, gin.H{
				"nodes":  result,
				"online": onlineCount,
				"total":  len(nodes),
			})
		})
		public.POST("/nodes/:id/heartbeat", func(c *gin.Context) {
			clientIP := c.ClientIP()
			c.Set("vpn_ip", clientIP)
			nodeService.Heartbeat(c)
		})

		// Job claiming and completion
		public.POST("/jobs/claim", func(c *gin.Context) {
			clientIP := c.ClientIP()
			c.Set("vpn_ip", clientIP)
			jobService.Claim(c)
		})
		public.POST("/jobs/:id/complete", func(c *gin.Context) {
			clientIP := c.ClientIP()
			c.Set("vpn_ip", clientIP)
			jobService.Complete(c)
		})
	}

	// PROTECTED API ROUTES - Require JWT authentication with role-based access control
	protected := router.Group("/api/v1")
	protected.Use(middleware.Auth(authService))
	{
		// All authenticated roles
		protected.GET("/auth/me", authHandlers.Me)
		protected.GET("/nodes/:id/terminal", terminalHandlers.Terminal)
		protected.GET("/stats", func(c *gin.Context) {
			var nodes []models.Node
			var jobs []models.Job
			var projects []models.Project

			database.Find(&nodes)
			onlineNodes := 0
			for _, n := range nodes {
				if n.Online {
					onlineNodes++
				}
			}

			database.Find(&jobs)
			success, failed, pending, running := 0, 0, 0, 0
			for _, j := range jobs {
				switch j.Status {
				case models.JobStatusCompleted:
					success++
				case models.JobStatusFailed:
					failed++
				case models.JobStatusPending:
					pending++
				case models.JobStatusRunning:
					running++
				}
			}

			database.Find(&projects)
			healthyProjects, unhealthyProjects := 0, 0
			for _, p := range projects {
				if p.HealthStatus == "healthy" {
					healthyProjects++
				} else if p.HealthStatus == "unhealthy" {
					unhealthyProjects++
				}
			}

			c.JSON(http.StatusOK, gin.H{
				"nodes":             len(nodes),
				"onlineNodes":       onlineNodes,
				"jobs":              len(jobs),
				"success":           success,
				"failed":            failed,
				"pending":           pending,
				"running":           running,
				"projects":          len(projects),
				"healthyProjects":   healthyProjects,
				"unhealthyProjects": unhealthyProjects,
			})

		})

		// Viewer+ (all authenticated roles)
		viewer := protected.Group("/")
		viewer.Use(middleware.RequireRole(models.RoleAdmin, models.RoleOperator, models.RoleViewer))
		{
			viewer.GET("/nodes", nodeService.List)
			viewer.GET("/nodes/:id/details", nodeService.GetNodeDetails)
			viewer.GET("/nodes/:id", nodeService.Get)

			viewer.GET("/jobs", jobService.List)
			viewer.GET("/jobs/:id", jobService.Get)
			viewer.GET("/jobs/:id/logs", jobService.GetLogs)

			viewer.GET("/deployments", deploymentService.ListDeployments)
			viewer.GET("/deployments/:id", deploymentService.GetDeployment)

			viewer.GET("/containers", containerService.ListContainers)
			viewer.GET("/containers/:id", containerService.GetContainer)

			viewer.GET("/projects", projectService.ListProjects)
			viewer.GET("/projects/:id", projectService.GetProject)
			viewer.GET("/nodes/:id/projects", projectService.GetProjectsByNode)
			viewer.GET("/projects/:id/health", healthService.GetProjectHealth)

			viewer.GET("/migrations", migrationService.ListMigrations)
			viewer.GET("/migrations/:id", migrationService.GetMigration)
		}

		// Operator+ (operator and admin)
		operator := protected.Group("/")
		operator.Use(middleware.RequireRole(models.RoleAdmin, models.RoleOperator))
		{
			operator.POST("/agents/deploy", func(c *gin.Context) {
				var nodes []models.Node
				database.Where("online = ?", true).Find(&nodes)

				if len(nodes) == 0 {
					c.JSON(http.StatusOK, gin.H{"message": "no online nodes", "dispatched": 0})
					return
				}

				dispatched := 0
				for _, node := range nodes {
					job := &models.Job{
						ID:     uuid.New().String(),
						NodeID: node.ID,
						Type:   "agent_update",
						Status: models.JobStatusPending,
						Command: `set -e

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
if [ "$OS" = "darwin" ]; then
    BINARY="asdl-agent-mac"
    # Install coreutils if sha256sum is missing
    if ! command -v sha256sum &>/dev/null; then
        echo "📦 Installing coreutils for sha256sum..."
        if command -v brew &>/dev/null; then
            brew install coreutils
            # Add coreutils to PATH for this session
            export PATH="/usr/local/opt/coreutils/libexec/gnubin:$PATH"
            CHECKSUM_CMD="sha256sum"
        else
            echo "⚠️  Homebrew not found, installing from source may take time..."
            # Fallback to using shasum (built-in on macOS)
            CHECKSUM_CMD="shasum -a 256"
        fi
    else
        CHECKSUM_CMD="sha256sum"
    fi
else
    BINARY="asdl-agent-linux"
    # Install coreutils if sha256sum is missing (Linux)
    if ! command -v sha256sum &>/dev/null; then
        echo "📦 Installing coreutils for sha256sum..."
        if command -v apt-get &>/dev/null; then
            apt-get update -qq && apt-get install -y -qq coreutils
        elif command -v yum &>/dev/null; then
            yum install -y -q coreutils
        elif command -v apk &>/dev/null; then
            apk add --no-cache coreutils
        else
            echo "⚠️  No package manager found, checksum verification will be skipped"
            CHECKSUM_CMD=""
        fi
    else
        CHECKSUM_CMD="sha256sum"
    fi
fi

echo "📥 Downloading agent binary..."
curl -fsSL https://github.com/asadullahbro/asdl-agent/releases/latest/download/$BINARY -o /tmp/asdl-agent-new

# Skip checksum if no tool available
if [ -z "$CHECKSUM_CMD" ] || ! command -v $(echo $CHECKSUM_CMD | awk '{print $1}') &>/dev/null; then
    echo "⚠️  No checksum tool available, skipping verification"
    echo "✅ Binary downloaded without checksum verification"
    chmod +x /tmp/asdl-agent-new
    if [ "$OS" = "darwin" ]; then
        sudo mv /tmp/asdl-agent-new /usr/local/bin/asdl-agent
        launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.asdl.agent.plist 2>/dev/null || true
        launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.asdl.agent.plist
    else
        sudo mv /tmp/asdl-agent-new /usr/local/bin/asdl-agent
        (sleep 3 && sudo systemctl restart asdl-agent) &
    fi
    echo "✅ Agent updated successfully"
    exit 0
fi

echo "🔍 Verifying checksum..."
EXPECTED=$(curl -fsSL https://github.com/asadullahbro/asdl-agent/releases/latest/download/checksums.txt | grep "$BINARY" | awk '{print $1}')
ACTUAL=$($CHECKSUM_CMD /tmp/asdl-agent-new | awk '{print $1}')

if [ "$EXPECTED" != "$ACTUAL" ]; then
    echo "❌ Checksum mismatch! Expected: $EXPECTED, Got: $ACTUAL"
    rm /tmp/asdl-agent-new
    exit 1
fi

echo "✅ Checksum verified"
chmod +x /tmp/asdl-agent-new

if [ "$OS" = "darwin" ]; then
    sudo mv /tmp/asdl-agent-new /usr/local/bin/asdl-agent
    launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.asdl.agent.plist 2>/dev/null || true
    launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.asdl.agent.plist
else
    sudo mv /tmp/asdl-agent-new /usr/local/bin/asdl-agent
    (sleep 3 && sudo systemctl restart asdl-agent) &
fi

echo "✅ Agent updated successfully"
`,
						MaxRetries: 1,
						CreatedAt:  time.Now(),
					}
					if err := database.Create(job).Error; err != nil {
						log.Printf("⚠️ Failed to create agent_update job for node %s: %v", node.Hostname, err)
						continue
					}
					dispatched++
					log.Printf("📦 Agent update job dispatched to %s", node.Hostname)
				}

				c.JSON(http.StatusOK, gin.H{
					"message":    fmt.Sprintf("Agent update dispatched to %d nodes", dispatched),
					"dispatched": dispatched,
					"total":      len(nodes),
				})
			})

			operator.POST("/jobs", jobService.Create)
			operator.POST("/deployments", deploymentService.CreateDeployment)

			operator.POST("/nginx/update", func(c *gin.Context) {
				if err := nginxService.UpdateNginxConfig(); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"message": "nginx config updated"})
			})

			operator.POST("/containers/:id/stop", containerService.StopContainer)
			operator.POST("/containers/:id/start", containerService.StartContainer)
			operator.POST("/containers/:id/restart", containerService.RestartContainer)

			operator.POST("/projects", projectService.CreateProject)
			operator.PUT("/projects/:id", projectService.UpdateProject)
			operator.DELETE("/projects/:id", projectService.DeleteProject)

			operator.POST("/migrations", migrationService.MigrateProject)
		}

		// Admin only - Enrollment token management
		adminEnrollment := protected.Group("/enrollment")
		adminEnrollment.Use(middleware.RequireRole(models.RoleAdmin))
		{
			adminEnrollment.POST("/tokens", enrollmentHandlers.CreateToken)
			adminEnrollment.GET("/tokens", enrollmentHandlers.ListTokens)
			adminEnrollment.DELETE("/tokens/:id", enrollmentHandlers.RevokeToken)
			adminEnrollment.GET("/wireguard/status", enrollmentHandlers.WireGuardStatus)
		}

		// Admin only - Settings
		settings := protected.Group("/settings")
		settings.Use(middleware.RequireRole(models.RoleAdmin))
		{
			settings.POST("/verify-password", settingsHandlers.VerifyPassword)
			settings.GET("/tokens", settingsHandlers.ListTokens)
			settings.POST("/tokens", settingsHandlers.GenerateToken)
			settings.DELETE("/tokens/:id", settingsHandlers.RevokeToken)
			settings.GET("/github-token", settingsHandlers.GetGitHubToken)
			settings.POST("/github-token", settingsHandlers.SetGitHubToken)
			settings.GET("/master-node", settingsHandlers.GetMasterNode)
			settings.POST("/master-node", settingsHandlers.SetMasterNode)
			settings.DELETE("/master-node", settingsHandlers.ClearMasterNode)
			settings.GET("/users", settingsHandlers.ListUsers)
			settings.POST("/users", settingsHandlers.CreateUser)
			settings.PUT("/users/:id/password", settingsHandlers.ChangePassword)
			settings.PUT("/users/:id/role", settingsHandlers.ChangeRole)
			settings.DELETE("/users/:id", settingsHandlers.DeleteUser)
		}

		// Admin only — legacy permanent token endpoint
		protected.POST("/auth/permanent-token", middleware.RequireRole(models.RoleAdmin), authHandlers.GeneratePermanentToken)
	}

	staticDir := "./dashboard/out"
	router.Static("/_next", staticDir+"/_next")
	router.StaticFile("/favicon.ico", staticDir+"/favicon.ico")

	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		filePath := staticDir + path
		if _, err := os.Stat(filePath); err == nil {
			c.File(filePath)
			return
		}

		indexPath := staticDir + path + "/index.html"
		if _, err := os.Stat(indexPath); err == nil {
			c.File(indexPath)
			return
		}

		c.File(staticDir + "/index.html")
	})

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("Hub starting on port %d", cfg.Server.Port)
	log.Printf("VPN networks: %v", cfg.Server.VPNNetworks)
	log.Printf("Dashboard available at http://localhost:%d", cfg.Server.Port)

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func loadConfig() *Config {
	cfg := &Config{}

	// Server config from env
	cfg.Server.Port = getEnvAsInt("SERVER_PORT", 8080)
	cfg.Server.VPNNetworks = getEnvAsStringSlice("VPN_NETWORKS", []string{"10.100.0.0/24", "127.0.0.0/8", "::1/128"})

	// Database config from env
	cfg.Database.Host = getEnv("DB_HOST", "localhost")
	cfg.Database.Port = getEnvAsInt("DB_PORT", 5432)
	cfg.Database.User = getEnv("DB_USER", "asdl")
	cfg.Database.Password = getEnv("DB_PASSWORD", "dbpass123")
	cfg.Database.Name = getEnv("DB_NAME", "asdl_hub")
	cfg.Database.SSLMode = getEnv("DB_SSLMODE", "disable")

	return cfg
}

// Helper functions for environment variables
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvAsStringSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		// Parse comma-separated values
		var result []string
		for _, v := range splitAndTrim(value, ",") {
			if v != "" {
				result = append(result, v)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return defaultValue
}

func splitAndTrim(s, sep string) []string {
	parts := []string{}
	for _, part := range splitString(s, sep) {
		trimmed := trimSpace(part)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func splitString(s, sep string) []string {
	// Simple split implementation
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep[0] {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
