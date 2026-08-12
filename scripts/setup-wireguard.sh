#!/usr/bin/env bash
set -Eeuo pipefail

# Kept as a standalone helper for manual troubleshooting and future upgrades.
# The main installer currently performs the same checks inline.

WG_INTERFACE="${1:-asdl0}"
WG_PORT="${2:-51820}"

command -v wg >/dev/null || {
    echo "WireGuard tools are not installed."
    exit 1
}

if ip link show "$WG_INTERFACE" >/dev/null 2>&1; then
    echo "WireGuard interface exists: $WG_INTERFACE"
else
    echo "WireGuard interface name is available: $WG_INTERFACE"
fi

if ss -lunH | awk '{print $5}' | grep -qE "(:|\.)${WG_PORT}$"; then
    echo "UDP port $WG_PORT is already occupied." >&2
    exit 1
fi

echo "WireGuard checks passed."
