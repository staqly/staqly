-- +goose Up
-- Archiving undecided work now rejects it: fix items archived while still pending.
UPDATE items SET decision = 'rejected', decided_at = COALESCE(decided_at, archived_at)
WHERE archived_at IS NOT NULL AND decision = 'pending';

-- +goose Down
SELECT 1;
