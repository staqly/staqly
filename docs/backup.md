# Backup, restore and upgrades

All state lives in the `staqly_staqly-data` Docker volume:

```
/data/workspace.db        SQLite database (WAL mode; -wal/-shm side files while running)
/data/uploads/            media files
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
3. `uploads/` is archived to `./backups/<ts>/uploads.tar.gz`.

Media files are written before the database row that references them and are
never modified afterwards, so taking the DB snapshot first and the uploads
archive second never yields a row pointing at a missing file. (An upload that
lands between the two steps is an orphan file in the archive — harmless.)

Copy `./backups` somewhere off the machine.

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
docker exec staqly-api server backup /data/backups/manual.db
docker cp staqly-api:/data/backups/manual.db ./workspace.db
docker run --rm -v staqly_staqly-data:/data:ro -v "$PWD:/b" alpine tar czf /b/uploads.tar.gz -C /data uploads
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
