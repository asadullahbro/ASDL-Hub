#!/usr/bin/env bash
set -Eeuo pipefail

PROJECT_DIR="${1:?project directory required}"
INSTALL_DIR="${2:?install directory required}"

BIN_DIR="$INSTALL_DIR/bin"
BINARY="$BIN_DIR/asdl-hub"

cd "$PROJECT_DIR"

[[ -f go.mod ]] || { echo "go.mod not found." >&2; exit 1; }

mkdir -p "$BIN_DIR"

echo "→ Downloading Go dependencies..."
go mod download

if [[ -d "$PROJECT_DIR/dashboard" && -f "$PROJECT_DIR/dashboard/package.json" ]]; then
    echo "→ Building dashboard..."
    cd "$PROJECT_DIR/dashboard"

    if [[ -f package-lock.json ]]; then
        npm ci --silent --no-fund --no-audit
    else
        npm install --silent --no-fund --no-audit
    fi

    npm run build
    cd "$PROJECT_DIR"
fi

echo "→ Building Go binary..."

if [[ -f "$PROJECT_DIR/cmd/hub/main.go" ]]; then
    go build -trimpath -o "$BINARY" ./cmd/hub
elif [[ -f "$PROJECT_DIR/main.go" ]]; then
    go build -trimpath -o "$BINARY" .
else
    echo "Could not find Hub entrypoint." >&2
    exit 1
fi

chmod 755 "$BINARY"

# Prefer a version command if your binary implements one.
"$BINARY" --version >/dev/null 2>&1 || true

echo "✓ Build complete: $BINARY"
