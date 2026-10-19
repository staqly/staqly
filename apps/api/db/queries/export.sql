-- ---- event export ----

-- name: ExportEventsPage :many
SELECT * FROM events
WHERE workspace_id = @workspace_id
  AND seq > @after_seq
  AND (sqlc.narg(from_ts) IS NULL OR created_at >= sqlc.narg(from_ts))
  AND (sqlc.narg(to_ts) IS NULL OR created_at < sqlc.narg(to_ts))
  AND (sqlc.narg(type) IS NULL OR type = sqlc.narg(type))
  AND (sqlc.narg(type_prefix) IS NULL OR substr(type, 1, length(sqlc.narg(type_prefix))) = sqlc.narg(type_prefix))
  AND (sqlc.narg(actor_type) IS NULL OR actor_type = sqlc.narg(actor_type))
  AND (sqlc.narg(actor_id) IS NULL OR actor_id = sqlc.narg(actor_id))
  AND (sqlc.narg(collection_id) IS NULL OR collection_id = sqlc.narg(collection_id))
  AND (sqlc.narg(post_id) IS NULL OR item_id = sqlc.narg(post_id))
  -- Same rule as ListEvents: credential/session audit events only for admin principals.
  AND (CAST(@include_audit AS INTEGER) = 1 OR NOT (type GLOB 'api_key.*' OR type GLOB 'session.*'
       OR type GLOB 'recovery_key.*' OR type GLOB 'device.*' OR type GLOB 'device_link.*'))
ORDER BY seq
LIMIT @lim;

-- ---- workspace export jobs ----

-- name: CreateWorkspaceExport :exec
INSERT INTO workspace_exports (id, workspace_id, actor_type, actor_id, actor_name, created_at, expires_at, includes_media)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetWorkspaceExport :one
SELECT * FROM workspace_exports WHERE workspace_id = ? AND id = ?;

-- name: ActiveWorkspaceExport :one
SELECT * FROM workspace_exports WHERE workspace_id = ? AND status IN ('pending', 'running') LIMIT 1;

-- name: ListWorkspaceExports :many
SELECT * FROM workspace_exports WHERE workspace_id = ? ORDER BY created_at DESC LIMIT 20;

-- name: ListWorkspaceExportIDs :many
SELECT id FROM workspace_exports WHERE workspace_id = ?;

-- name: PendingWorkspaceExports :many
SELECT * FROM workspace_exports WHERE status = 'pending' ORDER BY created_at LIMIT ?;

-- name: ClaimWorkspaceExport :execrows
UPDATE workspace_exports SET status = 'running', started_at = ? WHERE id = ? AND status = 'pending';

-- name: RequeueRunningExports :exec
UPDATE workspace_exports SET status = 'pending', started_at = NULL WHERE status = 'running';

-- name: RequeueStaleExports :exec
-- PostgreSQL (several server processes): only exports abandoned long ago, not the ones another process builds.
UPDATE workspace_exports SET status = 'pending', started_at = NULL WHERE status = 'running' AND started_at < @before;

-- name: FinishWorkspaceExport :execrows
-- Only a still-running export finishes: a purge or workspace deletion may
-- have expired (or removed) it while it was being built.
UPDATE workspace_exports SET status = ?, size_bytes = ?, error = ?, finished_at = ?, expires_at = ?
WHERE id = ? AND status = 'running';

-- name: ExpiredWorkspaceExports :many
SELECT id FROM workspace_exports WHERE status IN ('ready', 'failed') AND expires_at <= ? LIMIT 100;

-- name: ReadyWorkspaceExports :many
-- Exports that may contain current data: finished or being built right now.
SELECT id FROM workspace_exports WHERE workspace_id = ? AND status IN ('ready', 'running');

-- name: ExportStatusByID :one
SELECT status FROM workspace_exports WHERE id = ?;

-- name: MarkWorkspaceExportExpired :exec
UPDATE workspace_exports SET status = 'expired' WHERE id = ?;

-- name: DeleteOldWorkspaceExports :execrows
DELETE FROM workspace_exports WHERE status IN ('expired', 'failed') AND created_at < ?;

-- name: ExportAllCollections :many
SELECT * FROM collections WHERE workspace_id = ? ORDER BY created_at, id;

-- name: ExportMediaPage :many
SELECT * FROM media WHERE workspace_id = @workspace_id AND id > @after_id ORDER BY id LIMIT @lim;

-- ---- retention ----

-- name: RetentionWorkspaceIDs :many
SELECT id FROM workspaces ORDER BY id;

-- name: PruneDeliveries :execrows
DELETE FROM webhook_deliveries
WHERE id IN (SELECT id FROM webhook_deliveries d
             WHERE d.status IN ('succeeded', 'failed') AND d.created_at < @cutoff LIMIT @lim);

-- name: DeleteDeliveriesForOldEvents :execrows
-- Finished deliveries of events about to be pruned (they reference events by FK).
DELETE FROM webhook_deliveries
WHERE webhook_deliveries.workspace_id = @workspace_id AND webhook_deliveries.status <> 'pending'
  AND event_id IN (SELECT e.id FROM events e WHERE e.workspace_id = @workspace_id AND e.created_at < @cutoff);

-- name: PruneEvents :execrows
-- Events with a delivery still pending are kept until it finishes (at-least-once).
DELETE FROM events
WHERE seq IN (SELECT e.seq FROM events e
              WHERE e.workspace_id = @workspace_id AND e.created_at < @cutoff
                AND NOT EXISTS (SELECT 1 FROM webhook_deliveries d WHERE d.event_id = e.id)
              ORDER BY e.seq LIMIT @lim);

-- ---- GDPR: erase a post's history, erase workspace ----

-- name: DeleteDeliveriesForPostEvents :execrows
DELETE FROM webhook_deliveries
WHERE webhook_deliveries.workspace_id = @workspace_id
  AND webhook_deliveries.event_id IN (SELECT e.id FROM events e WHERE e.workspace_id = @workspace_id AND e.item_id = @post_id);

-- name: DeletePostEvents :execrows
DELETE FROM events WHERE workspace_id = ? AND item_id = @post_id;

-- name: DeleteIdempotencyResponsesMentioning :exec
DELETE FROM idempotency_keys WHERE workspace_id = ? AND instr(CAST(response_body AS TEXT), CAST(@needle AS TEXT)) > 0;

-- name: ListWorkspaceMediaFiles :many
SELECT storage_name FROM media WHERE workspace_id = ?;

-- name: DeleteWorkspaceDeliveries :exec
DELETE FROM webhook_deliveries WHERE workspace_id = ?;

-- name: DeleteWorkspaceRow :execrows
DELETE FROM workspaces WHERE id = ?;

-- name: DeleteInstallationFor :exec
DELETE FROM installation WHERE workspace_id = ?;
