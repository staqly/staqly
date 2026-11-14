-- +goose Up
CREATE TABLE product_feedback_attachments (
    feedback_id TEXT NOT NULL REFERENCES product_feedback(id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK (position >= 0 AND position < 10),
    storage_key TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    PRIMARY KEY (feedback_id, position)
);
INSERT INTO product_feedback_attachments (feedback_id, position, storage_key, mime_type)
SELECT id, 0, attachment_key, attachment_type FROM product_feedback WHERE attachment_key <> '';

-- +goose Down
DROP TABLE product_feedback_attachments;
