-- +goose Up
-- A sign-in that may publish as several accounts (Facebook Pages, YouTube channels, Google Business locations...)
-- waits here, sealed, while the person picks the accounts that become channels. Rows live ten minutes.
CREATE TABLE pending_connections (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    provider     TEXT NOT NULL,
    credentials  TEXT NOT NULL,
    accounts     TEXT NOT NULL DEFAULT '[]',
    link_id      TEXT NOT NULL DEFAULT '',
    group_id     TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL
);

CREATE INDEX pending_connections_expiry ON pending_connections (expires_at);

-- +goose Down
DROP TABLE pending_connections;
