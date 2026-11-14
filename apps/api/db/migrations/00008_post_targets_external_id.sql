-- +goose Up
-- Meta's webhooks name the post a comment is under by its id on the platform: find the target from it.
CREATE INDEX post_targets_external_id ON post_targets (external_id);

-- +goose Down
DROP INDEX post_targets_external_id;
