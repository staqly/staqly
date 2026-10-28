-- +goose Up
-- The analytics page (GET /v1/analytics) counts what waits (scheduled posts, drafts, the next time one goes out)
-- and the preview feedback threads of a workspace; without these both read every post or comment of it.
CREATE INDEX posts_status ON posts (workspace_id, status, publish_at);

CREATE INDEX post_comments_workspace ON post_comments (workspace_id, parent_id, created_at);

-- +goose Down
DROP INDEX post_comments_workspace;

DROP INDEX posts_status;
