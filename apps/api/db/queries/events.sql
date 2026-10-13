-- name: InsertEvent :one
INSERT INTO events (id, workspace_id, type, collection_id, item_id, actor_type, actor_id, actor_name, data, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListEvents :many
SELECT * FROM events
WHERE workspace_id = @workspace_id
  AND (sqlc.narg(item_id) IS NULL OR item_id = sqlc.narg(item_id))
  AND (sqlc.narg(collection_id) IS NULL OR collection_id = sqlc.narg(collection_id))
  AND (sqlc.narg(type) IS NULL OR type = sqlc.narg(type))
  AND (sqlc.narg(actor_type) IS NULL OR actor_type = sqlc.narg(actor_type))
  AND (sqlc.narg(before_seq) IS NULL OR seq < sqlc.narg(before_seq))
  -- Credential/session audit events are visible to admin principals only.
  AND (CAST(@include_audit AS INTEGER) = 1 OR NOT (type GLOB 'api_key.*' OR type GLOB 'session.*'
       OR type GLOB 'recovery_key.*' OR type GLOB 'device.*' OR type GLOB 'device_link.*'))
ORDER BY seq DESC
LIMIT @lim;

-- name: GetEvent :one
SELECT * FROM events WHERE id = ?;

-- name: ListItemEvents :many
-- Item history, newest first (keyset on seq; uses events_item).
SELECT * FROM events
WHERE workspace_id = @workspace_id AND item_id = @item_id AND seq < @before_seq
ORDER BY seq DESC
LIMIT @lim;
