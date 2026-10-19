-- +goose Up
-- Which agent (MCP client or REST User-Agent, as listed under Agents) caused an event, so activity can be
-- shown per agent even when every local agent shares the keyless "local" principal.
ALTER TABLE events ADD COLUMN agent TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE events DROP COLUMN agent;
