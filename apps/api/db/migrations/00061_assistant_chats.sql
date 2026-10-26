-- +goose Up
-- assistant_chats: the dashboard assistant's conversations, one person's in one workspace (owner: the user id, or
-- the principal for local mode and keys). Saved by the server after every turn; listed newest first by updated_at
-- and searched by title (the first thing the person asked).
--   messages: the conversation in the model's chat format (JSON array), at most the last 200
--   pending: the tool call waiting for Allow or Deny (JSON object), '' for none
CREATE TABLE assistant_chats (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    owner        TEXT NOT NULL,
    title        TEXT NOT NULL,
    messages     TEXT NOT NULL DEFAULT '[]',
    pending      TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);
CREATE INDEX assistant_chats_list ON assistant_chats (workspace_id, owner, updated_at, id);

-- +goose Down
DROP TABLE assistant_chats;
