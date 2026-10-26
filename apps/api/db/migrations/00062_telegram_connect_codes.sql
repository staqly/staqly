-- +goose Up
-- telegram_connect_codes: connecting a Telegram chat through the server's own bot. The dashboard asks for a code;
-- the person adds the bot to the chat and sends /connect CODE there; the bot's poller finds the code, connects
-- the chat as a channel of the workspace that asked and records the outcome (channel_id, or error).
-- Codes last 10 minutes and are deleted a day after.
CREATE TABLE telegram_connect_codes (
    code         TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    created_by   TEXT NOT NULL,
    actor_name   TEXT NOT NULL DEFAULT '',
    group_id     TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    channel_id   TEXT NOT NULL DEFAULT '',
    error        TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE telegram_connect_codes;
