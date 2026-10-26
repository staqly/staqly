-- name: CreateAIJob :exec
INSERT INTO ai_jobs (id, workspace_id, created_by, operation, kind, status, prompt, params, inputs, input_hash,
  idempotency_key, cost_micros, created_at)
VALUES (?, ?, ?, ?, ?, 'queued', ?, ?, ?, ?, ?, ?, ?);

-- name: GetAIJob :one
SELECT * FROM ai_jobs WHERE workspace_id = ? AND id = ?;

-- name: GetAIJobByIdempotencyKey :one
SELECT * FROM ai_jobs WHERE workspace_id = ? AND created_by = ? AND idempotency_key = ?;

-- name: FindLiveAIJob :one
-- The same request from the same person still queued or running (a double click, a second tab).
SELECT * FROM ai_jobs
WHERE workspace_id = ? AND created_by = ? AND input_hash = ? AND status IN ('queued', 'running')
ORDER BY created_at DESC LIMIT 1;

-- name: CountLiveAIJobs :one
SELECT COUNT(*) FROM ai_jobs WHERE workspace_id = ? AND status IN ('queued', 'running');

-- name: SumAICost :one
-- The estimated cost of a workspace's AI requests since a time (the month's start): running ones count with
-- their estimate, failed and cancelled ones with what they may still have cost (0 when nothing was charged).
SELECT CAST(COALESCE(SUM(cost_micros), 0) AS INTEGER) AS n FROM ai_jobs WHERE workspace_id = ? AND created_at >= ?;

-- name: StartAIJob :execrows
UPDATE ai_jobs SET status = 'running', started_at = ? WHERE id = ? AND status = 'queued';

-- name: SetAIJobSubmitting :execrows
UPDATE ai_jobs SET provider = @provider, model = @model, fallback_from = @fallback_from, submit_state = 'submitting',
  cost_micros = @cost_micros
WHERE id = @id AND status = 'running';

-- name: SetAIJobSubmitted :exec
UPDATE ai_jobs SET submit_state = 'submitted', provider_request = ? WHERE id = ?;

-- name: FinishAIJob :execrows
-- A job ends once: succeeded, failed or cancelled (a later attempt or a cancel that lost the race changes nothing).
UPDATE ai_jobs SET status = @status, result_key = @result_key, result_type = @result_type, result_width = @result_width,
  result_height = @result_height, error_code = @error_code, error = @error, cost_micros = @cost_micros,
  duration_ms = @duration_ms, finished_at = @finished_at
WHERE id = @id AND status IN ('queued', 'running');

-- name: SetAIJobMedia :exec
UPDATE ai_jobs SET media_id = ?, result_key = '' WHERE workspace_id = ? AND id = ?;

-- name: ClearAIJobResult :exec
UPDATE ai_jobs SET result_key = '' WHERE workspace_id = ? AND id = ?;

-- name: ListAIJobsToPurge :many
-- Finished jobs older than the cut whose files and prompt are still kept.
SELECT * FROM ai_jobs WHERE files_purged = 0 AND created_at < ? AND status NOT IN ('queued', 'running')
ORDER BY created_at LIMIT 100;

-- name: PurgeAIJobFiles :exec
UPDATE ai_jobs SET files_purged = 1, prompt = '', inputs = '', result_key = '' WHERE id = ?;
