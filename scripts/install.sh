#!/usr/bin/env bash
set -Eeuo pipefail

# ASDL Hub installer
# Usage:
#   curl -fsSL https://get.asdl.website/asdl-hub | sudo bash

APP_NAME="ASDL Hub"
ASDL_VERSION=""  # stamped at release time by CI
INSTALL_DIR="/opt/asdl-hub"
SERVICE_NAME="asdl-hub"
RUN_USER="asdl-hub"
WG_INTERFACE="asdl0"
WG_PORT_HINT="${ASDL_WG_PORT:-51820}"
WG_PORT=""  # set by find_free_port
HUB_PORT="${ASDL_HUB_PORT:-}"  # set by find_free_hub_port if not specified
DOCS_URL="https://docs.asdl.website/asdl-hub"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info(){ echo -e "${BLUE}➜${NC} $*"; }
ok(){ echo -e "${GREEN}✓${NC} $*"; }
warn(){ echo -e "${YELLOW}⚠${NC} $*"; }
fail(){ echo -e "${RED}✗${NC} $*" >&2; }

die() {
    fail "$*"
    echo
    echo "Troubleshooting: $DOCS_URL"
    echo "Logs: journalctl -u $SERVICE_NAME -n 100 --no-pager"
    exit 1
}

trap 'echo; fail "Installation failed."; echo "See: $DOCS_URL"; echo "Logs: journalctl -u $SERVICE_NAME -n 100 --no-pager"' ERR

[[ $EUID -eq 0 ]] || { echo "Run with sudo."; exit 1; }

[[ -r /etc/os-release ]] || die "Cannot detect the operating system."
. /etc/os-release

case "${ID:-}" in
    ubuntu|debian) ok "Supported OS: ${PRETTY_NAME:-$ID}" ;;
    *) die "Unsupported OS: ${PRETTY_NAME:-unknown}. Ubuntu and Debian are currently supported." ;;
esac

ARCH="$(dpkg --print-architecture 2>/dev/null || true)"
case "$ARCH" in
    amd64|arm64) ok "Architecture: $ARCH" ;;
    *) die "Unsupported architecture: ${ARCH:-unknown}" ;;
esac

# ── Firewall ────────────────────────────────────────────────────────────────

setup_firewall() {
    if ! command -v ufw >/dev/null 2>&1; then
        warn "UFW not installed. Make sure your VPS firewall allows TCP 80/443 and UDP $WG_PORT."
        return
    fi

    if ! ufw status | grep -q '^Status: active'; then
        warn "UFW is inactive. Make sure your VPS firewall allows TCP 80, UDP $WG_PORT${USE_TLS:+, TCP 443}."
        return
    fi

    info "Configuring UFW..."
    ufw allow 80/tcp >/dev/null
    [[ "$USE_TLS" -eq 1 ]] && ufw allow 443/tcp >/dev/null
    ufw allow "${WG_PORT}/udp" >/dev/null
    ok "Firewall configured."
}

# ── Nginx ───────────────────────────────────────────────────────────────────

setup_nginx() {
    local CONFIG="/etc/nginx/sites-available/asdl-hub"
    local ENABLED="/etc/nginx/sites-enabled/asdl-hub"

    info "Configuring Nginx..."

    cat > "$CONFIG" <<EOF
server {
    listen 80;
    listen [::]:80;
    server_name $HUB_HOST;

    location / {
        proxy_pass http://127.0.0.1:$HUB_PORT;
        proxy_http_version 1.1;

        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;

        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";

        proxy_read_timeout 3600;
        proxy_connect_timeout 60;
    }

    location /ws/ {
        proxy_pass http://127.0.0.1:$HUB_PORT;
        proxy_http_version 1.1;

        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;

        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";

        proxy_read_timeout 3600;
        proxy_connect_timeout 60;
    }
}
EOF

    ln -sfn "$CONFIG" "$ENABLED"
    rm -f /etc/nginx/sites-enabled/default

    nginx -t
    systemctl enable nginx
    systemctl reload nginx
    ok "Nginx configured."

    if [[ "$USE_TLS" -eq 1 && -n "$HUB_DOMAIN" ]]; then
        info "Requesting Let's Encrypt certificate..."
        if ! command -v certbot >/dev/null 2>&1; then
            apt-get update -qq
            apt-get install -y certbot python3-certbot-nginx
        fi

        if certbot --nginx \
            --non-interactive \
            --agree-tos \
            --register-unsafely-without-email \
            --redirect \
            -d "$HUB_DOMAIN"; then
            ok "HTTPS configured."
        else
            warn "HTTPS could not be configured yet. Make sure DNS points $HUB_DOMAIN to this server."
            warn "The Hub is still available at: http://$HUB_DOMAIN"
        fi
    fi
}


# ── Main functions ───────────────────────────────────────────────────────────

prepare_source() {
    if [[ -f "$PWD/go.mod" && ( -d "$PWD/cmd" || -f "$PWD/main.go" ) ]]; then
        PROJECT_DIR="$PWD"
        ok "Using local ASDL Hub source: $PROJECT_DIR"
        return
    fi

    local tmp version
    tmp="$(mktemp -d)"
    trap '[[ -n "${tmp:-}" ]] && rm -rf "$tmp"' EXIT

    if [[ -n "${ASDL_VERSION:-}" ]]; then
        version="$ASDL_VERSION"
        ok "Pinned version: $version"
    else
        info "Fetching latest version..."
        version="$(curl -fsSL --max-time 10 \
            https://api.github.com/repos/asadullahbro/ASDL-Hub/releases/latest \
            | grep '"tag_name"' | head -1 | cut -d'"' -f4)"
        [[ -n "$version" ]] || die "Could not determine latest version."
        ok "Latest version: $version"
    fi

    info "Downloading ASDL Hub $version..."
    curl -fL --retry 3 --connect-timeout 10 \
        "https://github.com/asadullahbro/ASDL-Hub/releases/download/${version}/asdl-hub-${version}-linux-${ARCH}.tar.gz" \
        -o "$tmp/asdl-hub.tar.gz"

    tar -xzf "$tmp/asdl-hub.tar.gz" -C "$tmp"
    PROJECT_DIR="$(find "$tmp" -mindepth 1 -maxdepth 1 -type d | head -n1)"
    [[ -n "$PROJECT_DIR" ]] || die "Downloaded archive was empty."
    ok "Downloaded ASDL Hub $version."
}

install_packages() {
    info "Installing system dependencies..."
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y \
        ca-certificates curl openssl \
        postgresql postgresql-contrib \
        nginx \
        wireguard \
        iproute2 \
        iptables
    ok "System dependencies ready."
}

detect_existing() {
    if [[ -f "$INSTALL_DIR/.env" || -f "$INSTALL_DIR/bin/asdl-hub" ]]; then
        warn "Existing ASDL Hub installation detected. This will be upgraded in place."
    else
        ok "Fresh installation."
    fi
}

detect_public_ip() {
    local a b
    a="$(curl -4fsS --max-time 5 https://api.ipify.org 2>/dev/null || true)"
    b="$(curl -4fsS --max-time 5 https://ifconfig.me/ip 2>/dev/null || true)"

    if [[ -n "$a" && -n "$b" && "$a" != "$b" ]]; then
        warn "Public IP services disagree: $a vs $b"
        return 1
    fi

    [[ -n "$a" ]] && { printf '%s' "$a"; return 0; }
    [[ -n "$b" ]] && { printf '%s' "$b"; return 0; }
    return 1
}

configure_endpoint() {
    # Reuse existing config on upgrade
    if [[ -f "$INSTALL_DIR/.env" ]]; then
        existing_url="$(awk -F= '$1=="PUBLIC_URL"{print $2;exit}' "$INSTALL_DIR/.env" 2>/dev/null || true)"
        existing_port="$(awk -F= '$1=="SERVER_PORT"{print $2;exit}' "$INSTALL_DIR/.env" 2>/dev/null || true)"
        [[ -n "$existing_port" ]] && HUB_PORT="$existing_port"
        if [[ -n "$existing_url" ]]; then
            HUB_URL="$existing_url"
            if [[ "$HUB_URL" == https://* ]]; then
                HUB_DOMAIN="${HUB_URL#https://}"
                HUB_HOST="$HUB_DOMAIN"
                USE_TLS=1
            else
                HUB_HOST="${HUB_URL#http://}"
                HUB_DOMAIN=""
                USE_TLS=0
            fi
            ok "Reusing existing endpoint: $HUB_URL"
            return
        fi
    fi

    echo
    echo "How should users access this Hub?"
    echo "Leave the domain empty to use the detected public IP."
    echo
    read -r -p "Domain (optional): " HUB_DOMAIN </dev/tty

    if [[ -n "$HUB_DOMAIN" ]]; then
        [[ "$HUB_DOMAIN" =~ ^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$ ]] \
            || die "Invalid domain name: $HUB_DOMAIN"
        HUB_HOST="$HUB_DOMAIN"
        HUB_URL="https://$HUB_DOMAIN"
        USE_TLS=1
        ok "Using domain: $HUB_DOMAIN"
    else
        PUBLIC_IP="$(detect_public_ip || true)"
        [[ -n "${PUBLIC_IP:-}" ]] || read -r -p "Public IP (could not auto-detect): " PUBLIC_IP </dev/tty
        [[ "$PUBLIC_IP" =~ ^[0-9.]+$ ]] || die "Invalid public IP."
        HUB_HOST="$PUBLIC_IP"
        HUB_URL="http://$PUBLIC_IP"
        USE_TLS=0
        ok "Using public IP: $PUBLIC_IP"
    fi
}

ensure_user() {
    if ! id "$RUN_USER" >/dev/null 2>&1; then
        useradd --system --home-dir "$INSTALL_DIR" --shell /usr/sbin/nologin "$RUN_USER"
        ok "Created service user."
    fi
    mkdir -p "$INSTALL_DIR"
}

generate_secrets() {
    mkdir -p "$INSTALL_DIR"
    chmod 750 "$INSTALL_DIR"

    local old_jwt old_admin
    old_jwt="$(awk -F= '$1=="JWT_SECRET"{print $2;exit}' "$INSTALL_DIR/.env" 2>/dev/null || true)"
    old_admin="$(awk -F= '$1=="ADMIN_PASSWORD"{print $2;exit}' "$INSTALL_DIR/.env" 2>/dev/null || true)"

    if [[ -s "$INSTALL_DIR/.db_password" ]]; then
        DB_PASSWORD="$(cat "$INSTALL_DIR/.db_password")"
    else
        DB_PASSWORD="$(openssl rand -hex 32)"
        printf '%s\n' "$DB_PASSWORD" > "$INSTALL_DIR/.db_password"
        chmod 600 "$INSTALL_DIR/.db_password"
    fi

    JWT_SECRET="${old_jwt:-$(openssl rand -hex 48)}"
    ADMIN_PASSWORD="${old_admin:-$(openssl rand -base64 32 | tr -dc 'A-Za-z0-9' | head -c 24)}"

    ok "Secrets ready."
}

setup_database() {
    info "Configuring PostgreSQL..."
    systemctl enable --now postgresql

    if ! sudo -u postgres psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='asdl'" | grep -qx 1; then
        sudo -u postgres psql -v ON_ERROR_STOP=1 -c "CREATE ROLE asdl LOGIN PASSWORD '$DB_PASSWORD';"
    else
        sudo -u postgres psql -v ON_ERROR_STOP=1 -c "ALTER ROLE asdl WITH LOGIN PASSWORD '$DB_PASSWORD';"
    fi

    if ! sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='asdl_hub'" | grep -qx 1; then
        sudo -u postgres createdb -O asdl asdl_hub
    else
        sudo -u postgres psql -v ON_ERROR_STOP=1 -c "ALTER DATABASE asdl_hub OWNER TO asdl;"
    fi

    ok "PostgreSQL ready."
}
find_free_port() {
    local hint="${1:-51820}"
    for port in "$hint" 51821 51822 51823 51824 51825; do
        if ! ss -ulpn | grep -q ":${port} "; then
            echo "$port"
            return 0
        fi
    done
    die "Could not find a free UDP port for WireGuard (tried 51820-51825)."
}
find_free_hub_port() {
    local hint="${1:-8080}"
    for port in "$hint" 8081 8082 8083 8084 8085 3000 3001; do
        if ! ss -tlpn | grep -q ":${port} "; then
            echo "$port"
            return 0
        fi
    done
    die "Could not find a free TCP port for ASDL Hub (tried 8080-8085, 3000-3001)."
}

setup_wireguard() {
    info "Preparing WireGuard..."

    command -v wg >/dev/null || die "WireGuard tools are unavailable."
    modprobe wireguard 2>/dev/null || true

    if ip link show "$WG_INTERFACE" >/dev/null 2>&1; then
        ok "Reusing existing WireGuard interface: $WG_INTERFACE"

        WG_PORT="$(wg show "$WG_INTERFACE" listen-port 2>/dev/null || true)"
        WG_HUB_IP="$(ip -4 addr show "$WG_INTERFACE" | awk '/inet /{print $2}' | cut -d/ -f1 | head -1)"
        WG_NETWORK="${WG_HUB_IP%.*}.0/24"
        WG_HUB_PUBKEY="$(wg show "$WG_INTERFACE" public-key)"

        [[ -n "$WG_PORT" ]]       || die "Could not read WireGuard listen port from $WG_INTERFACE."
        [[ -n "$WG_HUB_IP" ]]     || die "Could not read WireGuard IP from $WG_INTERFACE."
        [[ -n "$WG_HUB_PUBKEY" ]] || die "Could not read WireGuard public key from $WG_INTERFACE."

        ok "WireGuard: $WG_INTERFACE ($WG_HUB_IP) / UDP $WG_PORT"
        return
    fi

    # Find a free UDP port
    WG_PORT="$(find_free_port "${ASDL_WG_PORT:-51820}")"
    ok "Using WireGuard port: UDP $WG_PORT"

    # Find a free 10.X.0.0/24 subnet
    local hub_ip subnet found=0
    for third in 100 101 102 103 104 105 150 200 201 202; do
        subnet="10.${third}.0.0/24"
        hub_ip="10.${third}.0.1"
        if ! ip addr show | grep -q "10\.${third}\."; then
            found=1
            break
        fi
    done

    # Fallback to 192.168.x range
    if [[ "$found" -eq 0 ]]; then
        for third in 200 201 202 203 204 210 220; do
            subnet="192.168.${third}.0/24"
            hub_ip="192.168.${third}.1"
            if ! ip addr show | grep -q "192\.168\.${third}\."; then
                found=1
                break
            fi
        done
    fi

    [[ "$found" -eq 1 ]] || die "Could not find a free subnet."
    ok "Using subnet: $subnet"

    WG_HUB_IP="$hub_ip"
    WG_NETWORK="$subnet"

    WG_PRIVATE_KEY="$(wg genkey)"
    WG_PUBLIC_KEY="$(echo "$WG_PRIVATE_KEY" | wg pubkey)"

    mkdir -p /etc/wireguard
    cat > "/etc/wireguard/${WG_INTERFACE}.conf" <<EOF
[Interface]
PrivateKey = $WG_PRIVATE_KEY
Address = ${hub_ip}/24
ListenPort = $WG_PORT
EOF
    chmod 600 "/etc/wireguard/${WG_INTERFACE}.conf"

    systemctl enable "wg-quick@${WG_INTERFACE}"
    systemctl start "wg-quick@${WG_INTERFACE}" || die "Failed to bring up WireGuard interface."

    WG_HUB_PUBKEY="$(wg show "$WG_INTERFACE" public-key)"

    sysctl -w net.ipv4.ip_forward=1 >/dev/null
    if grep -qE '^[[:space:]]*net\.ipv4\.ip_forward=' /etc/sysctl.conf; then
        sed -i 's/^[[:space:]]*net\.ipv4\.ip_forward=.*/net.ipv4.ip_forward=1/' /etc/sysctl.conf
    else
        echo 'net.ipv4.ip_forward=1' >> /etc/sysctl.conf
    fi

    ok "WireGuard interface created: $WG_INTERFACE ($hub_ip) / UDP $WG_PORT"
}

write_env() {
    info "Writing configuration..."
    cat > "$INSTALL_DIR/.env" <<EOF
DB_HOST=127.0.0.1
DB_PORT=5432
DB_USER=asdl
DB_PASSWORD=$DB_PASSWORD
DB_NAME=asdl_hub
DB_SSLMODE=disable

JWT_SECRET=$JWT_SECRET
ADMIN_PASSWORD=$ADMIN_PASSWORD

SERVER_PORT=$HUB_PORT
PUBLIC_URL=$HUB_URL

WG_INTERFACE=$WG_INTERFACE
WG_PORT=$WG_PORT
WG_HUB_IP=$WG_HUB_IP
WG_NETWORK=$WG_NETWORK
WG_CIDR=$WG_NETWORK
WG_START_IP=${WG_HUB_IP%.*}.2
VPN_NETWORKS=$WG_NETWORK,127.0.0.0/8,::1/128
WG_HUB_PUBKEY=$WG_HUB_PUBKEY
WG_ENDPOINT=$HUB_HOST:$WG_PORT
EOF
    chmod 600 "$INSTALL_DIR/.env"
    ok "Configuration written."
}

install_files() {
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    sleep 1

    setcap cap_net_admin+eip /usr/bin/wg 2>/dev/null || true

    mkdir -p "$INSTALL_DIR/bin"
    rm -f "$INSTALL_DIR/bin/asdl-hub"
    cp "$PROJECT_DIR/bin/asdl-hub" "$INSTALL_DIR/bin/asdl-hub"
    chmod 755 "$INSTALL_DIR/bin/asdl-hub"

    if [[ -d "$PROJECT_DIR/dashboard/out" ]]; then
        rm -rf "$INSTALL_DIR/dashboard"
        cp -a "$PROJECT_DIR/dashboard" "$INSTALL_DIR/dashboard"
    fi

    # Allow hub to manage WireGuard peers without password
    echo "asdl-hub ALL=(ALL) NOPASSWD: /usr/bin/wg, /usr/sbin/ip" \
        > /etc/sudoers.d/asdl-hub
    chmod 440 /etc/sudoers.d/asdl-hub

    chown -R "$RUN_USER:$RUN_USER" "$INSTALL_DIR"
    chmod 750 "$INSTALL_DIR"
}

setup_service() {
    info "Configuring systemd service..."
    cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=ASDL Hub
After=network-online.target postgresql.service
Wants=network-online.target postgresql.service

[Service]
Type=simple
User=$RUN_USER
Group=$RUN_USER
WorkingDirectory=$INSTALL_DIR
EnvironmentFile=$INSTALL_DIR/.env
ExecStart=$INSTALL_DIR/bin/asdl-hub
Restart=always
RestartSec=5
NoNewPrivileges=false
PrivateTmp=true
ProtectSystem=false
ProtectHome=true
AmbientCapabilities=CAP_NET_ADMIN
CapabilityBoundingSet=CAP_NET_ADMIN

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable "$SERVICE_NAME"
    ok "Systemd service configured."
}

verify() {
    info "Starting ASDL Hub..."
    systemctl restart "$SERVICE_NAME"
    sleep 3

    systemctl is-active --quiet "$SERVICE_NAME" || {
        journalctl -u "$SERVICE_NAME" -n 80 --no-pager
        die "ASDL Hub failed to start."
    }
    ok "Service is running."

    info "Running health check..."
    curl -fsS --max-time 10 "http://127.0.0.1:${HUB_PORT}/health" >/dev/null \
        || die "Hub is running but /health did not respond."
    ok "Health check passed."

    if [[ "$USE_TLS" -eq 1 ]]; then
        curl -fsS --max-time 15 "$HUB_URL/health" >/dev/null \
            || warn "Local health passed but public HTTPS check failed. Check DNS/firewall/TLS."
    else
        curl -fsS --max-time 15 "$HUB_URL/health" >/dev/null \
            || warn "Local health passed but public check failed. Check your VPS firewall."
    fi
}

summary() {
    echo
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║                 ASDL Hub is ready! 🚀                       ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo
    echo "  Dashboard:      $HUB_URL"
    echo "  Health:         $HUB_URL/health"
    echo
    echo "  Admin user:     admin"
    echo "  Admin password: $ADMIN_PASSWORD"
    echo
    echo "  WireGuard:      $WG_INTERFACE / UDP $WG_PORT"
    echo
    warn "If nodes can't connect, ensure UDP $WG_PORT and TCP 80 is open in your cloud firewall:"
    echo "    → AWS:          EC2 → Security Groups → Inbound Rules"
    echo "    → GCP:          VPC → Firewall Rules"
    echo "    → DigitalOcean: Networking → Firewalls"
    echo "    → Oracle Cloud: Security Lists / NSGs"
    echo "    → Hetzner:      Firewall in Cloud Console"
    echo
    warn "Keep the generated admin password somewhere safe."
    echo
}

main() {
    prepare_source
    install_packages
    detect_existing
    ensure_user
    configure_endpoint
    HUB_PORT="${HUB_PORT:-$(find_free_hub_port 8080)}"
    ok "Using Hub port: TCP $HUB_PORT"
    generate_secrets
    setup_database
    setup_wireguard
    write_env
    install_files
    setup_service
    setup_firewall
    setup_nginx
    verify
    summary
}

main "$@"