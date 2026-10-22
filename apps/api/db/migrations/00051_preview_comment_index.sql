-- +goose Up
-- The per-link comment limit on public preview pages counts a link's recent comments on every comment.
CREATE INDEX post_comments_preview ON post_comments (preview_id, created_at);

-- +goose Down
DROP INDEX post_comments_preview;
