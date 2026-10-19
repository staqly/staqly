-- name: UpsertJob :exec
-- One live job per key: scheduling again moves the run time (and resets the attempts) instead of adding a job.
-- A job that is running keeps its lease and runs once: it is only marked requeued, and FinishJob puts it back
-- in the queue at the new run time when the current run ends (never two runs of one key at once).
INSERT INTO jobs (id, workspace_id, kind, key, payload, status, run_at, attempts, max_attempts, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, 'queued', ?, 0, ?, ?, ?)
ON CONFLICT (key) DO UPDATE SET payload = excluded.payload, run_at = excluded.run_at,
    max_attempts = excluded.max_attempts, last_error = '', done_at = NULL, updated_at = excluded.updated_at,
    status = CASE WHEN jobs.status = 'running' THEN 'running' ELSE 'queued' END,
    requeued = CASE WHEN jobs.status = 'running' THEN 1 ELSE 0 END,
    attempts = CASE WHEN jobs.status = 'running' THEN jobs.attempts ELSE 0 END,
    lease_until = CASE WHEN jobs.status = 'running' THEN jobs.lease_until ELSE NULL END,
    leased_by = CASE WHEN jobs.status = 'running' THEN jobs.leased_by ELSE '' END,
    lease_token = CASE WHEN jobs.status = 'running' THEN jobs.lease_token ELSE '' END;

-- name: CancelJob :execrows
-- Queued only: a running job finishes on its own (it checks the state it acts on first), and the job
-- that cancels its own key (a timeout settling its target) must still end as done.
-- A running job that was queued again only loses that request (requeued = 0): its current run ends normally.
UPDATE jobs SET status = CASE WHEN status = 'queued' THEN 'cancelled' ELSE status END,
    done_at = CASE WHEN status = 'queued' THEN sqlc.narg(now) ELSE done_at END, requeued = 0, updated_at = sqlc.narg(now)
WHERE key = @key AND (status = 'queued' OR (status = 'running' AND requeued = 1));

-- name: DueJobs :many
-- Queued and due, plus running ones whose lease ran out (a crashed worker): oldest run time first.
SELECT * FROM jobs
WHERE (status = 'queued' AND run_at <= @now) OR (status = 'running' AND lease_until IS NOT NULL AND lease_until < @now)
ORDER BY run_at LIMIT @lim;

-- name: LeaseJob :execrows
-- Atomic claim: only one worker's UPDATE matches. A fresh lease_token fences the previous holder out.
UPDATE jobs SET status = 'running', lease_until = @lease_until, leased_by = @leased_by, lease_token = @lease_token,
    attempts = attempts + 1, updated_at = @now
WHERE id = @id AND ((status = 'queued' AND run_at <= @now) OR (status = 'running' AND lease_until IS NOT NULL AND lease_until < @now));

-- name: RenewJobLease :execrows
-- The worker still holding the lease extends it while the run goes on; 0 rows: the lease was lost.
UPDATE jobs SET lease_until = @lease_until, updated_at = @now WHERE id = @id AND status = 'running' AND lease_token = @lease_token;

-- name: FinishJob :one
-- Only the lease holder finishes (lease_token): a worker whose lease ran out and was taken over cannot.
-- A job queued again while it ran (requeued) goes back to the queue at that run time instead of ending.
-- Returns the status it ended in; no row means the lease was lost.
UPDATE jobs SET
    status = CASE WHEN requeued = 1 THEN 'queued' ELSE @status END,
    run_at = CASE WHEN requeued = 1 THEN run_at ELSE @run_at END,
    attempts = CASE WHEN requeued = 1 THEN 0 ELSE attempts END,
    last_error = CASE WHEN requeued = 1 THEN '' ELSE @last_error END,
    done_at = CASE WHEN requeued = 1 THEN NULL ELSE @done_at END,
    requeued = 0, lease_until = NULL, leased_by = '', lease_token = '', updated_at = @now
WHERE id = @id AND status = 'running' AND lease_token = @lease_token
RETURNING status;

-- name: GetJobByKey :one
SELECT * FROM jobs WHERE key = ?;

-- name: PruneJobs :execrows
DELETE FROM jobs WHERE status IN ('done', 'failed', 'cancelled') AND done_at < ?;

-- name: ObsJobCounts :many
SELECT status, COUNT(*) AS n FROM jobs GROUP BY status;

-- name: ListJobs :many
-- Newest first; keyset on (created_at, id).
SELECT * FROM jobs
WHERE workspace_id = @workspace_id
  AND (sqlc.narg(status) IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(kind) IS NULL OR kind = sqlc.narg(kind))
  AND (sqlc.narg(before_created) IS NULL OR (created_at, id) < (sqlc.narg(before_created), sqlc.narg(before_id)))
ORDER BY created_at DESC, id DESC LIMIT @lim;

-- name: GetJob :one
SELECT * FROM jobs WHERE workspace_id = ? AND id = ?;

-- name: JobCountsByKind :many
SELECT kind, status, COUNT(*) AS n FROM jobs WHERE workspace_id = ? GROUP BY kind, status;

-- name: JobsDoneSince :one
SELECT COUNT(*) FROM jobs WHERE workspace_id = ? AND status = 'done' AND done_at >= ?;

-- name: RetryJob :execrows
UPDATE jobs SET status = 'queued', run_at = @now, last_error = '', done_at = NULL, lease_until = NULL, leased_by = '', updated_at = @now
WHERE workspace_id = @workspace_id AND id = @id AND status = 'failed';

-- name: CancelJobByID :execrows
UPDATE jobs SET status = 'cancelled', done_at = @now, updated_at = @now WHERE workspace_id = @workspace_id AND id = @id AND status = 'queued';
