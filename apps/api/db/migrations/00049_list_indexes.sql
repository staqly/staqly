-- +goose Up
-- The post list sorts by publish time with unscheduled posts last (db.SearchPosts: COALESCE(publish_at, '9999')
-- ascending, the default, and COALESCE(publish_at, '') descending). posts_calendar holds the bare column, so
-- each page sorted every post of the workspace; these hold the sort keys themselves, so a page reads its rows
-- in order and stops at the limit.
CREATE INDEX posts_publish_asc ON posts (workspace_id, (COALESCE(publish_at, '9999')), id);

CREATE INDEX posts_publish_desc ON posts (workspace_id, (COALESCE(publish_at, '')), id);

-- The channel list's activity (db.ChannelActivity): a channel's published targets by time.
CREATE INDEX post_targets_channel_published ON post_targets (channel_id, status, published_at);

-- +goose Down
DROP INDEX post_targets_channel_published;

DROP INDEX posts_publish_desc;

DROP INDEX posts_publish_asc;
