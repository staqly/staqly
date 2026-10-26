-- +goose Up
-- The files a design's pages saved as, in page order (a carousel): the library shows them as one, and a post can
-- take them all at once.
ALTER TABLE media_projects ADD COLUMN page_ids TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE media_projects DROP COLUMN page_ids;
