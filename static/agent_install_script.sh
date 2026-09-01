#!/bin/bash
        
set -e

HUB_URL="%s"

# Derive a short slug from the hub URL for namespacing
# e.g. https://hub.asdl.website -> hub-asdl-website
HUB_SLUG=$(echo "$HUB_URL" | sed 's|https\?://||' | sed 's|/.*||' | sed 's|[^a-zA-Z0-9]|-|g' | tr '[:upper:]' '[:lower:]' | sed 's|-*$||' | cut -c1-15)

AGENT_BIN="/usr/local/bin/asdl-agent-${HUB_SLUG}"
AGENT_SERVICE="asdl-agent-${HUB_SLUG}"
WG_IFACE="asdl-$(echo "$HUB_URL" | sed 's|https\?://||' | sed 's|/.*||' | sed 's|[^a-zA-Z0-9]|-|g' | tr '[:upper:]' '[:lower:]' | sed 's|-*$||' | cut -c1-10)"

echo "╔══════════════════════════════════════╗"
echo "║        ASDL Hub Node Enrollment      ║"
echo "╚══════════════════════════════════════╝"
echo ""
echo "   Hub:  $HUB_URL"
echo "   Slug: $HUB_SLUG"
echo ""
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Rollback on failure
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
ROLLBACK_DONE=0
rollback() {
    [ "$ROLLBACK_DONE" -eq 1 ] && return
    ROLLBACK_DONE=1

    echo ""
    echo "Installation failed — rolling back..."

    # Tell the hub to clean up if we got far enough to enroll
    if [ -n "${NODE_ID:-}" ] && [ "$NODE_ID" != "null" ]; then
        echo "   Notifying hub to remove node record..."
        curl -fsSL -X DELETE "${HUB_URL}/api/v1/enrollment/rollback/${NODE_ID}" \
            2>/dev/null || true
    fi

    case "$OS" in
        linux)
            systemctl stop "$AGENT_SERVICE" 2>/dev/null || true
            systemctl disable "$AGENT_SERVICE" 2>/dev/null || true
            rm -f "/etc/systemd/system/${AGENT_SERVICE}.service"
            systemctl daemon-reload 2>/dev/null || true

            systemctl stop "wg-quick@${WG_IFACE}" 2>/dev/null || true
            systemctl disable "wg-quick@${WG_IFACE}" 2>/dev/null || true
            wg-quick down "${WG_IFACE}" 2>/dev/null || true
            rm -f "/etc/wireguard/${WG_IFACE}.conf"

            rm -f "$AGENT_BIN"
            rm -rf "/etc/asdl/${HUB_SLUG}"
            ;;
        darwin)
            PLIST_LABEL="website.asdl.agent.${HUB_SLUG}"
            PLIST_PATH="/Library/LaunchDaemons/${PLIST_LABEL}.plist"
            sudo launchctl bootout "system/${PLIST_LABEL}" 2>/dev/null || true
            sudo rm -f "$PLIST_PATH"

            sudo wg-quick down "/usr/local/etc/wireguard/${WG_IFACE}.conf" 2>/dev/null || true
            sudo rm -f "/usr/local/etc/wireguard/${WG_IFACE}.conf"

            sudo rm -f "$AGENT_BIN"
            sudo rm -rf "/usr/local/etc/asdl/${HUB_SLUG}"
            ;;
    esac

    echo "Rollback completed."
    echo ""
}

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
            if SSH_USER=$(logname 2>/dev/null) && [ -n "$SSH_USER" ]; then
                :
            elif SSH_USER=$(stat -f '%Su' /dev/console 2>/dev/null) && [ -n "$SSH_USER" ]; then
                :
            elif [ -n "$SUDO_USER" ]; then
                SSH_USER="$SUDO_USER"
            elif [ -n "$USER" ]; then
                SSH_USER="$USER"
            else
                SSH_USER=$(whoami)
            fi
            ;;
        *)
            echo "Unsupported OS: $OS"
            echo "   Supported: linux, darwin (macOS)"
            exit 1
            ;;
    esac

    if [ -z "$SSH_USER" ] || [ "$SSH_USER" = "root" ]; then
        echo "Could not determine the actual user. Do not run as root."
        exit 1
    fi

    echo "Detected:"
    echo "   OS:       $OS"
    echo "   Arch:     $ARCH"
    echo "   Hostname: $HOSTNAME"
    echo "   User:     $SSH_USER"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 2: Check privileges
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
check_privileges() {
    case "$OS" in
        linux)
            if [ "$EUID" -ne 0 ]; then
                echo "Please run as root (sudo bash)"
                exit 1
            fi
            ;;
        darwin)
            if [ "$EUID" -eq 0 ]; then
                echo "Do not run as root on macOS. Run without sudo:"
                echo "   curl -fsSL ${HUB_URL}/install | bash"
                exit 1
            fi
            if ! sudo -v 2>/dev/null; then
                echo "This script requires sudo access on macOS."
                exit 1
            fi
            ;;
    esac
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 2.5: Check for existing agent for this hub
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
check_existing_agent() {
    if [ -f "$AGENT_BIN" ] || systemctl is-active --quiet "$AGENT_SERVICE" 2>/dev/null; then
        echo ""
        echo " This node is already enrolled with $HUB_URL"
        echo ""
        read -r -p "   Re-enroll? This will replace the existing agent. [y/N]: " confirm </dev/tty
        if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
            echo "Aborted."
            exit 0
        fi
        # Stop existing before re-enrolling
        case "$OS" in
            linux)
                systemctl stop "$AGENT_SERVICE" 2>/dev/null || true
                ;;
            darwin)
                sudo launchctl bootout "system/website.asdl.agent.${HUB_SLUG}" 2>/dev/null || true
                ;;
        esac
    fi
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 3: Collect enrollment info
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
collect_info() {
    read -p "Enter enrollment token: " ENROLLMENT_TOKEN </dev/tty

    echo ""
    echo "Enrollment info:"
    echo "   Token: [hidden]"
    echo "   Hub URL: $HUB_URL"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 4: Install dependencies
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
install_dependencies() {
    echo ""
    echo "Installing dependencies..."

    case "$OS" in
        linux)
            if command -v apt-get &>/dev/null; then
                apt-get update -qq && apt-get install -y -qq wireguard wireguard-tools curl jq
            elif command -v dnf &>/dev/null; then
                dnf install -y -q wireguard-tools curl jq
            elif command -v yum &>/dev/null; then
                yum install -y -q wireguard-tools curl jq
            else
                echo "No supported package manager found (apt, dnf, yum)"
                exit 1
            fi
            ;;
        darwin)
            if ! command -v brew &>/dev/null; then
                echo "Homebrew required. Install from https://brew.sh"
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

    echo "Dependencies installed"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 5: Generate WireGuard keys
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
generate_wireguard_keys() {
    echo ""
    echo "Generating WireGuard keypair..."
    WG_PRIVATE_KEY=$(wg genkey)
    WG_PUBLIC_KEY=$(echo "$WG_PRIVATE_KEY" | wg pubkey)
    echo "   Public key: $WG_PUBLIC_KEY"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 6: Gather system info
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
gather_system_info() {
    echo ""
    echo "Gathering system information..."

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
    echo "Enrolling with hub..."

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
    WG_NETWORK=$(echo "$RESPONSE" | jq -r '.wireguard_network // "10.100.0.0/24"')

    if [ "$NODE_ID" = "null" ] || [ -z "$NODE_ID" ]; then
        echo "Enrollment failed. Response was:"
        echo "$RESPONSE"
        exit 1
    fi

    echo "Enrolled!"
    echo "   Node ID:     $NODE_ID"
    echo "   Assigned IP: $ASSIGNED_IP"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 8: Add SSH key
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
add_ssh_key() {
    if [ -n "$SSH_PUBLIC_KEY" ] && [ "$SSH_PUBLIC_KEY" != "null" ]; then
        echo ""
        echo "Adding SSH key for terminal access..."
        SSH_HOME=$(eval echo "~${SSH_USER}")
        mkdir -p "${SSH_HOME}/.ssh"
        echo "$SSH_PUBLIC_KEY" >> "${SSH_HOME}/.ssh/authorized_keys"
        chmod 700 "${SSH_HOME}/.ssh"
        chmod 600 "${SSH_HOME}/.ssh/authorized_keys"
        chown -R "${SSH_USER}:${SSH_USER}" "${SSH_HOME}/.ssh" 2>/dev/null || true
        echo "SSH key added for user: $SSH_USER"
    fi
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 9: Download agent
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
download_agent() {
    echo ""
    echo "Downloading ASDL agent..."

    case "$OS" in
        linux)
            BINARY="asdl-agent-linux"
            curl -fsSL "https://github.com/asadullahbro/asdl-agent/releases/latest/download/${BINARY}" \
                -o "$AGENT_BIN"
            chmod +x "$AGENT_BIN"
            ;;
        darwin)
            BINARY="asdl-agent-mac"
            if [ "$ARCH" = "arm64" ]; then BINARY="asdl-agent-mac-arm64"; fi
            curl -fsSL "https://github.com/asadullahbro/asdl-agent/releases/latest/download/${BINARY}" \
                -o /tmp/asdl-agent-tmp
            sudo mv /tmp/asdl-agent-tmp "$AGENT_BIN"
            sudo chmod +x "$AGENT_BIN"
            ;;
    esac

    echo "Agent downloaded to $AGENT_BIN"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 9.5: Find a free port for the dashboard
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
find_dashboard_port() {
    echo ""
    echo "Finding a free port for the agent dashboard..."

    DASHBOARD_PORT=""
    for port in 8081 8082 8083 8084 8085 8086 8087 8088 8089 8090; do
        if ! ss -tlnp 2>/dev/null | grep -q ":${port} "; then
            DASHBOARD_PORT=$port
            break
        fi
    done

    if [ -z "$DASHBOARD_PORT" ]; then
        echo "   Could not find a free port in range 8081-8090, defaulting to 8099"
        DASHBOARD_PORT=8099
    fi

    echo "   Dashboard port: $DASHBOARD_PORT"
}
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 10: Save agent config
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
save_agent_config() {
    echo ""
    echo "Saving agent configuration..."

    case "$OS" in
        linux)
            mkdir -p "/etc/asdl/${HUB_SLUG}"
            cat > "/etc/asdl/${HUB_SLUG}/agent.conf" << EOF
hub_url: http://${HUB_VPN_IP}:${HUB_PORT}
node_id: ${NODE_ID}
vpn_ip: ${ASSIGNED_IP}
enrolled: true
interval: 30s
work_dir: /tmp/asdl-${HUB_SLUG}
max_jobs: 5
dashboard:
  port: ${DASHBOARD_PORT}
EOF
            chmod 600 "/etc/asdl/${HUB_SLUG}/agent.conf"
            ;;
        darwin)
            sudo mkdir -p "/usr/local/etc/asdl/${HUB_SLUG}"
            sudo tee "/usr/local/etc/asdl/${HUB_SLUG}/agent.conf" > /dev/null << EOF
hub_url: http://${HUB_VPN_IP}:${HUB_PORT}
node_id: ${NODE_ID}
vpn_ip: ${ASSIGNED_IP}
enrolled: true
interval: 30s
work_dir: /tmp/asdl-${HUB_SLUG}
max_jobs: 5
dashboard:
  port: ${DASHBOARD_PORT}
EOF
            sudo chmod 600 "/usr/local/etc/asdl/${HUB_SLUG}/agent.conf"
            ;;
    esac

    echo "Agent config saved"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 11: Install service (systemd / launchd)
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
install_service() {
    echo ""
    echo "Installing service..."

    case "$OS" in
        linux)
            cat > "/etc/systemd/system/${AGENT_SERVICE}.service" << EOF
[Unit]
Description=ASDL Agent (${HUB_SLUG})
After=network.target wg-quick@${WG_IFACE}.service
Wants=wg-quick@${WG_IFACE}.service

[Service]
Type=simple
ExecStart=${AGENT_BIN} -config /etc/asdl/${HUB_SLUG}/agent.conf
Restart=always
RestartSec=10
User=root

[Install]
WantedBy=multi-user.target
EOF
            systemctl daemon-reload
            ;;
        darwin)
            PLIST_LABEL="website.asdl.agent.${HUB_SLUG}"
            PLIST_PATH="/Library/LaunchDaemons/${PLIST_LABEL}.plist"
            sudo tee "$PLIST_PATH" > /dev/null << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>${PLIST_LABEL}</string>

    <key>ProgramArguments</key>
    <array>
        <string>${AGENT_BIN}</string>
        <string>-config</string>
        <string>/usr/local/etc/asdl/${HUB_SLUG}/agent.conf</string>
    </array>

    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/var/log/asdl-agent-${HUB_SLUG}.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/asdl-agent-${HUB_SLUG}.err</string>
</dict>
</plist>
EOF
            sudo chown root:wheel "$PLIST_PATH"
            sudo chmod 644 "$PLIST_PATH"
            ;;
    esac

    echo "Service installed: $AGENT_SERVICE"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 12: Write WireGuard config
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
write_wireguard_config() {
    echo ""
    echo "Writing WireGuard configuration..."

    case "$OS" in
        linux)
            mkdir -p /etc/wireguard
            cat > "/etc/wireguard/${WG_IFACE}.conf" << EOF
[Interface]
PrivateKey = ${WG_PRIVATE_KEY}
Address = ${ASSIGNED_IP}/24

[Peer]
PublicKey = ${HUB_WG_PUBKEY}
Endpoint = ${HUB_WG_ENDPOINT}
AllowedIPs = ${WG_NETWORK}
PersistentKeepalive = 25
EOF
            chmod 600 "/etc/wireguard/${WG_IFACE}.conf"
            ;;
        darwin)
            sudo mkdir -p /usr/local/etc/wireguard
            sudo tee "/usr/local/etc/wireguard/${WG_IFACE}.conf" > /dev/null << EOF
[Interface]
PrivateKey = ${WG_PRIVATE_KEY}
Address = ${ASSIGNED_IP}/24

[Peer]
PublicKey = ${HUB_WG_PUBKEY}
Endpoint = ${HUB_WG_ENDPOINT}
AllowedIPs = ${WG_NETWORK}
PersistentKeepalive = 25
EOF
            sudo chmod 600 "/usr/local/etc/wireguard/${WG_IFACE}.conf"
            ;;
    esac

    echo "WireGuard config saved: ${WG_IFACE}"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 13: Bring up WireGuard
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
start_wireguard() {
    echo ""
    echo "Starting WireGuard..."

    case "$OS" in
        linux)
            systemctl enable "wg-quick@${WG_IFACE}" 2>/dev/null || true
            systemctl start "wg-quick@${WG_IFACE}"
            ;;
        darwin)
            sudo route delete -net "${WG_NETWORK}" 2>/dev/null || true
            sudo wg-quick down "/usr/local/etc/wireguard/${WG_IFACE}.conf" 2>/dev/null || true
            sudo wg-quick up "/usr/local/etc/wireguard/${WG_IFACE}.conf"
            if ! netstat -rn | grep -q utun; then
                echo "Route not added by wg-quick, adding manually..."
                TUNNEL=$(sudo wg show "${WG_IFACE}" 2>/dev/null | grep -o 'utun[0-9]*' | head -1)
                if [ -n "$TUNNEL" ]; then
                    sudo route add -net "${WG_NETWORK}" -interface "$TUNNEL" 2>/dev/null || true
                fi
            fi
            ;;
    esac

    echo "WireGuard started — IP: $ASSIGNED_IP"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# STEP 14: Start service
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
start_service() {
    echo ""
    echo "Starting service..."

    case "$OS" in
        linux)
            systemctl enable "$AGENT_SERVICE"
            systemctl start "$AGENT_SERVICE"
            ;;
        darwin)
            PLIST_LABEL="website.asdl.agent.${HUB_SLUG}"
            PLIST_PATH="/Library/LaunchDaemons/${PLIST_LABEL}.plist"
            sudo launchctl bootout "system/${PLIST_LABEL}" 2>/dev/null || true
            sudo launchctl bootstrap system "$PLIST_PATH"
            ;;
    esac

    echo "Service started: $AGENT_SERVICE"
}

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Main execution
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

detect_os
check_privileges
check_existing_agent
collect_info
install_dependencies
generate_wireguard_keys
gather_system_info
enroll_with_hub
add_ssh_key
download_agent
find_dashboard_port
save_agent_config
install_service
write_wireguard_config
start_wireguard
start_service

ROLLBACK_DONE=1

# Get the node's public IP for the dashboard URL
NODE_PUBLIC_IP=$(curl -fsSL --max-time 5 https://api.ipify.org 2>/dev/null || echo "localhost")

echo ""
echo "╔══════════════════════════════════════╗"
echo "║         Enrollment Complete!         ║"
echo "╚══════════════════════════════════════╝"
echo ""
echo "   Node ID:    ${NODE_ID}"
echo "   VPN IP:     ${ASSIGNED_IP}"
echo "   Hub:        http://${HUB_VPN_IP}:${HUB_PORT}"
echo "   Interface:  ${WG_IFACE}"
echo "   Service:    ${AGENT_SERVICE}"
echo "   OS:         ${OS}"
echo ""
echo "   ┌─────────────────────────────────────┐"
echo "   │  Agent Dashboard                    │"
echo "   │  http://${NODE_PUBLIC_IP}:${DASHBOARD_PORT}          │"
echo "   │  (also accessible at               │"
echo "   │   http://${ASSIGNED_IP}:${DASHBOARD_PORT})           │"
echo "   └─────────────────────────────────────┘"
echo ""
echo "Node will appear in your hub dashboard shortly."