-- name: CreateRule :exec
INSERT INTO rules (id, workspace_id, name, priority, enabled, match, action, params, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetRule :one
SELECT * FROM rules WHERE workspace_id = ? AND id = ?;

-- name: ListRules :many
SELECT * FROM rules WHERE workspace_id = ? ORDER BY priority, created_at;

-- name: ListEnabledRules :many
SELECT * FROM rules WHERE workspace_id = ? AND enabled = 1 ORDER BY priority, created_at;

-- name: UpdateRule :exec
UPDATE rules SET name = ?, priority = ?, enabled = ?, match = ?, action = ?, params = ?, updated_at = ?
WHERE workspace_id = ? AND id = ?;

-- name: DeleteRule :execrows
DELETE FROM rules WHERE workspace_id = ? AND id = ?;

-- name: CountRules :one
SELECT COUNT(*) FROM rules WHERE workspace_id = ?;

-- name: BumpRule :exec
UPDATE rules SET times_applied = times_applied + 1 WHERE id = ?;

-- name: SetItemExpiry :exec
UPDATE items SET expires_at = ? WHERE id = ?;

-- name: ExpiredItems :many
SELECT workspace_id, id FROM items WHERE expires_at IS NOT NULL AND expires_at <= ? AND status = 'pending' LIMIT 100;

-- name: ClearItemExpiry :exec
UPDATE items SET expires_at = NULL WHERE id = ?;

-- name: CreateWatcher :exec
INSERT INTO watchers (id, workspace_id, collection_id, name, kind, url, keywords, exclude_keywords, every_seconds, preset_id, next_run_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

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
