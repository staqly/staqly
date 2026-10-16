-- +goose Up
-- Agents seen talking to the workspace: an MCP client (by its clientInfo) or a REST client (by its User-Agent),
-- per credential. Recorded automatically; browser sessions are not agents.
CREATE TABLE agents (
    id             TEXT PRIMARY KEY,
    workspace_id   TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    kind           TEXT NOT NULL,             -- mcp | api
    name           TEXT NOT NULL,
    version        TEXT NOT NULL DEFAULT '',
    principal_id   TEXT NOT NULL,             -- the API key id, or "local"
    principal_name TEXT NOT NULL DEFAULT '',
    first_seen_at  TEXT NOT NULL,
    last_seen_at   TEXT NOT NULL,
    requests       INTEGER NOT NULL DEFAULT 0,
    items_created  INTEGER NOT NULL DEFAULT 0,
    UNIQUE (workspace_id, kind, name, principal_id)
);

-- +goose Down
DROP TABLE agents;
