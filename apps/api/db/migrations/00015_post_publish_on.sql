-- +goose Up
-- What a scheduled post waits for instead of a time: "live:<channel id>", the channel's next stream start (a
-- platform that reports it, providers.LiveNotifier). Empty: the post goes by publish_at as before.
ALTER TABLE posts ADD COLUMN publish_on TEXT NOT NULL DEFAULT '';

CREATE INDEX posts_publish_on ON posts (publish_on) WHERE publish_on <> '';

-- +goose Down
DROP INDEX posts_publish_on;
ALTER TABLE posts DROP COLUMN publish_on;
