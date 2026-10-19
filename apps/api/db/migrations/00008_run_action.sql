-- +goose Up
-- The collection action that started the item's current run (e.g. publish), so the run can read
-- "Publishing" / "Published" / "Publish failed" with that action's own labels. NULL = no run, or unknown.
ALTER TABLE items ADD COLUMN run_action TEXT;

-- +goose Down
ALTER TABLE items DROP COLUMN run_action;
