-- +goose Up
-- Whether this connection may post comments under its posts. LinkedIn allows it only to an app with the
-- Community Management API (w_member_social_feed); channels connected before were connected without it.
ALTER TABLE channels ADD COLUMN comments INTEGER NOT NULL DEFAULT 1;
UPDATE channels SET comments = 0 WHERE provider = 'linkedin';

-- +goose Down
ALTER TABLE channels DROP COLUMN comments;
