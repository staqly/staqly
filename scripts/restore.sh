#!/usr/bin/env bash
# Restores a backup made by scripts/backup.sh. REPLACES the current data.
# Usage: scripts/restore.sh backups/<timestamp>
set -euo pipefail
cd "$(dirname "$0")/.."
dir="${1:?usage: scripts/restore.sh backups/<timestamp>}"
VOLUME="${STAQLY_VOLUME:-staqly_staqly-data}"
[ -f "$dir/workspace.db" ] || { echo "missing $dir/workspace.db" >&2; exit 1; }
if [ "${FORCE:-}" != "1" ]; then
  read -r -p "This replaces all current Staqly data with $dir. Continue? [y/N] " ok
  [ "$ok" = "y" ] || exit 1
fi
docker compose stop api
# Remove WAL/SHM files from the old database so they are not replayed onto the restored one.
docker run --rm -v "$VOLUME:/data" -v "$PWD/$dir:/backup:ro" alpine:3.22 sh -ec '
  rm -f /data/workspace.db /data/workspace.db-wal /data/workspace.db-shm
  cp /backup/workspace.db /data/workspace.db
  if [ -f /backup/uploads.tar.gz ]; then rm -rf /data/uploads && tar xzf /backup/uploads.tar.gz -C /data; fi  # also restores notify_secret.key
  chown -R 10001:10001 /data'
docker compose start api
echo "Restored from $dir (pending migrations run automatically on startup)."
