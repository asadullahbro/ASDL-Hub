#!/usr/bin/env bash
set -Eeuo pipefail

# Usage: setup-wireguard.sh [interface] [port_hint]
# Outputs to stdout (source with eval or parse):
#   WG_PORT=...
#   WG_HUB_IP=...
#   WG_NETWORK=...
#   WG_HUB_PUBKEY=...

WG_INTERFACE="${1:-asdl0}"
WG_PORT_HINT="${2:-51820}"

INSTALL_DIR="${INSTALL_DIR:-/opt/asdl-hub}"

die() { echo "✗ $*" >&2; exit 1; }
ok()  { echo "✓ $*"; }
info(){ echo "→ $*"; }

[[ $EUID -eq 0 ]] || die "Run as root."

command -v wg >/dev/null || die "WireGuard tools are not installed."
modprobe wireguard 2>/dev/null || true

# ── Reuse existing interface ────────────────────────────────────────────────
if ip link show "$WG_INTERFACE" >/dev/null 2>&1; then
    ok "Reusing existing WireGuard interface: $WG_INTERFACE"
    WG_PORT="$(grep -E '^ListenPort' /etc/wireguard/${WG_INTERFACE}.conf | awk '{print $3}')"
    WG_HUB_IP="$(grep -E '^Address' /etc/wireguard/${WG_INTERFACE}.conf | awk '{print $3}' | cut -d/ -f1)"
    WG_NETWORK="${WG_HUB_IP%.*}.0/24"
    WG_HUB_PUBKEY="$(wg show "$WG_INTERFACE" public-key)"
    ok "WireGuard ready: $WG_INTERFACE ($WG_HUB_IP) / UDP $WG_PORT"
    echo "WG_PORT=$WG_PORT"
    echo "WG_HUB_IP=$WG_HUB_IP"
    echo "WG_NETWORK=$WG_NETWORK"
    echo "WG_HUB_PUBKEY=$WG_HUB_PUBKEY"
    exit 0
fi

# ── Find a free UDP port ────────────────────────────────────────────────────
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

WG_PORT="$(find_free_port "$WG_PORT_HINT")"
ok "Using WireGuard port: UDP $WG_PORT"

# ── Find a free subnet ──────────────────────────────────────────────────────
hub_ip=""
subnet=""
found=0

for third in 100 101 102 103 104 105 150 200 201 202; do
    subnet="10.${third}.0.0/24"
    hub_ip="10.${third}.0.1"
    if ! ip addr show | grep -q "10\.${third}\."; then
        found=1
        break
    fi
done

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

# ── Generate keys and config ────────────────────────────────────────────────
info "Generating WireGuard keys..."
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

# ── Bring up interface ──────────────────────────────────────────────────────
info "Bringing up $WG_INTERFACE..."
systemctl enable "wg-quick@${WG_INTERFACE}"
systemctl start "wg-quick@${WG_INTERFACE}" || die "Failed to bring up WireGuard interface."

WG_HUB_PUBKEY="$(wg show "$WG_INTERFACE" public-key)"

# ── IP forwarding ───────────────────────────────────────────────────────────
sysctl -w net.ipv4.ip_forward=1 >/dev/null
if grep -qE '^[[:space:]]*net\.ipv4\.ip_forward=' /etc/sysctl.conf; then
    sed -i 's/^[[:space:]]*net\.ipv4\.ip_forward=.*/net.ipv4.ip_forward=1/' /etc/sysctl.conf
else
    echo 'net.ipv4.ip_forward=1' >> /etc/sysctl.conf
fi

ok "WireGuard interface created: $WG_INTERFACE ($hub_ip) / UDP $WG_PORT"

# ── Output vars for caller to consume ──────────────────────────────────────
echo "WG_PORT=$WG_PORT"
echo "WG_HUB_IP=$WG_HUB_IP"
echo "WG_NETWORK=$WG_NETWORK"
echo "WG_HUB_PUBKEY=$WG_HUB_PUBKEY"