-- +goose Up
-- Labels on an item (a JSON array of strings), set by agents or reviewers, filterable.
ALTER TABLE items ADD COLUMN tags TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE items DROP COLUMN tags;
