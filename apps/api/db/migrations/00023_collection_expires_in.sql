-- +goose Up
-- A collection's default review window, in seconds (NULL = never): an item created pending or draft
-- without its own expires_in gets expires_at = created_at + expires_in and becomes expired when nobody
-- decides it in time. Changing it later leaves existing items as they are.
ALTER TABLE collections ADD COLUMN expires_in INTEGER;

-- +goose Down
ALTER TABLE collections DROP COLUMN expires_in;
