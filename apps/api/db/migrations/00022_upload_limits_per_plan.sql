-- +goose Up
-- Largest file per plan: Starter (the default) 10 MB, Plus 25 MB, Pro 50 MB, Business 100 MB. Files already
-- stored stay; the limit applies to new uploads. Values an operator set for one workspace are left alone.
UPDATE limits SET value = 10485760, updated_at = '2026-11-15T00:00:00Z' WHERE key = 'max_upload_bytes';
INSERT INTO limit_plans (limit_key, plan_id, value, updated_at) VALUES
    ('max_upload_bytes', 'plus', 26214400, '2026-11-15T00:00:00Z'),
    ('max_upload_bytes', 'pro', 52428800, '2026-11-15T00:00:00Z'),
    ('max_upload_bytes', 'business', 104857600, '2026-11-15T00:00:00Z')
ON CONFLICT (limit_key, plan_id) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at;

-- +goose Down
UPDATE limits SET value = 26214400 WHERE key = 'max_upload_bytes';
UPDATE limit_plans SET value = 104857600 WHERE limit_key = 'max_upload_bytes' AND plan_id IN ('plus', 'pro', 'business');
