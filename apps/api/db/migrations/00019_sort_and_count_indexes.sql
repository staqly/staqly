-- +goose Up
-- Item lists sorted by decision date or title: walk an index instead of sorting every item of the workspace.
-- The expressions match itemSorts in internal/db/items_search.go exactly (keyset cursor included).
CREATE INDEX items_workspace_decided_sort ON items (workspace_id, COALESCE(decided_at, ''), id);
CREATE INDEX items_workspace_title_sort ON items (workspace_id, title COLLATE NOCASE, id);

-- Flow view counts: covering indexes, so grouping reads the index only.
CREATE INDEX items_workspace_flow ON items (workspace_id, decision, run, run_action, archived_at, collection_id);
CREATE INDEX items_workspace_source ON items (workspace_id, source, created_at, collection_id);

-- Tags as rows (kept in step with items.tags by triggers): the tag cloud and the tag filter use an index
-- instead of expanding the JSON of every item.
CREATE TABLE item_tags (
    item_id      TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    tag          TEXT NOT NULL,
    PRIMARY KEY (item_id, tag)
) WITHOUT ROWID;
CREATE INDEX item_tags_workspace ON item_tags (workspace_id, tag, item_id);
INSERT OR IGNORE INTO item_tags (item_id, workspace_id, tag)
SELECT i.id, i.workspace_id, CAST(j.value AS TEXT) FROM items i, json_each(i.tags) j;

-- +goose StatementBegin
CREATE TRIGGER item_tags_ai AFTER INSERT ON items BEGIN
    INSERT OR IGNORE INTO item_tags (item_id, workspace_id, tag)
    SELECT new.id, new.workspace_id, CAST(value AS TEXT) FROM json_each(new.tags);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER item_tags_au AFTER UPDATE OF tags, workspace_id ON items BEGIN
    DELETE FROM item_tags WHERE item_id = old.id;
    INSERT OR IGNORE INTO item_tags (item_id, workspace_id, tag)
    SELECT new.id, new.workspace_id, CAST(value AS TEXT) FROM json_each(new.tags);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER item_tags_ad AFTER DELETE ON items BEGIN
    DELETE FROM item_tags WHERE item_id = old.id;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER item_tags_ad;
DROP TRIGGER item_tags_au;
DROP TRIGGER item_tags_ai;
DROP TABLE item_tags;
DROP INDEX items_workspace_source;
DROP INDEX items_workspace_flow;
DROP INDEX items_workspace_title_sort;
DROP INDEX items_workspace_decided_sort;
