-- +goose Up
-- Mastodon connects with OAuth now ("mastodon"). Every Mastodon channel connected before uses an access token, which
-- keeps working as its own platform, "mastodon_token": same channel ids, credentials and posts.
UPDATE channels SET provider = 'mastodon_token' WHERE provider = 'mastodon';

-- +goose Down
UPDATE channels SET provider = 'mastodon' WHERE provider = 'mastodon_token';
