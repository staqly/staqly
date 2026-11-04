#!/usr/bin/env bash
#
# Back up a stopped staqly install started with Compose from this checkout
# (`make dev`, `make up`). Running the image with `docker run`
# instead? The same steps as plain docker commands are in the docs
# (Self-hosting -> Backup & restore, Docker tab); no checkout needed.
#
# Usage:
#   make backup              (or: scripts/backup.sh)
# Stop the container first; start it again after the backup completes.
#
# Output: backups/<UTC timestamp>/
#   workspace.db      consistent copy of the SQLite database
#   uploads.tar.gz    uploaded media + secret.key + notify_secret.key
#
# Backups contain secrets (webhook signing secrets, secret.key, which unseals
# channel credentials and developer app secrets, notify_secret.key, which
# decrypts alert secrets, and hashed credentials), so all of it is owner-only:
# directories 0700, files 0600. Without secret.key a restored install cannot
# read its channels' credentials (unless STAQLY_SECRET_KEY holds the key).
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

# A database-only VACUUM INTO can run online, but a later uploads archive can
# miss a file permanently deleted after that snapshot. Keep all writers stopped
# through both copies, including workers in another container on this volume.
if [ "$(docker inspect --format '{{.State.Running}}' "$container")" != "false" ]; then
  echo "Stop $container before a full backup: docker stop -t 30 $container. Start it again after the backup." >&2
  exit 1
fi
if [ -n "$(docker ps --filter "volume=$volume" --format '{{.ID}}')" ]; then
  echo "Stop all containers using $volume before a full backup." >&2
  exit 1
fi

mkdir -p backups
chmod 700 backups
mkdir "$out"
chmod 700 "$out"

# ── Database, uploads and keys (and local mode's access token) ──────────────────
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

    # A forced stop can leave committed changes in WAL. Refuse a partial copy:
    # restart staqly, let it recover, then stop it cleanly before trying again.
    if [ -s workspace.db-wal ]; then
      echo "Nonempty SQLite WAL: start staqly and stop it cleanly before a full backup." >&2
      exit 1
    fi
    cp workspace.db /backup/workspace.db

    files="uploads"
    for key in secret.key notify_secret.key access_token; do
      if [ -f "$key" ]; then
        files="$files $key"
      fi
    done

    # Thumbnails (uploads/workspaces/*/thumbs) are a cache the API rebuilds on demand.
    tar czf /backup/uploads.tar.gz --exclude='uploads/workspaces/*/thumbs' $files
    chown "$HOST_UID:$HOST_GID" /backup/workspace.db /backup/uploads.tar.gz
    chmod 600 /backup/workspace.db /backup/uploads.tar.gz
  '

echo "Backup written to $out"
echo "Start $container again when ready: docker start $container"
ls -lh "$out"
