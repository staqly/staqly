-- +goose Up
ALTER TABLE media ADD COLUMN purge_started_at TEXT;

-- +goose Down
ALTER TABLE media DROP COLUMN purge_started_at;
