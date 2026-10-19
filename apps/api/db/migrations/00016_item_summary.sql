-- +goose Up
-- A one-line summary of what the item is (at most 120 characters), written by the auto-tagger's model.
ALTER TABLE items ADD COLUMN summary TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE items DROP COLUMN summary;
