#!/usr/bin/env bash
set -Eeuo pipefail

# ASDL Hub installer
# Usage:
#   curl -fsSL https://get.asdl.website/asdl-hub | sudo bash

APP_NAME="ASDL Hub"
INSTALL_DIR="/opt/asdl-hub"
SERVICE_NAME="asdl-hub"
RUN_USER="asdl-hub"
WG_INTERFACE="asdl0"
WG_PORT="${ASDL_WG_PORT:-51820}"
HUB_PORT="${ASDL_HUB_PORT:-8080}"
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

    info "Fetching latest version..."
    version="$(curl -fsSL --max-time 10 \
        https://api.github.com/repos/asadullahbro/ASDL-Hub/releases/latest \
        | grep '"tag_name"' | head -1 | cut -d'"' -f4)"
    [[ -n "$version" ]] || die "Could not determine latest version."
    ok "Latest version: $version"

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

setup_wireguard() {
    info "Preparing WireGuard..."

    command -v wg >/dev/null || die "WireGuard tools are unavailable."
    wg show >/dev/null 2>&1 || modprobe wireguard 2>/dev/null || true

    if ip link show "$WG_INTERFACE" >/dev/null 2>&1; then
        if [[ -f "/etc/wireguard/${WG_INTERFACE}.conf" ]]; then
            ok "Reusing existing WireGuard interface: $WG_INTERFACE"
        else
            local i=1
            while ip link show "asdl$i" >/dev/null 2>&1; do
                i=$((i + 1))
            done
            WG_INTERFACE="asdl$i"
            warn "asdl0 is already in use. Automatically selected $WG_INTERFACE."
        fi
    else
        ok "WireGuard interface name available: $WG_INTERFACE"
    fi

    if ss -lunH | awk '{print $5}' | grep -qE "(:|\.)${WG_PORT}$"; then
        die "UDP port $WG_PORT is already in use. Set ASDL_WG_PORT to another port and retry."
    fi

    sysctl -w net.ipv4.ip_forward=1 >/dev/null
    if grep -qE '^[[:space:]]*net\.ipv4\.ip_forward=' /etc/sysctl.conf; then
        sed -i 's/^[[:space:]]*net\.ipv4\.ip_forward=.*/net.ipv4.ip_forward=1/' /etc/sysctl.conf
    else
        echo 'net.ipv4.ip_forward=1' >> /etc/sysctl.conf
    fi

    ok "WireGuard prepared: $WG_INTERFACE / UDP $WG_PORT"
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
WG_NETWORK=10.100.0.0/24
VPN_NETWORKS=10.100.0.0/24,127.0.0.0/8,::1/128
EOF
    chmod 600 "$INSTALL_DIR/.env"
    ok "Configuration written."
}

install_files() {
    mkdir -p "$INSTALL_DIR/bin"
    cp "$PROJECT_DIR/bin/asdl-hub" "$INSTALL_DIR/bin/asdl-hub"
    chmod 755 "$INSTALL_DIR/bin/asdl-hub"

    if [[ -d "$PROJECT_DIR/dashboard/out" ]]; then
        rm -rf "$INSTALL_DIR/dashboard"
        cp -a "$PROJECT_DIR/dashboard" "$INSTALL_DIR/dashboard"
    fi

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
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true

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
    echo "  Logs:"
    echo "    journalctl -u $SERVICE_NAME -f"
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