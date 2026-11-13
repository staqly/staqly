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

# ── Check the whole backup before anything is stopped or changed ─────────────

if [ ! -s "$dir/workspace.db" ]; then
  echo "missing or empty $dir/workspace.db" >&2
  exit 1
fi
# A database that is not SQLite, was cut off (the header says how many pages the file has) or fails
# SQLite's own check (when sqlite3 is installed here; on a copy, so the backup stays as it is) would
# replace a working one.
db="$dir/workspace.db"
bad_db() {
  echo "$db $1; nothing was changed" >&2
  exit 1
}

if [ "$(head -c 15 "$db" 2>/dev/null)" != "SQLite format 3" ]; then
  bad_db "is not a SQLite database"
fi

# shellcheck disable=SC2207  # sixteen numbers, split on purpose
header=($(od -An -tu1 -j16 -N16 "$db"))
page=$((header[0] * 256 + header[1]))

if [ "$page" -eq 1 ]; then
  page=65536
fi

pages=$((((header[12] * 256 + header[13]) * 256 + header[14]) * 256 + header[15]))

if [ "$(wc -c <"$db")" -lt $((page * pages)) ]; then
  bad_db "is incomplete (shorter than its header says)"
fi

if command -v sqlite3 >/dev/null 2>&1; then
  check=$(mktemp -d)
  trap 'rm -rf "$check"' EXIT
  cp "$db" "$check/workspace.db"
  result=$(sqlite3 "$check/workspace.db" 'PRAGMA integrity_check;' 2>&1) || true
  rm -rf "$check"
  trap - EXIT

  if [ "$result" != "ok" ]; then
    bad_db "fails SQLite's integrity check"
  fi
fi

if [ -f "$dir/uploads.tar.gz" ] && ! tar tzf "$dir/uploads.tar.gz" >/dev/null; then
  echo "$dir/uploads.tar.gz is damaged or incomplete; nothing was changed" >&2
  exit 1
fi
dir=$(cd -- "$dir" && pwd -P)

# ── Confirm ──────────────────────────────────────────────────────────────────

if [ "${FORCE:-}" != "1" ]; then
  read -r -p "This replaces all current staqly data with $dir. Continue? [y/N] " answer

  if [ "$answer" != "y" ]; then
    exit 1
  fi
fi

# ── Swap the data while the API is stopped ───────────────────────────────────
# A throwaway container writes into the volume:
#   - unpacks the whole backup into /data/.restore-new first: a backup that cannot be read
#     leaves the current data untouched
#   - moves the current database (with its -wal/-shm files, so they are not replayed onto
#     the restored copy), uploads and every key the backup carries (secret.key,
#     notify_secret.key, access_token) into /data/.restore-old, and the new ones into place
#   - puts the previous files back if that fails, and deletes them only after it succeeded
#   - gives everything to the API user (uid 10001), secrets as 0600
# The staging folders live in the volume (renames, not copies): it needs room for the backup.

docker stop "$container" >/dev/null

status=0
docker run --rm \
  -v "$volume:/data" \
  -v "$dir:/backup:ro" \
  alpine:3.24 sh -ec '
    umask 077
    new=/data/.restore-new
    old=/data/.restore-old

    if [ -e "$old" ]; then
      echo "$old holds the data of an earlier restore that failed: move it back or delete it first." >&2
      exit 3
    fi

    # 1. Stage. Nothing current is touched yet.
    rm -rf "$new"
    trap "rm -rf \"\$new\"" EXIT
    mkdir "$new"
    cp /backup/workspace.db "$new/workspace.db"

    if [ -f /backup/uploads.tar.gz ]; then
      tar xzf /backup/uploads.tar.gz -C "$new"
    fi

    chown -R 10001:10001 "$new"
    chmod 600 "$new/workspace.db"

    for key in secret.key notify_secret.key access_token; do
      if [ -f "$new/$key" ]; then
        chmod 600 "$new/$key"
      fi
    done

    # 2. Swap, keeping the previous files until every new one is in place.
    installed=""
    rollback() {
      code=$?
      trap - EXIT

      if [ "$code" -ne 0 ]; then
        for name in $installed; do
          rm -rf "/data/$name"
        done

        for name in workspace.db workspace.db-wal workspace.db-shm uploads secret.key notify_secret.key access_token; do
          if [ -e "$old/$name" ]; then
            mv "$old/$name" "/data/$name" || exit 3
          fi
        done
        rmdir "$old" || exit 3
      fi

      rm -rf "$new"
      exit "$code"
    }
    trap rollback EXIT
    mkdir "$old"

    for name in workspace.db-wal workspace.db-shm; do
      if [ -e "/data/$name" ]; then
        mv "/data/$name" "$old/$name"
      fi
    done

    for name in workspace.db uploads secret.key notify_secret.key access_token; do
      if [ -e "$new/$name" ]; then
        if [ -e "/data/$name" ]; then
          mv "/data/$name" "$old/$name"
        fi

        mv "$new/$name" "/data/$name"
        installed="$installed $name"
      fi
    done

    rm -rf "$old"
  ' || status=$?

# A helper that was killed ran no rollback: unless the volume shows that nothing was left aside, the
# data may be half swapped and the container must not start on it.
if [ "$status" -ne 0 ] && [ "$status" -ne 3 ] &&
  ! docker run --rm -v "$volume:/data" alpine:3.24 sh -c '[ ! -e /data/.restore-old ]'; then
  status=3
fi

if [ "$status" -eq 3 ]; then
  echo "Restore failed and the previous data is not back in place: it is in /data/.restore-old of the" >&2
  echo "volume $volume. $container is left STOPPED; move those files back before starting it." >&2
  exit 1
fi

docker start "$container" >/dev/null

if [ "$status" -ne 0 ]; then
  echo "Restore failed: the previous data is unchanged and $container is running again." >&2
  exit 1
fi

echo "Restored from $dir (pending migrations run automatically on startup)."
