-- +goose Up
-- Structured sources: a watcher on a JSON or XML document says where its entries are (extract: format,
-- items path and the field paths for title, url, id, content, published, author). NULL = no extraction:
-- rss entries or url_diff page changes, as before.
-- ponytail: the kind column's CHECK (rss, url_diff) stays as the fetch strategy; json and xml watchers are
-- stored as url_diff + extract and reported by their format. Rebuild the table if kinds multiply.
ALTER TABLE watchers ADD COLUMN extract TEXT;

-- +goose Down
ALTER TABLE watchers DROP COLUMN extract;
