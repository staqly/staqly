-- +goose Up
-- The webhook worker looks for items stuck in processing on every pass (StaleProcessingItems);
-- without this index that query scanned and sorted the whole items table.
CREATE INDEX items_processing ON items (updated_at) WHERE status = 'processing';

-- +goose Down
DROP INDEX items_processing;
