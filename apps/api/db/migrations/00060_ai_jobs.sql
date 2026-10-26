-- +goose Up
-- ai_jobs: one AI request of the design editor or the media library (a picture or a video made by fal.ai or
-- Higgsfield), run by the job queue (ai_run:<id>). The row is also the AI usage ledger: who asked, which provider
-- and model ran it, how long it took and what it is estimated to cost (cost_micros, millionths of a US dollar;
-- the monthly limit sums it). Inputs and the result are temporary files under workspaces/<id>/ai/<job>/, deleted
-- once accepted, discarded or a day old; the prompt is cleared with them.
--   status: queued, running, succeeded, failed, canceled
--   submit_state: '' (not sent), submitting (sent, answer not seen: never sent again blindly), submitted
CREATE TABLE ai_jobs (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    created_by          TEXT NOT NULL,
    operation           TEXT NOT NULL,
    kind                TEXT NOT NULL,
    status              TEXT NOT NULL,
    prompt              TEXT NOT NULL DEFAULT '',
    params              TEXT NOT NULL DEFAULT '{}',
    inputs              TEXT NOT NULL DEFAULT '',
    input_hash          TEXT NOT NULL,
    idempotency_key     TEXT NOT NULL DEFAULT '',
    provider            TEXT NOT NULL DEFAULT '',
    model               TEXT NOT NULL DEFAULT '',
    fallback_from       TEXT NOT NULL DEFAULT '',
    submit_state        TEXT NOT NULL DEFAULT '',
    provider_request    TEXT NOT NULL DEFAULT '',
    result_key          TEXT NOT NULL DEFAULT '',
    result_type         TEXT NOT NULL DEFAULT '',
    result_width        INTEGER NOT NULL DEFAULT 0,
    result_height       INTEGER NOT NULL DEFAULT 0,
    media_id            TEXT NOT NULL DEFAULT '',
    error_code          TEXT NOT NULL DEFAULT '',
    error               TEXT NOT NULL DEFAULT '',
    cost_micros         INTEGER NOT NULL DEFAULT 0,
    duration_ms         INTEGER NOT NULL DEFAULT 0,
    created_at          TEXT NOT NULL,
    started_at          TEXT NOT NULL DEFAULT '',
    finished_at         TEXT NOT NULL DEFAULT '',
    files_purged        INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX ai_jobs_workspace_created ON ai_jobs (workspace_id, created_at);
CREATE UNIQUE INDEX ai_jobs_idempotency ON ai_jobs (workspace_id, created_by, idempotency_key) WHERE idempotency_key <> '';
CREATE INDEX ai_jobs_unpurged ON ai_jobs (files_purged, created_at);

-- +goose Down
DROP TABLE ai_jobs;
