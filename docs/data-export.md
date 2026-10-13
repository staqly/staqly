# Data export, retention and deletion

How to get data out of staq.ly, how long it is kept, and how to delete it.

## Export the activity log

```
GET /v1/events/export?format=ndjson|csv&from=&to=&type=&type_prefix=&actor_type=&actor_id=&collection_id=&item_id=
```

* Scope: `items:read`. The dashboard session works too.
* Events come oldest first. The response is streamed in chunks, so any size of
  log can be exported without paging.
* `from` is inclusive and `to` is exclusive. Both take RFC 3339 or
  `YYYY-MM-DD`. `type` is an exact match. `type_prefix` is a literal prefix,
  e.g. `item.` for every item event.
* **NDJSON** has one event per line, in the same shape as `GET /v1/events`.
* **CSV** columns are
  `created_at,id,type,actor_type,actor_id,actor_name,collection_id,item_id,data`.
  `data` holds the event's JSON. Quoting follows RFC 4180 with CRLF line
  endings. A cell starting with `=`, `+`, `-`, `@`, tab or CR gets a leading
  `'`, so a spreadsheet won't run it as a formula.

```bash
curl -H "Authorization: Bearer $KEY" \
  "http://localhost:3333/v1/events/export?format=csv&type_prefix=item.&from=2026-01-01" -o events.csv
```

## Export the whole workspace (GDPR data portability)

1. `POST /v1/workspace/export` (scope `admin`; dashboard sessions and local
   mode have it) returns `202` with a job `{id, status: "pending", ...}`. Only
   one export per workspace can be pending or running at a time. A second
   request gets `409` with `details.export_id`.
2. `GET /v1/workspace/exports/{id}` reports the status: `pending`, `running`,
   `ready`, `failed` or `expired`. Once it is `ready`, it also has
   `download_url`.
3. `GET /v1/workspace/exports/{id}/download` returns the ZIP.

A background worker builds the archive in `DATA_DIR/exports` (default
`./data/exports`). The archive is deleted 24 hours after it is finished; after
that the download returns `410`. A server restart re-queues interrupted jobs.
`workspace.export_requested` and `workspace.exported` events are recorded.

ZIP contents:

| File | Content |
|---|---|
| `workspace.json` | Workspace, export format version, export time, record counts |
| `collections.ndjson` | All collections, including archived ones |
| `items.ndjson` | All items. Each one has `versions` (its full edit history, oldest first) and `media_file` (path of its file in the archive) |
| `events.ndjson` | The activity log, oldest first |
| `media.ndjson` | Every upload: id, type, size, SHA-256 and `file` (path in the archive) |
| `media/` | Uploaded files under their original names. Duplicate names become `name (2).ext` |
| `webhooks.json` | Webhook endpoints. Signing secrets are replaced by `[redacted]`; `secret` fields inside events are redacted too |
| `rules.json`, `watchers.json` | Approval rules and watchers |
| `README.txt` | A description of the files |

API keys, sessions and the recovery key are never exported. The archive is
built from live data page by page, not inside one transaction. Rows changed
while it is being built may appear before or after the change. For a
point-in-time copy of the whole database, use `make backup`
(see [backup.md](backup.md)).

## Retention

A daily worker deletes old data. The first run is one minute after start, then
every 24 hours. Set the limits with environment variables:

| Variable | Default | Deletes |
|---|---|---|
| `RETENTION_DELIVERIES_DAYS` | `30` | Webhook deliveries that `succeeded` or `failed` more than N days ago. Pending deliveries are never deleted. `0` keeps them forever |
| `RETENTION_ARCHIVED_ITEMS_DAYS` | `0` (never) | Items with status `archived` that have not changed for N days. Their versions go with them; their events stay as history. One `retention.items_pruned` event per batch of up to 1000 items lists the `item_ids` |
| `RETENTION_EVENTS_DAYS` | `0` (keep forever) | Events older than N days. One `retention.events_pruned` event per workspace records the count |

Notes:

* Events can't be changed: a database trigger blocks `UPDATE`. Retention only
  deletes whole events, which the trigger allows. Finished deliveries of an
  event are deleted first. An event with a delivery that is still pending is
  kept until the delivery finishes, so webhooks stay at-least-once.
  Notification outbox rows for pruned events are deleted with them.
* Retention removes item records but not their media. Delete media with
  `DELETE /v1/media/{id}`, or purge the item (below).
* Idempotency keys expire after 24 hours on their own.
* Bookkeeping rows of expired or failed exports are deleted after 7 days.

## Deleting data

### Purge one item: `POST /v1/items/{id}/purge` (admin)

`DELETE /v1/items/{id}` removes the item, but its events stay in the activity
log. Those events hold snapshots of the item, so personal data in it stays
there too. Purge also removes that history:

* the item and its versions
* every event about the item, and those events' webhook deliveries, **even
  pending ones** (receivers won't get queued notifications about it)
* its media file, unless another item uses it
* cached idempotent responses that contain the item
* finished workspace exports, which are expired immediately

A single `item.purged` event is recorded. It holds only the IDs and counts, no
content.

**Trade-off:** events can't be edited, so personal data can't be scrubbed from
them. Purge deletes them instead, and the activity log loses that item's
history. Data that other systems already received (webhook receivers, earlier
downloads) is out of staq.ly's reach. Events that mention the item without
setting `item_id` (e.g. the IDs listed in `retention.items_pruned`) are kept.

### Delete the workspace: `DELETE /v1/workspace` (cloud)

```json
{ "confirm": "<exact workspace name>" }
```

This permanently deletes the workspace and everything in it: collections,
items, versions, media files on disk, events, webhooks and deliveries, rules,
watchers, notification channels, API keys, sessions and exports. Every session
stops working and the response clears the cookie.

* Only a signed-in dashboard session can do it. API keys get `403`, even with
  `admin`, so a leaked automation key can't destroy a workspace.
* A wrong or missing `confirm` returns `422`.
* Other workspaces are not affected.
* Database backups made earlier still contain the data. Rotate them according
  to your backup policy.

**Local mode** returns `409`. The installation has exactly one workspace, so
delete the data instead. Stop the server and remove its data volume
(`docker compose down -v`) or the `DATA_DIR` folder.
