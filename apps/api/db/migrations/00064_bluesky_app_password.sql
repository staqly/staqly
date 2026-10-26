-- +goose Up
-- Bluesky connects with OAuth now ("bluesky"). Every Bluesky channel connected before uses an app password, which
-- keeps working as its own platform, "bluesky_app_password": same channel ids, credentials and posts.
UPDATE channels SET provider = 'bluesky_app_password' WHERE provider = 'bluesky';

-- +goose Down
UPDATE channels SET provider = 'bluesky' WHERE provider = 'bluesky_app_password';
