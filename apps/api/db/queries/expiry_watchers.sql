-- name: SetItemExpiry :exec
UPDATE items SET expires_at = ? WHERE id = ?;

-- name: ExpiredItems :many
SELECT workspace_id, id FROM items WHERE expires_at IS NOT NULL AND expires_at <= ? AND status = 'pending' LIMIT 100;

-- name: ClearItemExpiry :exec
UPDATE items SET expires_at = NULL WHERE id = ?;

-- name: CreateWatcher :exec
INSERT INTO watchers (id, workspace_id, collection_id, name, kind, url, keywords, exclude_keywords, every_seconds, next_run_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetWatcher :one
SELECT * FROM watchers WHERE workspace_id = ? AND id = ?;

-- name: GetWatcherByID :one
SELECT * FROM watchers WHERE id = ?;

-- name: ListWatchers :many
SELECT * FROM watchers WHERE workspace_id = ? ORDER BY created_at;

-- name: UpdateWatcher :exec
UPDATE watchers SET collection_id = ?, name = ?, url = ?, keywords = ?, exclude_keywords = ?, every_seconds = ?,
    status = ?, fail_count = ?, degraded_since = ?, next_run_at = ?, updated_at = ?
WHERE workspace_id = ? AND id = ?;

-- name: DeleteWatcher :execrows
DELETE FROM watchers WHERE workspace_id = ? AND id = ?;

-- name: CountActiveWatchers :one
SELECT COUNT(*) FROM watchers WHERE workspace_id = ? AND status <> 'paused';

-- name: DueWatchers :many
SELECT * FROM watchers WHERE status IN ('active', 'degraded') AND next_run_at <= ? ORDER BY next_run_at LIMIT ?;

-- name: LeaseWatcher :execrows
UPDATE watchers SET next_run_at = @lease_until WHERE id = @id AND next_run_at = @expected;

-- name: FinishWatcherRun :exec
UPDATE watchers SET status = ?, baseline_done = ?, content_hash = ?, fail_count = ?, degraded_since = ?,
    last_error = ?, last_run_at = ?, next_run_at = ?, items_created = items_created + ?, updated_at = ?
WHERE id = ?;

-- name: MarkSeen :execrows
INSERT INTO watcher_seen (watcher_id, entry_hash, seen_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING;

-- name: PruneSeen :exec
DELETE FROM watcher_seen WHERE seen_at < ?;

-- name: StaleProcessingItems :many
-- Items processing since before the cutoff (the shortest possible timeout), with their workspace's
-- timeout; the caller applies each workspace's own limit. updated_at is when they entered processing,
-- unless edited since.
SELECT i.workspace_id, i.id, i.updated_at, i.processing_timeout_seconds AS item_timeout_seconds, w.processing_timeout_seconds
FROM items i JOIN workspaces w ON w.id = i.workspace_id
WHERE i.status = 'processing' AND i.updated_at <= ?
ORDER BY i.updated_at LIMIT 500;

-- name: WatcherCollections :many
SELECT collection_id FROM watcher_collections WHERE watcher_id = ? ORDER BY rowid;

-- name: ClearWatcherCollections :exec
DELETE FROM watcher_collections WHERE watcher_id = ?;

-- name: AddWatcherCollection :exec
INSERT INTO watcher_collections (watcher_id, collection_id) VALUES (?, ?);

-- name: RepointWatchers :exec
-- Before a collection is deleted: watchers filed under it that add to other collections too move to the next
-- one instead of being deleted with it.
UPDATE watchers SET collection_id = (
    SELECT wc.collection_id FROM watcher_collections wc
    WHERE wc.watcher_id = watchers.id AND wc.collection_id <> @collection_id ORDER BY wc.rowid LIMIT 1)
WHERE workspace_id = @workspace_id AND collection_id = @collection_id AND EXISTS (
    SELECT 1 FROM watcher_collections wc WHERE wc.watcher_id = watchers.id AND wc.collection_id <> @collection_id);
