-- +goose Up
-- Watcher presets (Reddit, YouTube, GitHub...) are gone; watchers take any URL.
ALTER TABLE watchers DROP COLUMN preset_id;

-- +goose Down
ALTER TABLE watchers ADD COLUMN preset_id TEXT NOT NULL DEFAULT '';
