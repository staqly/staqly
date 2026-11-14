-- +goose Up
-- What happens around a channel on its platform, from the platform's webhook (Kick): follows, new and renewed
-- subscriptions, gifted subscriptions (count: how many in one gift). The analytics' Audience section sums them.
CREATE TABLE channel_activity (
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    channel_id   TEXT NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('follow', 'subscription', 'renewal', 'gift')),
    count        INTEGER NOT NULL DEFAULT 1,
    at           TEXT NOT NULL
);

CREATE INDEX channel_activity_at ON channel_activity (workspace_id, at);

-- +goose Down
DROP TABLE channel_activity;
