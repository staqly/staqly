-- +goose Up
-- Business takes files up to 90 MB (was 100 MB), the most any workspace may upload (MAX_UPLOAD_MB's ceiling): every
-- file then fits through the dashboard's proxy. Files already stored stay; a value an operator set for one
-- workspace is left alone.
UPDATE limit_plans SET value = 94371840, updated_at = '2026-11-15T00:00:00Z'
WHERE limit_key = 'max_upload_bytes' AND plan_id = 'business';

-- +goose Down
UPDATE limit_plans SET value = 104857600 WHERE limit_key = 'max_upload_bytes' AND plan_id = 'business';
