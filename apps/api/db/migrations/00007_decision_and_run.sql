-- +goose Up
-- An item's state as two independent facts instead of one mixed status:
--   decision  what the reviewer decided: pending, approved, rejected, expired (NULL = draft, not submitted)
--   run       what the automation did after: none, processing, completed, failed
--   archived_at  put away (any decision, any run)
-- `status` stays as the single summary older clients read.
ALTER TABLE items ADD COLUMN decision TEXT CHECK (decision IN ('pending', 'approved', 'rejected', 'expired'));
ALTER TABLE items ADD COLUMN run TEXT NOT NULL DEFAULT 'none' CHECK (run IN ('none', 'processing', 'completed', 'failed'));
ALTER TABLE items ADD COLUMN archived_at TEXT;

-- The last decision event tells what was decided before an item went on to processing, completed or failed.
UPDATE items SET decision = CASE
    WHEN status = 'draft' THEN NULL
    WHEN status IN ('pending', 'approved', 'rejected', 'expired') THEN status
    ELSE COALESCE((
        SELECT CASE e.type WHEN 'item.approved' THEN 'approved' WHEN 'item.rejected' THEN 'rejected'
                           WHEN 'item.expired' THEN 'expired' END
        FROM events e
        WHERE e.workspace_id = items.workspace_id AND e.item_id = items.id
          AND e.type IN ('item.approved', 'item.rejected', 'item.expired')
        ORDER BY e.seq DESC LIMIT 1), CASE WHEN decided_at IS NOT NULL THEN 'approved' ELSE 'pending' END)
END;
UPDATE items SET run = status WHERE status IN ('processing', 'completed', 'failed');
UPDATE items SET archived_at = updated_at WHERE status = 'archived';

CREATE INDEX items_decision ON items (workspace_id, decision);
CREATE INDEX items_run ON items (workspace_id, run) WHERE run != 'none';

-- +goose Down
DROP INDEX items_run;
DROP INDEX items_decision;
ALTER TABLE items DROP COLUMN archived_at;
ALTER TABLE items DROP COLUMN run;
ALTER TABLE items DROP COLUMN decision;
