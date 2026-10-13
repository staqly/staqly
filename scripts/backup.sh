#!/usr/bin/env bash
# Consistent backup of a running Staqly install into ./backups/<timestamp>/
#  - database: SQLite VACUUM INTO (safe while the server writes, WAL-aware)
#  - uploads:  tar of /data/uploads (+ notify_secret.key, which encrypts
#              notification-channel secrets stored in the database)
set -euo pipefail
cd "$(dirname "$0")/.."
CONTAINER="${STAQLY_CONTAINER:-staqly-api}"
VOLUME="${STAQLY_VOLUME:-staqly_staqly-data}"
ts=$(date -u +%Y%m%dT%H%M%SZ)
out="backups/$ts"
mkdir -p "$out"

docker exec "$CONTAINER" server backup "/data/backups/workspace-$ts.db" >/dev/null
docker cp "$CONTAINER:/data/backups/workspace-$ts.db" "$out/workspace.db"
docker exec "$CONTAINER" rm -f "/data/backups/workspace-$ts.db"
docker run --rm -v "$VOLUME:/data:ro" -v "$PWD/$out:/backup" alpine:3.22 \
  sh -c 'cd /data && tar czf /backup/uploads.tar.gz uploads $(ls notify_secret.key 2>/dev/null)' 
echo "Backup written to $out"
ls -lh "$out"
