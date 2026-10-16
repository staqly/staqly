# Backup, restore and upgrades

All state lives in one data directory: `~/.local/share/staqly` with the binary
(or `--data`), the `staqly-data` volume at `/data` with the Docker image
(`staqly_staqly-data` with the Compose setup of a checkout):

```
workspace.db        SQLite database (WAL mode; -wal/-shm side files while running)
uploads/            media files
notify_secret.key   key for notification-channel secrets (created with the first channel, unless NOTIFY_SECRET_KEY is set)
```

Never back up by copying `workspace.db` from a running server: under WAL the
latest commits may live only in `workspace.db-wal`, and a file copy taken
mid-write can be torn. The backup command runs SQLite `VACUUM INTO`, a
transactionally consistent, compacted copy taken while the server keeps
serving requests. It refuses to overwrite an existing file.

Media files are written before the database row that references them and are
never modified afterwards, so taking the DB snapshot first and the uploads
archive second never yields a row pointing at a missing file. (An upload that
lands between the two steps is an orphan file in the archive — harmless.)

**Backups contain secrets.** The database holds webhook signing secrets (in
plain text, they are needed to sign), hashed API keys and sessions, and
encrypted notification-channel secrets; `notify_secret.key` decrypts them. Run
the commands with `umask 077`, store backups encrypted, and don't commit them
or put them on shared drives.

## Binary

```bash
umask 077
staqly serve backup ~/staqly-backups/workspace.db       # works while staqly runs
cd ~/.local/share/staqly
tar czf ~/staqly-backups/uploads.tar.gz --exclude=uploads/thumbs uploads $(ls notify_secret.key 2>/dev/null)
```

Restore: stop staqly (`staqly service uninstall` if it runs in the background),
then in the data directory delete `workspace.db`, `-wal` and `-shm`, copy the
backup to `workspace.db`, replace `uploads/` from the archive and start
`staqly serve` again.

## Docker image (`docker run`)

For the container `staqly` from the docs' `docker run` command; no checkout
needed. Back up:

```bash
umask 077
docker exec staqly server backup /data/backups/staqly-backup.db
docker cp staqly:/data/backups/staqly-backup.db ./staqly-backup.db
docker exec staqly rm /data/backups/staqly-backup.db
docker run --rm --volumes-from staqly:ro -v "$PWD":/out alpine \
  tar czf /out/staqly-uploads.tar.gz -C /data uploads
docker cp staqly:/data/notify_secret.key .   # only if it exists (notification channels without NOTIFY_SECRET_KEY)
```

Restore (from the folder with both files):

```bash
docker stop staqly
docker run --rm --volumes-from staqly -v "$PWD":/backup:ro alpine sh -ec '
  rm -f /data/workspace.db /data/workspace.db-wal /data/workspace.db-shm
  cp /backup/staqly-backup.db /data/workspace.db
  rm -rf /data/uploads
  tar xzf /backup/staqly-uploads.tar.gz -C /data
  chown -R 10001:10001 /data
  chmod 600 /data/workspace.db'
docker start staqly
```

Deleting `-wal` and `-shm` matters: stale WAL files must not be replayed onto
the restored DB. Tested: items created after a backup disappear after restore,
and media from the backup is served again.

## From a checkout (Compose)

For developing staqly with Compose (and cloud servers running `make cloud`):

```bash
make backup                                      # = scripts/backup.sh → backups/<ts>/{workspace.db,uploads.tar.gz}
make restore BACKUP=backups/20261013T081329Z     # = scripts/restore.sh; asks for confirmation, FORCE=1 to skip
```

The scripts run the Docker steps above against the Compose volume
(`STAQLY_CONTAINER`, default `staqly`; `STAQLY_VOLUME`, default
`staqly_staqly-data`). `./backups` and each backup directory are `0700`, the
files `0600`; the uploads archive skips the `uploads/thumbs` cache and includes
`notify_secret.key` when present.

## Upgrades

Take a backup first. Migrations (goose, embedded in the binary) run
automatically at startup, each in its own transaction; if a startup is
interrupted the next start resumes from the last applied version.

* Binary: run the install script again (`STAQLY_VERSION=v0.2.0` for a
  specific release), then restart `staqly serve` or run
  `staqly service install` again.
* Docker image: `docker pull ghcr.io/staqly/staqly:latest`, `docker rm -f staqly`,
  then the same `docker run` command. The data stays in the volume;
  `docker volume rm staqly-data` deletes it.
* Checkout: update it and run `make dev` (or `make cloud` on a cloud server).
  `docker compose down -v` **deletes the volume** — don't.
