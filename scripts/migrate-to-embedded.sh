#!/usr/bin/env bash
set -Eeuo pipefail

# Moves an installed Hub from the system's PostgreSQL to the database the Hub
# runs inside its own service (DB_EMBEDDED=true).
#
#   sudo bash migrate-to-embedded.sh
#
# What it does, in order: checks the Hub binary supports it, stops the Hub, saves a
# full SQL backup, starts the embedded database on its own, restores the backup into
# it, compares the row count of every table with the old database, and only then
# switches the Hub over and starts it. If anything fails it puts the old settings
# back and starts the Hub on the old database again. It never deletes the old
# database or uninstalls PostgreSQL: do that yourself once you are happy.

INSTALL_DIR="${INSTALL_DIR:-/opt/asdl-hub}"
SERVICE_NAME="${SERVICE_NAME:-asdl-hub}"
RUN_USER="${RUN_USER:-asdl-hub}"
EMBEDDED_PORT=54329
UNIT="/etc/systemd/system/${SERVICE_NAME}.service"
STAMP="$(date +%Y%m%d-%H%M%S)"
BACKUP_DIR="$INSTALL_DIR/backups"
BACKUP="$BACKUP_DIR/asdl_hub-$STAMP.sql.gz"
ENV_FILE="$INSTALL_DIR/.env"
ENV_SAVED="$ENV_FILE.pre-embedded-$STAMP"
UNIT_SAVED="$UNIT.pre-embedded-$STAMP"

info(){ echo "➜ $*"; }
ok(){ echo "✓ $*"; }
# After the point of no return, die() rolls back too (exit does not trigger the ERR trap)
die(){ echo "✗ $*" >&2; if (( ARMED )); then rollback 1; fi; exit 1; }

DB_PID=""
STARTED_CHANGES=0
ARMED=0

env_value() { awk -F= -v k="$1" '$1==k{sub(/^[^=]*=/,"");print;exit}' "$ENV_FILE"; }

stop_embedded() {
    if [[ -n "$DB_PID" ]] && kill -0 "$DB_PID" 2>/dev/null; then
        kill -TERM "$DB_PID" 2>/dev/null || true
        for _ in $(seq 1 60); do kill -0 "$DB_PID" 2>/dev/null || break; sleep 1; done
        kill -KILL "$DB_PID" 2>/dev/null || true
    fi
    DB_PID=""
}

rollback() {
    local code="${1:-$?}"
    trap - ERR
    ARMED=0
    echo "✗ Something failed (exit $code). Putting the old setup back." >&2
    stop_embedded
    if (( STARTED_CHANGES )); then
        [[ -f "$ENV_SAVED" ]] && cp -p "$ENV_SAVED" "$ENV_FILE"
        [[ -f "$UNIT_SAVED" ]] && cp -p "$UNIT_SAVED" "$UNIT" && systemctl daemon-reload
        rm -rf "$INSTALL_DIR/data/postgres"
    fi
    systemctl start "$SERVICE_NAME" || true
    echo "The Hub is starting on its old database again. Your backup (if it got that far) is in $BACKUP_DIR." >&2
    exit "$code"
}

[[ $EUID -eq 0 ]] || die "Run this as root: sudo bash $0"
[[ -f "$ENV_FILE" ]] || die "No $ENV_FILE: is the Hub installed in $INSTALL_DIR?"
if grep -q '^DB_EMBEDDED=true' "$ENV_FILE"; then die "This Hub already runs its own database. Nothing to do."; fi
[[ -x "$INSTALL_DIR/bin/asdl-hub" ]] || die "No Hub binary at $INSTALL_DIR/bin/asdl-hub."
grep -q DB_EMBEDDED "$INSTALL_DIR/bin/asdl-hub" || die "This Hub version cannot run its own database yet. Update first: sudo asdl-hub update install"
for tool in pg_dump psql gzip curl; do
    command -v "$tool" >/dev/null || die "'$tool' is needed (it comes with the system's PostgreSQL client)."
done

DB_NAME="$(env_value DB_NAME)"; DB_NAME="${DB_NAME:-asdl_hub}"
DB_USER="$(env_value DB_USER)"; DB_USER="${DB_USER:-asdl}"
DB_PASSWORD="$(env_value DB_PASSWORD)"
[[ -n "$DB_PASSWORD" && "$DB_PASSWORD" != dbpass123 ]] || die "DB_PASSWORD in $ENV_FILE must be a real password."
OLD_HOST="$(env_value DB_HOST)"; OLD_HOST="${OLD_HOST:-127.0.0.1}"
OLD_PORT="$(env_value DB_PORT)"; OLD_PORT="${OLD_PORT:-5432}"
HUB_PORT="$(env_value SERVER_PORT)"; HUB_PORT="${HUB_PORT:-8080}"

old_psql() { PGPASSWORD="$DB_PASSWORD" psql -h "$OLD_HOST" -p "$OLD_PORT" -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -Atq "$@"; }
new_psql() { PGPASSWORD="$DB_PASSWORD" psql -h 127.0.0.1 -p "$EMBEDDED_PORT" -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -Atq "$@"; }

old_psql -c 'select 1' >/dev/null || die "Cannot reach the current database at $OLD_HOST:$OLD_PORT."
ok "Current database reachable."

trap rollback ERR
ARMED=1

info "Stopping the Hub..."
systemctl stop "$SERVICE_NAME"

info "Saving a full backup to $BACKUP ..."
mkdir -p "$BACKUP_DIR"; chmod 700 "$BACKUP_DIR"
PGPASSWORD="$DB_PASSWORD" pg_dump -h "$OLD_HOST" -p "$OLD_PORT" -U "$DB_USER" -d "$DB_NAME" \
    --no-owner --no-privileges | gzip > "$BACKUP"
[[ -s "$BACKUP" ]] || die "The backup is empty."
gzip -t "$BACKUP" || die "The backup is corrupt."
ok "Backup saved ($(du -h "$BACKUP" | cut -f1))."

info "Switching the configuration to the embedded database..."
cp -p "$ENV_FILE" "$ENV_SAVED"
cp -p "$UNIT" "$UNIT_SAVED"
STARTED_CHANGES=1
sed -i -e '/^DB_HOST=/d' -e '/^DB_SSLMODE=/d' -e '/^DB_PORT=/d' -e '/^DB_EMBEDDED=/d' "$ENV_FILE"
printf 'DB_EMBEDDED=true\nDB_PORT=%s\n' "$EMBEDDED_PORT" >> "$ENV_FILE"
chown "$RUN_USER:$RUN_USER" "$ENV_FILE" 2>/dev/null || true

info "Starting the embedded database on its own (the first start downloads PostgreSQL)..."
mkdir -p "$INSTALL_DIR/data"; chown -R "$RUN_USER:$RUN_USER" "$INSTALL_DIR/data"
rm -rf "$INSTALL_DIR/data/postgres"
# setpriv execs the Hub directly (no wrapper process), so the stop signal reaches it and it shuts PostgreSQL down cleanly
( cd "$INSTALL_DIR" && exec env HOME="$INSTALL_DIR" setpriv --reuid="$RUN_USER" --regid="$RUN_USER" --init-groups ./bin/asdl-hub serve --db-only ) > "$INSTALL_DIR/data/db-only.log" 2>&1 &
DB_PID=$!
ready=0
for _ in $(seq 1 180); do
    if new_psql -c 'select 1' >/dev/null 2>&1; then ready=1; break; fi
    kill -0 "$DB_PID" 2>/dev/null || { tail -20 "$INSTALL_DIR/data/db-only.log" >&2; die "The embedded database exited during start."; }
    sleep 2
done
(( ready )) || { tail -20 "$INSTALL_DIR/data/db-only.log" >&2; die "The embedded database did not come up in 6 minutes."; }
ok "Embedded database is up."

info "Restoring the backup..."
gzip -dc "$BACKUP" | PGPASSWORD="$DB_PASSWORD" psql -h 127.0.0.1 -p "$EMBEDDED_PORT" -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -q -o /dev/null
ok "Restored."

info "Comparing every table's row count with the old database..."
mismatch=0
tables="$(old_psql -c "select tablename from pg_tables where schemaname='public' order by 1")"
[[ -n "$tables" ]] || die "The old database has no tables: refusing to continue."
while read -r t; do
    a="$(old_psql -c "select count(*) from \"$t\"")"
    b="$(new_psql -c "select count(*) from \"$t\"")"
    if [[ "$a" != "$b" ]]; then echo "  ✗ $t: old $a, new $b"; mismatch=1; else echo "  ✓ $t: $a"; fi
done <<< "$tables"
(( mismatch == 0 )) || die "Row counts differ: not switching."
ok "All tables match."

stop_embedded
ok "Embedded database stopped cleanly."

info "Updating the systemd service..."
sed -i -e 's/^After=network-online.target postgresql.service$/After=network-online.target/' \
       -e 's/^Wants=network-online.target postgresql.service$/Wants=network-online.target/' "$UNIT"
grep -q '^TimeoutStopSec=' "$UNIT" || sed -i 's/^RestartSec=5$/RestartSec=5\nTimeoutStopSec=60/' "$UNIT"
systemctl daemon-reload

info "Starting the Hub on the embedded database..."
systemctl start "$SERVICE_NAME"
up=0
for _ in $(seq 1 60); do
    if curl -fsS --max-time 5 "http://127.0.0.1:${HUB_PORT}/health" >/dev/null 2>&1; then up=1; break; fi
    systemctl is-active --quiet "$SERVICE_NAME" || break
    sleep 2
done
(( up )) || { journalctl -u "$SERVICE_NAME" -n 40 --no-pager >&2; die "The Hub did not come up on the embedded database."; }

trap - ERR
ARMED=0
ok "The Hub is running on its own database."
echo
echo "  Backup:        $BACKUP"
echo "  Old settings:  $ENV_SAVED and $UNIT_SAVED"
echo
echo "  The system's PostgreSQL is still installed and still holds the old data."
echo "  When you are happy (give it a few days), you can remove it:"
echo "    sudo systemctl disable --now postgresql"
echo "    sudo apt-get remove postgresql postgresql-contrib   # data is kept until you purge"
echo
echo "  To go back: copy $ENV_SAVED to $ENV_FILE, $UNIT_SAVED to $UNIT, then"
echo "    sudo systemctl daemon-reload && sudo systemctl restart $SERVICE_NAME"
