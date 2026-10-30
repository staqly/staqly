-- +goose Up
-- A post's title, shared by its channels: platforms with a title field (YouTube, Reddit, blogs) publish it, the
-- others only show it in staqly's lists. '' on posts made before it was required.
ALTER TABLE posts ADD COLUMN title TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE posts DROP COLUMN title;
