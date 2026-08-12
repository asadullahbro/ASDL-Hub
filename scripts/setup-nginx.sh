#!/usr/bin/env bash
set -Eeuo pipefail

HUB_HOST="${1:?host required}"
HUB_PORT="${2:?port required}"
USE_TLS="${3:-0}"
HUB_DOMAIN="${4:-}"
HUB_URL="${5:?url required}"

CONFIG="/etc/nginx/sites-available/asdl-hub"
ENABLED="/etc/nginx/sites-enabled/asdl-hub"

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

echo "✓ Nginx configured."

if [[ "$USE_TLS" -eq 1 && -n "$HUB_DOMAIN" ]]; then
    echo "→ Requesting Let's Encrypt certificate..."

    if ! command -v certbot >/dev/null 2>&1; then
        apt-get update -qq
        apt-get install -y certbot python3-certbot-nginx
    fi

    # Certificate acquisition is intentionally non-fatal. The Hub can still
    # run over HTTP if DNS is not ready yet.
    if certbot --nginx \
        --non-interactive \
        --agree-tos \
        --register-unsafely-without-email \
        --redirect \
        -d "$HUB_DOMAIN"; then
        echo "✓ HTTPS configured."
    else
        echo "⚠ HTTPS could not be configured yet."
        echo "  Make sure DNS points $HUB_DOMAIN to this server."
        echo "  The Hub is still available at: http://$HUB_DOMAIN"
    fi
fi
