-- +goose Up
-- Whop channels connected with an Account API key before Whop had sign-in become Whop (API key) channels: same
-- credentials and publishing, and Whop itself is the sign-in (a person; each post picks the business).
UPDATE channels SET provider = 'whop_key' WHERE provider = 'whop' AND deleted_at IS NULL;

-- +goose Down
UPDATE channels SET provider = 'whop' WHERE provider = 'whop_key';
