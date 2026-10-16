#!/usr/bin/env bash
#
# Back up a running staqly install started with Compose from this checkout
# (`make dev`, `make up`, `make cloud`). Running the image with `docker run`
# instead? The same steps as plain docker commands are in the docs
# (Self-hosting -> Backup & restore, Docker tab); no checkout needed.
#
# Usage:
#   make backup              (or: scripts/backup.sh)
#
# Output: backups/<UTC timestamp>/
#   workspace.db      consistent copy of the SQLite database
#   uploads.tar.gz    uploaded media + notify_secret.key
#
# Backups contain secrets (webhook signing secrets, notify_secret.key, which
# decrypts notification-channel secrets, and hashed credentials), so all of it
# is owner-only: directories 0700, files 0600.
#
# Environment:
#   STAQLY_CONTAINER   container name       (default: staqly)
#   STAQLY_VOLUME      data volume name     (default: staqly_staqly-data)

set -euo pipefail
umask 077
cd "$(dirname "$0")/.."

container="${STAQLY_CONTAINER:-staqly}"
volume="${STAQLY_VOLUME:-staqly_staqly-data}"
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
out="backups/$timestamp"

mkdir -p "$out"
chmod 700 backups "$out"

# ── 1. Database ──────────────────────────────────────────────────────────────
# `server backup` uses SQLite's VACUUM INTO: a consistent snapshot taken while
# the server keeps writing. It is written inside the container, copied out,
# then removed from the volume.

snapshot="/data/backups/workspace-$timestamp.db"

docker exec "$container" server backup "$snapshot" >/dev/null
docker cp "$container:$snapshot" "$out/workspace.db"
docker exec "$container" rm -f "$snapshot"
chmod 600 "$out/workspace.db"

# ── 2. Uploads and the notification key ──────────────────────────────────────
# A throwaway container mounts the volume read-only. It runs as root so it can
# read every file, then hands the archive back to you (uid/gid) as 0600.

docker run --rm \
  -e HOST_UID="$(id -u)" \
  -e HOST_GID="$(id -g)" \
  -v "$volume:/data:ro" \
  -v "$PWD/$out:/backup" \
  alpine:3.24 sh -ec '
    umask 077
    cd /data

    files="uploads"
    if [ -f notify_secret.key ]; then
      files="$files notify_secret.key"
    fi

    # uploads/thumbs is a cache the API rebuilds on demand.
    tar czf /backup/uploads.tar.gz --exclude=uploads/thumbs $files
    chown "$HOST_UID:$HOST_GID" /backup/uploads.tar.gz
    chmod 600 /backup/uploads.tar.gz
  '

echo "Backup written to $out"
ls -lh "$out"
