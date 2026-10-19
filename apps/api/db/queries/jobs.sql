-- The job queue runs on River (internal/app/queue.go, river_* tables). What stays here:

-- ---- the old queue's table: carried into River once at startup ----

-- name: ListLegacyJobs :many
-- Queued and running rows of the queue before River (an upgrade, or `server import-sqlite`).
SELECT * FROM jobs WHERE status IN ('queued', 'running') ORDER BY run_at;

-- name: DeleteLegacyJobs :exec
DELETE FROM jobs;

-- ---- a key queued again while its job runs ----

-- name: UpsertJobRequeue :exec
-- The latest request wins: one pending run per key.
INSERT INTO job_requeues (key, workspace_id, kind, payload, run_at, max_attempts, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (key) DO UPDATE SET payload = excluded.payload, run_at = excluded.run_at,
    max_attempts = excluded.max_attempts, created_at = excluded.created_at;

-- name: GetJobRequeue :one
SELECT * FROM job_requeues WHERE key = ?;

-- name: DeleteJobRequeue :execrows
DELETE FROM job_requeues WHERE key = ?;

-- name: DeleteWorkspaceJobRequeues :exec
DELETE FROM job_requeues WHERE workspace_id = ?;
