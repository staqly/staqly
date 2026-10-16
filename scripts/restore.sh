#!/usr/bin/env bash
#
# Restore a backup made by scripts/backup.sh into the Compose install of this
# checkout. This REPLACES all current staqly data. (For the image run with
# `docker run`, see the docs: Self-hosting -> Backup & restore, Docker tab.)
#
# Usage:
#   make restore BACKUP=backups/<timestamp>
#   scripts/restore.sh backups/<timestamp>
#   FORCE=1 scripts/restore.sh backups/<timestamp>    (no confirmation prompt)
#
# Environment:
#   STAQLY_CONTAINER   container name     (default: staqly)
#   STAQLY_VOLUME      data volume name   (default: staqly_staqly-data)

set -euo pipefail
umask 077
cd "$(dirname "$0")/.."

dir="${1:?usage: scripts/restore.sh backups/<timestamp>}"
container="${STAQLY_CONTAINER:-staqly}"
volume="${STAQLY_VOLUME:-staqly_staqly-data}"

if [ ! -f "$dir/workspace.db" ]; then
  echo "missing $dir/workspace.db" >&2
  exit 1
fi

# ── Confirm ──────────────────────────────────────────────────────────────────

if [ "${FORCE:-}" != "1" ]; then
  read -r -p "This replaces all current staqly data with $dir. Continue? [y/N] " answer

  if [ "$answer" != "y" ]; then
    exit 1
  fi
fi

# ── Swap the data while the API is stopped ───────────────────────────────────
# A throwaway container writes into the volume:
#   - deletes the old database together with its -wal/-shm files, so they are
#     not replayed onto the restored copy
#   - unpacks uploads (and notify_secret.key, if the backup has it)
#   - gives everything to the API user (uid 10001), secrets as 0600

docker stop "$container" >/dev/null

docker run --rm \
  -v "$volume:/data" \
  -v "$PWD/$dir:/backup:ro" \
  alpine:3.24 sh -ec '
    umask 077

    rm -f /data/workspace.db /data/workspace.db-wal /data/workspace.db-shm
    cp /backup/workspace.db /data/workspace.db

    if [ -f /backup/uploads.tar.gz ]; then
      rm -rf /data/uploads
      tar xzf /backup/uploads.tar.gz -C /data
    fi

    chown -R 10001:10001 /data
    chmod 600 /data/workspace.db

    if [ -f /data/notify_secret.key ]; then
      chmod 600 /data/notify_secret.key
    fi
  '

docker start "$container" >/dev/null

echo "Restored from $dir (pending migrations run automatically on startup)."
