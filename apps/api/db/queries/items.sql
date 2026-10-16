-- name: CreateItem :exec
INSERT INTO items (id, workspace_id, collection_id, title, type, content, url, media_id, metadata, status, decision, run, source, external_ref, tags, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetItem :one
SELECT * FROM items WHERE workspace_id = ? AND id = ?;

-- name: UpdateItem :execrows
-- Optimistic concurrency: only succeeds if the caller saw the current version.
UPDATE items
SET title = @title, content = @content, url = @url, media_id = @media_id, metadata = @metadata,
    status = @status, source = @source, external_ref = @external_ref, feedback = @feedback,
    decided_at = @decided_at, collection_id = @collection_id,
    decision = @decision, run = @run, archived_at = @archived_at, run_action = @run_action, tags = @tags,
    version = version + 1, updated_at = @updated_at
WHERE workspace_id = @workspace_id AND id = @id AND version = @version;

-- name: QueueSummary :exec
UPDATE items SET summary_next_at = @next_at, summary_attempts = 0 WHERE workspace_id = @workspace_id AND id = @id;

-- name: DueSummaries :many
SELECT id, workspace_id, summary_next_at FROM items
WHERE summary_next_at IS NOT NULL AND summary_next_at <= @now
ORDER BY summary_next_at LIMIT @lim;

-- name: LeaseSummary :execrows
-- Push the next try out first, so a crash mid-call only delays the item.
UPDATE items SET summary_next_at = @lease_until, summary_attempts = summary_attempts + 1
WHERE workspace_id = @workspace_id AND id = @id AND summary_next_at = @expected;

-- name: RetrySummary :exec
-- NULL gives up (too many attempts); done items are cleared by SetItemSummary.
UPDATE items SET summary_next_at = @next_at WHERE workspace_id = @workspace_id AND id = @id;

-- name: SetItemSummary :exec
-- Written by the model after the item is saved: no new version, and updated_at stays.
UPDATE items SET summary = @summary, summary_next_at = NULL, summary_attempts = 0
WHERE workspace_id = @workspace_id AND id = @id;

-- name: SetItemTags :execrows
-- Labels, not content: no new version, and updated_at stays.
UPDATE items SET tags = @tags WHERE workspace_id = @workspace_id AND id = @id;

-- name: SetItemProcessingTimeout :execrows
-- Not a content change: no new version, and updated_at (when processing started) stays.
UPDATE items SET processing_timeout_seconds = @processing_timeout_seconds
WHERE workspace_id = @workspace_id AND id = @id;

-- name: DeleteItem :execrows
DELETE FROM items WHERE workspace_id = ? AND id = ?;

-- name: InsertItemVersion :exec
INSERT INTO item_versions (item_id, version, snapshot, actor, created_at) VALUES (?, ?, ?, ?, ?);

-- name: ListItemVersions :many
SELECT * FROM item_versions WHERE item_id = ? ORDER BY version DESC;

-- name: CountItemsReferencingMedia :one
SELECT COUNT(*) FROM items WHERE media_id = ?;

-- name: SetItemEditable :exec
UPDATE items SET editable = ? WHERE id = ?;

-- name: GetItemEditable :one
SELECT editable FROM items WHERE id = ?;

-- name: ListTags :many
-- Every tag in the workspace with how many items carry it, most used first.
SELECT CAST(j.value AS TEXT) AS tag, COUNT(*) AS items
FROM items i, json_each(i.tags) j
WHERE i.workspace_id = @workspace_id
GROUP BY j.value
ORDER BY items DESC, tag
LIMIT 500;

-- name: FlowStates :many
-- How many items sit in each state, for the flow view (collection_id '' = every collection).
SELECT CAST(COALESCE(decision, '') AS TEXT) AS decision, run, CAST(COALESCE(run_action, '') AS TEXT) AS run_action,
       CAST(archived_at IS NOT NULL AS INTEGER) AS archived, COUNT(*) AS items
FROM items
WHERE workspace_id = @workspace_id AND (CAST(@collection_id AS TEXT) = '' OR collection_id = @collection_id)
GROUP BY 1, 2, 3, 4;

-- name: FlowEvents :many
-- Events since a time by type: how many items moved along each edge of the flow view.
SELECT type, COUNT(*) AS events
FROM events
WHERE workspace_id = @workspace_id AND created_at >= @since
  AND (CAST(@collection_id AS TEXT) = '' OR collection_id = @collection_id)
GROUP BY type;

-- name: FlowSources :many
-- Who adds items (the item's source: an agent, n8n, a watcher), most recent first.
SELECT source, COUNT(*) AS items, CAST(MAX(created_at) AS TEXT) AS last_created_at
FROM items
WHERE workspace_id = @workspace_id AND (CAST(@collection_id AS TEXT) = '' OR collection_id = @collection_id)
GROUP BY source
ORDER BY last_created_at DESC
LIMIT 50;
