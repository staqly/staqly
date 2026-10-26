-- +goose Up
-- AI pictures run on the server's own OpenAI keys (AI_OPENAI_API_KEY); the workspaces' own keys are forgotten.
DROP TABLE IF EXISTS workspace_ai;

-- +goose Down
CREATE TABLE IF NOT EXISTS workspace_ai (
  workspace_id TEXT PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  credentials TEXT NOT NULL,
  key_hint TEXT NOT NULL,
  updated_by TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);
