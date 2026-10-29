-- +goose Up
-- A channel group's icon: one name from the dashboard's set (app.channelGroupIcons, "briefcase"), '' for none.
ALTER TABLE channel_groups ADD COLUMN icon TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE channel_groups DROP COLUMN icon;
