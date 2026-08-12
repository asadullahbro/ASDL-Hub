#!/usr/bin/env bash
set -Eeuo pipefail

WG_PORT="${1:-51820}"
USE_TLS="${2:-0}"

# Never enable a firewall that was previously disabled.
# We only add rules when UFW is already active.
if ! command -v ufw >/dev/null 2>&1; then
    echo "→ UFW not installed. Skipping local firewall changes."
    echo "  Make sure your VPS/provider firewall allows TCP 80/443 and UDP $WG_PORT."
    exit 0
fi

if ! ufw status | grep -q '^Status: active'; then
    echo "→ UFW is inactive. Leaving it disabled."
    echo "  Make sure your VPS/provider firewall allows:"
    echo "    TCP 80"
    [[ "$USE_TLS" -eq 1 ]] && echo "    TCP 443"
    echo "    UDP $WG_PORT"
    exit 0
fi

echo "→ Configuring UFW..."

ufw allow 80/tcp >/dev/null

if [[ "$USE_TLS" -eq 1 ]]; then
    ufw allow 443/tcp >/dev/null
fi

ufw allow "${WG_PORT}/udp" >/dev/null

echo "✓ Firewall configured."
