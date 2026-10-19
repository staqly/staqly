-- name: CreateSchedule :exec
INSERT INTO schedules (id, workspace_id, collection_id, name, channels, kind, run_at, rule, pick, post_ids, source, endpoint_url, endpoint_secret, enabled, next_run_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSchedule :one
SELECT * FROM schedules WHERE workspace_id = ? AND id = ?;

-- name: GetScheduleByID :one
SELECT * FROM schedules WHERE id = ?;

-- name: ListSchedules :many
SELECT * FROM schedules WHERE workspace_id = @workspace_id AND (sqlc.narg(collection_id) IS NULL OR collection_id = sqlc.narg(collection_id))
ORDER BY collection_id, name COLLATE NOCASE, id;

-- name: UpdateSchedule :exec
UPDATE schedules SET name = ?, channels = ?, kind = ?, run_at = ?, rule = ?, pick = ?, post_ids = ?, source = ?, endpoint_url = ?, endpoint_secret = ?,
    enabled = ?, next_run_at = ?, updated_at = ?
WHERE workspace_id = ? AND id = ?;

-- name: FinishScheduleRun :exec
UPDATE schedules SET enabled = ?, next_run_at = ?, last_run_at = ?, last_result = ?, updated_at = ? WHERE id = ?;

-- name: DeleteSchedule :execrows
DELETE FROM schedules WHERE workspace_id = ? AND id = ?;

-- name: CountSchedules :one
SELECT COUNT(*) FROM schedules WHERE workspace_id = ?;

-- name: ExportAllSchedules :many
SELECT id, workspace_id, collection_id, name, channels, kind, run_at, rule, pick, post_ids, source, endpoint_url, enabled, next_run_at, last_run_at, last_result, created_at, updated_at
FROM schedules WHERE workspace_id = ? ORDER BY created_at, id;
