-- +goose Up
CREATE TABLE product_feedback (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    reporter_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('problem', 'feature')),
    description TEXT NOT NULL,
    page_path TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'in_progress', 'closed')),
    attachment_key TEXT NOT NULL DEFAULT '',
    attachment_type TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX product_feedback_created ON product_feedback(created_at, id);
CREATE INDEX product_feedback_reporter ON product_feedback(reporter_id, created_at);
CREATE INDEX product_feedback_workspace ON product_feedback(workspace_id);

-- +goose Down
DROP TABLE product_feedback;
