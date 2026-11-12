-- +goose Up
-- Unified login is Instagram ("instagram") again, also when it is offered next to Instagram login as Instagram
-- (Professional): 00018's instagram_unified is gone, and Instagram login is the provider of its own,
-- instagram_standalone (channels connected with it are moved at start: only their sealed credentials say so).
UPDATE channels SET provider = 'instagram' WHERE provider IN ('instagram_unified', 'instagram_business');
DELETE FROM provider_apps WHERE provider IN ('instagram_unified', 'instagram_business')
  AND EXISTS (SELECT 1 FROM provider_apps a WHERE a.workspace_id = provider_apps.workspace_id AND a.provider = 'instagram');
UPDATE provider_apps SET provider = 'instagram' WHERE provider IN ('instagram_unified', 'instagram_business');

-- +goose Down
-- Nothing to undo: which channels came from the second tile is no longer known.
SELECT 1;
