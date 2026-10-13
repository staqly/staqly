# Backup, restore and upgrades

All state lives in the `staqly_staqly-data` Docker volume:

```
/data/workspace.db        SQLite database (WAL mode; -wal/-shm side files while running)
/data/uploads/            media files
/data/notify_secret.key   key for notification-channel secrets (unless NOTIFY_SECRET_KEY is set)
```

Never back up by copying `workspace.db` from a running server: under WAL the
latest commits may live only in `workspace.db-wal`, and a file copy taken
mid-write can be torn.

## Backup (online, consistent)

```bash
make backup            # = scripts/backup.sh
```

1. `docker exec staqly-api server backup /data/backups/workspace-<ts>.db`
   runs SQLite `VACUUM INTO`, producing a transactionally consistent,
   compacted copy while the server keeps serving requests.
2. The copy is moved to `./backups/<ts>/workspace.db`.
3. `uploads/` (and `notify_secret.key`, if present) is archived to
   `./backups/<ts>/uploads.tar.gz`.

Media files are written before the database row that references them and are
never modified afterwards, so taking the DB snapshot first and the uploads
archive second never yields a row pointing at a missing file. (An upload that
lands between the two steps is an orphan file in the archive — harmless.)

**Backups contain secrets.** The database holds webhook signing secrets (in
plain text, they are needed to sign), hashed API keys and sessions, and
encrypted notification-channel secrets; `uploads.tar.gz` includes
`notify_secret.key`, which decrypts them. The scripts run with `umask 077`:
`./backups` and each backup directory are `0700`, `workspace.db` and
`uploads.tar.gz` are `0600`, and a restore writes `workspace.db` and
`notify_secret.key` back as `0600`. Keep it that way: store backups encrypted,
and don't commit them or put them on shared drives.

Copy `./backups` somewhere off the machine (encrypted).

## Restore

```bash
make restore BACKUP=backups/20261013T081329Z     # asks for confirmation; FORCE=1 to skip
```

The script stops the API, deletes `workspace.db`, `-wal` and `-shm` (stale WAL
files must not be replayed onto the restored DB), copies the backup in,
replaces `uploads/`, fixes ownership and starts the API. Sessions and API keys
are restored as they were at backup time.

Tested: items created after a backup disappear after restore, and media from
the backup is served again.

## Manual equivalent (no make)

```bash
umask 077
docker exec staqly-api server backup /data/backups/manual.db
docker cp staqly-api:/data/backups/manual.db ./workspace.db
docker run --rm -v staqly_staqly-data:/data:ro -v "$PWD:/b" alpine \
  sh -c 'cd /data && tar czf /b/uploads.tar.gz uploads $(ls notify_secret.key 2>/dev/null)'
chmod 600 workspace.db uploads.tar.gz
```

Running the API without Docker: `go run ./cmd/server backup ./backup.db`
(uses `DATA_DIR`/`DATABASE_PATH` like the server).

## Upgrades

`git pull && docker compose up -d --build`. Data lives in the named volume, so
rebuilding or recreating containers keeps it. Migrations (goose, embedded in
the binary) run automatically at startup, each in its own transaction; if a
startup is interrupted the next start resumes from the last applied version.
Take a backup before upgrading.

`docker compose down -v` **deletes the volume** — don't.
