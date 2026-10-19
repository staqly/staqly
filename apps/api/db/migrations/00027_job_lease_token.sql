-- +goose Up
-- Fenced leases: every claim of a job gets a fresh lease_token; renewing and finishing the job need
-- it, so a worker whose lease ran out (and whose job another worker took) cannot finish or extend
-- it. requeued records that the job's key was queued again while it ran: the run is not duplicated,
-- the job goes back to the queue when the current run ends.
ALTER TABLE jobs ADD COLUMN lease_token TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN requeued INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE jobs DROP COLUMN requeued;
ALTER TABLE jobs DROP COLUMN lease_token;
