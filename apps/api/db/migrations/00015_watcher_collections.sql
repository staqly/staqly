-- +goose Up
-- A watcher can add each new entry to several collections. watchers.collection_id stays the first of them
-- (events are filed under it); this table lists all of them, in the order they were given.
CREATE TABLE watcher_collections (
    watcher_id    TEXT NOT NULL REFERENCES watchers (id) ON DELETE CASCADE,
    collection_id TEXT NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
    PRIMARY KEY (watcher_id, collection_id)
);
CREATE INDEX watcher_collections_collection ON watcher_collections (collection_id);
INSERT INTO watcher_collections (watcher_id, collection_id) SELECT id, collection_id FROM watchers;

-- +goose Down
DROP TABLE watcher_collections;
