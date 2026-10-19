-- +goose Up
-- Scheduled publishing: the earliest time the publish run may start once the item is approved. The
-- scheduler starts it then (item.publish_requested with data.scheduled); until then an approved item
-- with a publish_at is "scheduled". Kept after the run as the planned time (calendar). NULL = no schedule:
-- publish on approval (auto_run) or by hand, as before.
ALTER TABLE items ADD COLUMN publish_at TEXT;
CREATE INDEX items_publish_at ON items (workspace_id, publish_at) WHERE publish_at IS NOT NULL;

-- +goose Down
DROP INDEX items_publish_at;
ALTER TABLE items DROP COLUMN publish_at;
