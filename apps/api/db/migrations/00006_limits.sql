-- +goose Up
-- Plan limits live here, editable in the admin panel: a default per limit, a value per plan where it differs, and a
-- value per workspace where an operator set one (it replaces the plan's; Stripe add-ons still come on top).
-- -1 = unlimited; data_export is 1 (on) or 0 (off). Plans themselves (names, prices) stay in code (app.Plans).
CREATE TABLE limits (
    key         TEXT PRIMARY KEY,
    value       INTEGER NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    updated_at  TEXT NOT NULL
);

CREATE TABLE limit_plans (
    limit_key  TEXT NOT NULL REFERENCES limits (key) ON DELETE CASCADE,
    plan_id    TEXT NOT NULL,
    value      INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (limit_key, plan_id)
);

CREATE TABLE limit_workspaces (
    limit_key    TEXT NOT NULL REFERENCES limits (key) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    value        INTEGER NOT NULL,
    note         TEXT NOT NULL DEFAULT '',
    updated_at   TEXT NOT NULL,
    PRIMARY KEY (workspace_id, limit_key)
);

-- The defaults are Starter's; the other plans differ where they give more.
INSERT INTO limits (key, value, description, updated_at) VALUES
    ('posts_per_month', 300, 'Posts created per calendar month (by people, agents and schedules)', '2026-11-02T00:00:00Z'),
    ('channels', 5, 'Connected channels', '2026-11-02T00:00:00Z'),
    ('alerts', 3, 'Alerts', '2026-11-02T00:00:00Z'),
    ('webhooks', 3, 'Webhooks', '2026-11-02T00:00:00Z'),
    ('api_keys', 2, 'API keys', '2026-11-02T00:00:00Z'),
    ('storage_bytes', 2147483648, 'Media storage, in bytes', '2026-11-02T00:00:00Z'),
    ('max_upload_bytes', 26214400, 'Largest file to upload, in bytes', '2026-11-02T00:00:00Z'),
    ('data_export', 0, 'Backups and full exports with files (1 on, 0 off)', '2026-11-02T00:00:00Z'),
    ('x_posts_per_period', 50, 'Posts to X included per month (a post with a live link counts more)', '2026-11-02T00:00:00Z'),
    ('linkedin_posts_per_period', 100, 'Posts to LinkedIn per month (profiles and Pages)', '2026-11-02T00:00:00Z'),
    ('linkedin_videos_per_period', 5, 'Of those LinkedIn posts, how many may carry a video', '2026-11-02T00:00:00Z'),
    ('ai_monthly_cents', 100, 'AI spend per month, in US dollar cents', '2026-11-02T00:00:00Z');

INSERT INTO limit_plans (limit_key, plan_id, value, updated_at) VALUES
    ('posts_per_month', 'plus', 1500, '2026-11-02T00:00:00Z'),
    ('channels', 'plus', 15, '2026-11-02T00:00:00Z'),
    ('alerts', 'plus', 10, '2026-11-02T00:00:00Z'),
    ('webhooks', 'plus', 10, '2026-11-02T00:00:00Z'),
    ('api_keys', 'plus', 10, '2026-11-02T00:00:00Z'),
    ('storage_bytes', 'plus', 10737418240, '2026-11-02T00:00:00Z'),
    ('max_upload_bytes', 'plus', 104857600, '2026-11-02T00:00:00Z'),
    ('data_export', 'plus', 1, '2026-11-02T00:00:00Z'),
    ('x_posts_per_period', 'plus', 100, '2026-11-02T00:00:00Z'),
    ('linkedin_posts_per_period', 'plus', 300, '2026-11-02T00:00:00Z'),
    ('linkedin_videos_per_period', 'plus', 15, '2026-11-02T00:00:00Z'),
    ('ai_monthly_cents', 'plus', 200, '2026-11-02T00:00:00Z'),
    ('posts_per_month', 'pro', 5000, '2026-11-02T00:00:00Z'),
    ('channels', 'pro', 40, '2026-11-02T00:00:00Z'),
    ('alerts', 'pro', 25, '2026-11-02T00:00:00Z'),
    ('webhooks', 'pro', 25, '2026-11-02T00:00:00Z'),
    ('api_keys', 'pro', 25, '2026-11-02T00:00:00Z'),
    ('storage_bytes', 'pro', 32212254720, '2026-11-02T00:00:00Z'),
    ('max_upload_bytes', 'pro', 104857600, '2026-11-02T00:00:00Z'),
    ('data_export', 'pro', 1, '2026-11-02T00:00:00Z'),
    ('x_posts_per_period', 'pro', 250, '2026-11-02T00:00:00Z'),
    ('linkedin_posts_per_period', 'pro', 600, '2026-11-02T00:00:00Z'),
    ('linkedin_videos_per_period', 'pro', 30, '2026-11-02T00:00:00Z'),
    ('ai_monthly_cents', 'pro', 500, '2026-11-02T00:00:00Z'),
    ('posts_per_month', 'business', 15000, '2026-11-02T00:00:00Z'),
    ('channels', 'business', 100, '2026-11-02T00:00:00Z'),
    ('alerts', 'business', 50, '2026-11-02T00:00:00Z'),
    ('webhooks', 'business', 50, '2026-11-02T00:00:00Z'),
    ('api_keys', 'business', 50, '2026-11-02T00:00:00Z'),
    ('storage_bytes', 'business', 107374182400, '2026-11-02T00:00:00Z'),
    ('max_upload_bytes', 'business', 104857600, '2026-11-02T00:00:00Z'),
    ('data_export', 'business', 1, '2026-11-02T00:00:00Z'),
    ('x_posts_per_period', 'business', 500, '2026-11-02T00:00:00Z'),
    ('linkedin_posts_per_period', 'business', 900, '2026-11-02T00:00:00Z'),
    ('linkedin_videos_per_period', 'business', 60, '2026-11-02T00:00:00Z'),
    ('ai_monthly_cents', 'business', 1000, '2026-11-02T00:00:00Z');

-- +goose Down
DROP TABLE limit_workspaces;
DROP TABLE limit_plans;
DROP TABLE limits;
