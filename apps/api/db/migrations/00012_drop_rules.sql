-- +goose Up
-- Rules are gone from staqly (see docs/PROGRESS.md); their table goes too.
DROP TABLE IF EXISTS rules;

-- +goose Down
CREATE TABLE rules (
    id             TEXT PRIMARY KEY,
    workspace_id   TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    priority       INTEGER NOT NULL DEFAULT 100,
    enabled        INTEGER NOT NULL DEFAULT 1,
    match          TEXT NOT NULL CHECK (json_valid(match)),
    action         TEXT NOT NULL,
    params         TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(params)),
    times_applied  INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE INDEX rules_workspace ON rules (workspace_id, enabled, priority);
