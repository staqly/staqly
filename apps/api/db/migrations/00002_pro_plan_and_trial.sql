-- +goose Up
-- Two cloud plans: free and pro (indie and team become pro; limits live in
-- internal/app/plans.go). plan_expires_at ends a pro trial or paid period:
-- NULL = no end; past = suspended (read-only + export) until it is paid or
-- the workspace switches to free.
UPDATE workspaces SET plan = 'pro' WHERE plan IN ('indie', 'team');
ALTER TABLE workspaces ADD COLUMN plan_expires_at TEXT;

-- +goose Down
ALTER TABLE workspaces DROP COLUMN plan_expires_at;
