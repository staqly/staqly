-- +goose Up
-- The whole schema, for SQLite (local mode) and PostgreSQL (cloud mode) alike. Before the first release the
-- history (00001-00047) was squashed into this one file; a database made before the squash is at version 45
-- and store.baselineSquash records this file as applied there. From the first release on, migrations are
-- append-only: add 00002_<name>.sql.
--
-- Written in SQLite's dialect. PostgreSQL reads it through store.postgresDDL: INTEGER becomes BIGINT, BLOB
-- becomes BYTEA, json_valid(x) becomes x IS JSON, lines between "-- +sqlite" and "-- +end" are dropped and
-- lines between "-- +postgres" and "-- +end" lose their leading "-- " (and say @goose for a goose annotation,
-- which goose would otherwise read in the SQLite run too). Text and fixed-width UTC timestamps
-- are TEXT on both (compared as strings), 0/1 flags are integers. Column order matters: SELECT * is scanned
-- by position. The job queue's tables (river_*) come from River's own migrator, after this file.

CREATE TABLE installation (
    id           INTEGER PRIMARY KEY CHECK (id = 1),
    workspace_id TEXT NOT NULL,
    created_at   TEXT NOT NULL
);

CREATE TABLE users (
    id                 TEXT PRIMARY KEY,
    email              TEXT NOT NULL,
    name               TEXT NOT NULL DEFAULT '',
    avatar_url         TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    last_login_at      TEXT,
    onboarded_at       TEXT,
    notification_prefs TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(notification_prefs))
);

CREATE TABLE accounts (
    id            TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL UNIQUE REFERENCES users (id),
    name          TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE workspaces (
    id                         TEXT PRIMARY KEY,
    name                       TEXT NOT NULL,
    recovery_verifier          TEXT NOT NULL,
    recovery_rotated_at        TEXT NOT NULL,
    created_at                 TEXT NOT NULL,
    updated_at                 TEXT NOT NULL,
    plan                       TEXT NOT NULL DEFAULT 'free',  -- cloud plan; local installs are always unlocked
    plan_expires_at            TEXT,
    stripe_customer_id         TEXT,
    stripe_subscription_id     TEXT,
    subscription_status        TEXT,
    processing_timeout_seconds INTEGER,
    account_id                 TEXT REFERENCES accounts (id),
    onboarded_at               TEXT,
    -- Stripe Entitlements: the features the subscription grants (plan features, add-ons, one customer's deal),
    -- a JSON array of {lookup_key, name, metadata}, written by the sync_entitlements job, read with the plan.
    entitlements               TEXT NOT NULL DEFAULT '[]'
);

CREATE INDEX workspaces_stripe_customer ON workspaces (stripe_customer_id);

CREATE INDEX workspaces_account ON workspaces (account_id) WHERE account_id IS NOT NULL;

CREATE TABLE api_keys (
    id                 TEXT PRIMARY KEY,
    workspace_id       TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    prefix             TEXT NOT NULL,           -- non-secret display prefix
    key_hash           TEXT NOT NULL UNIQUE,    -- sha256 of the full key
    scopes             TEXT NOT NULL CHECK (json_valid(scopes)),
    created_at         TEXT NOT NULL,
    last_used_at       TEXT,
    expires_at         TEXT,
    revoked_at         TEXT,
    created_by_user_id TEXT
);

CREATE INDEX api_keys_workspace ON api_keys (workspace_id);

CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    csrf_token   TEXT NOT NULL,
    user_agent   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    revoked_at   TEXT,
    user_id      TEXT REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX sessions_workspace ON sessions (workspace_id);

CREATE INDEX sessions_user ON sessions (user_id) WHERE user_id IS NOT NULL;

CREATE TABLE collections (
    id                         TEXT PRIMARY KEY,
    workspace_id               TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name                       TEXT NOT NULL,
    description                TEXT NOT NULL DEFAULT '',
    icon                       TEXT NOT NULL DEFAULT '',
    color                      TEXT NOT NULL DEFAULT '',
    default_view               TEXT NOT NULL DEFAULT 'gallery' CHECK (default_view IN ('gallery', 'table', 'list')),
    actions                    TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(actions)),
    archived_at                TEXT,
    created_at                 TEXT NOT NULL,
    updated_at                 TEXT NOT NULL,
    is_default                 INTEGER NOT NULL DEFAULT 0,
    expires_in                 INTEGER,
    processing_timeout_seconds INTEGER
);

CREATE INDEX collections_workspace ON collections (workspace_id, archived_at);

-- +sqlite
CREATE UNIQUE INDEX collections_name ON collections (workspace_id, name COLLATE NOCASE);
-- +end
-- +postgres
-- CREATE UNIQUE INDEX collections_name ON collections (workspace_id, lower(name));
-- +end

CREATE UNIQUE INDEX collections_default ON collections (workspace_id) WHERE is_default = 1;

CREATE TABLE media (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    storage_name  TEXT NOT NULL UNIQUE,   -- server-generated file name, never user input
    original_name TEXT NOT NULL,
    mime_type     TEXT NOT NULL,
    size_bytes    INTEGER NOT NULL,
    sha256        TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    alt_text      TEXT NOT NULL DEFAULT '',
    width         INTEGER NOT NULL DEFAULT 0,
    height        INTEGER NOT NULL DEFAULT 0,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    folder_id     TEXT NOT NULL DEFAULT '',
    tags          TEXT NOT NULL DEFAULT '[]',
    deleted_at    TEXT
);

CREATE INDEX media_workspace ON media (workspace_id);

CREATE INDEX media_workspace_created ON media (workspace_id, created_at);

CREATE INDEX media_workspace_folder ON media (workspace_id, folder_id);

CREATE INDEX media_workspace_deleted ON media (workspace_id, deleted_at);

CREATE INDEX media_workspace_sha256 ON media (workspace_id, sha256);

CREATE TABLE events (
-- +sqlite
    seq           INTEGER PRIMARY KEY AUTOINCREMENT,
-- +end
-- +postgres
--     seq           BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
-- +end
    id            TEXT NOT NULL UNIQUE,
    workspace_id  TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    type          TEXT NOT NULL,
    collection_id TEXT,
    item_id       TEXT,
    actor_type    TEXT NOT NULL,          -- session | api_key | system
    actor_id      TEXT NOT NULL,
    actor_name    TEXT NOT NULL,
    data          TEXT NOT NULL CHECK (json_valid(data)),
    created_at    TEXT NOT NULL,
    agent         TEXT NOT NULL DEFAULT '',
    channel_id    TEXT,
    schedule_id   TEXT
);

CREATE INDEX events_workspace ON events (workspace_id, seq);

CREATE INDEX events_item ON events (workspace_id, item_id, seq) WHERE item_id IS NOT NULL;

-- +sqlite
-- +goose StatementBegin
CREATE TRIGGER events_no_update BEFORE UPDATE ON events
BEGIN
    SELECT RAISE(ABORT, 'events are immutable');
END;
-- +goose StatementEnd
-- +end
-- +postgres
-- -- @goose StatementBegin
-- CREATE FUNCTION events_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
-- BEGIN
--     RAISE EXCEPTION 'events are immutable';
-- END;
-- $$;
-- -- @goose StatementEnd
--
-- CREATE TRIGGER events_no_update BEFORE UPDATE ON events FOR EACH ROW EXECUTE FUNCTION events_immutable();
-- +end

CREATE INDEX events_workspace_created ON events (workspace_id, created_at);

CREATE TABLE webhooks (
    id             TEXT PRIMARY KEY,
    workspace_id   TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    url            TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    secret         TEXT NOT NULL,         -- needed in clear to sign payloads
    events         TEXT NOT NULL CHECK (json_valid(events)),
    collection_ids TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(collection_ids)),
    active         INTEGER NOT NULL DEFAULT 1,
    timeout_ms     INTEGER NOT NULL DEFAULT 10000,
    max_attempts   INTEGER NOT NULL DEFAULT 8,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);

CREATE INDEX webhooks_workspace ON webhooks (workspace_id);

CREATE TABLE webhook_deliveries (
    id               TEXT PRIMARY KEY,
    workspace_id     TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    webhook_id       TEXT NOT NULL REFERENCES webhooks (id) ON DELETE CASCADE,
    event_id         TEXT NOT NULL REFERENCES events (id),
    event_type       TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'succeeded', 'failed')),
    attempts         INTEGER NOT NULL DEFAULT 0,
    next_attempt_at  TEXT NOT NULL,
    last_status_code INTEGER,
    last_error       TEXT NOT NULL DEFAULT '',
    last_response    TEXT NOT NULL DEFAULT '',
    last_attempt_at  TEXT,
    delivered_at     TEXT,
    created_at       TEXT NOT NULL,
    UNIQUE (webhook_id, event_id)
);

CREATE INDEX webhook_deliveries_due ON webhook_deliveries (status, next_attempt_at);

CREATE INDEX webhook_deliveries_webhook ON webhook_deliveries (webhook_id, created_at);

CREATE INDEX webhook_deliveries_workspace ON webhook_deliveries (workspace_id, created_at);

CREATE INDEX webhook_deliveries_event ON webhook_deliveries (event_id);

CREATE TABLE idempotency_keys (
    workspace_id  TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    key           TEXT NOT NULL,
    principal_id  TEXT NOT NULL,
    request_hash  TEXT NOT NULL,
    status_code   INTEGER,                -- NULL while the first request is in flight
    response_body BLOB,
    created_at    TEXT NOT NULL,
    PRIMARY KEY (workspace_id, principal_id, key)
);

CREATE INDEX idempotency_keys_created ON idempotency_keys (created_at);

CREATE TABLE usage_counters (
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    period       TEXT NOT NULL,           -- YYYY-MM
    metric       TEXT NOT NULL,
    count        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (workspace_id, period, metric)
);

CREATE TABLE device_links (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    created_by   TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    used_at      TEXT,
    user_id      TEXT NOT NULL DEFAULT ''
);

CREATE TABLE notification_channels (
    id                    TEXT PRIMARY KEY,
    workspace_id          TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name                  TEXT NOT NULL,
    type                  TEXT NOT NULL CHECK (type IN ('telegram', 'slack', 'discord', 'ntfy', 'email')),
    enabled               INTEGER NOT NULL DEFAULT 1,
    config                TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(config)),
    secrets               TEXT NOT NULL DEFAULT '',
    events                TEXT NOT NULL DEFAULT '["item.created","item.escalated"]' CHECK (json_valid(events)),
    collection_ids        TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(collection_ids)),
    digest_window_seconds INTEGER NOT NULL DEFAULT 0,
    fail_count            INTEGER NOT NULL DEFAULT 0,
    last_error            TEXT NOT NULL DEFAULT '',
    disabled_reason       TEXT NOT NULL DEFAULT '',
    disabled_at           TEXT, -- when the channel was last disabled (NULL while enabled)
    last_sent_at          TEXT,
    telegram_offset       INTEGER NOT NULL DEFAULT 0, -- getUpdates offset (long-polling mode)
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL
);

CREATE INDEX notification_channels_workspace ON notification_channels (workspace_id, enabled);

CREATE INDEX notification_channels_type ON notification_channels (type, enabled);

CREATE TABLE notification_outbox (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    channel_id      TEXT NOT NULL REFERENCES notification_channels (id) ON DELETE CASCADE,
    event_id        TEXT NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    event_type      TEXT NOT NULL,
    item_id         TEXT,
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed', 'skipped')),
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT NOT NULL,
    last_error      TEXT NOT NULL DEFAULT '',
    sent_at         TEXT,
    created_at      TEXT NOT NULL,
    UNIQUE (channel_id, event_id)
);

CREATE INDEX notification_outbox_due ON notification_outbox (status, next_attempt_at);

CREATE INDEX notification_outbox_channel ON notification_outbox (channel_id, status, created_at);

CREATE INDEX notification_outbox_event ON notification_outbox (event_id);

CREATE INDEX notification_outbox_workspace ON notification_outbox (workspace_id);

CREATE TABLE workspace_exports (
    id             TEXT PRIMARY KEY,
    workspace_id   TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'ready', 'failed', 'expired')),
    actor_type     TEXT NOT NULL,
    actor_id       TEXT NOT NULL,
    actor_name     TEXT NOT NULL,
    size_bytes     INTEGER NOT NULL DEFAULT 0,
    error          TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    started_at     TEXT,
    finished_at    TEXT,
    expires_at     TEXT NOT NULL,
    includes_media INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX workspace_exports_workspace ON workspace_exports (workspace_id, created_at);

CREATE INDEX workspace_exports_status ON workspace_exports (status, created_at);

CREATE UNIQUE INDEX workspace_exports_active ON workspace_exports (workspace_id) WHERE status IN ('pending', 'running');

CREATE TABLE stripe_events (
  id         TEXT PRIMARY KEY,
  created_at TEXT NOT NULL
);

CREATE TABLE agents (
    id             TEXT PRIMARY KEY,
    workspace_id   TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    kind           TEXT NOT NULL,             -- mcp | api
    name           TEXT NOT NULL,
    version        TEXT NOT NULL DEFAULT '',
    principal_id   TEXT NOT NULL,             -- the API key id, or "local"
    principal_name TEXT NOT NULL DEFAULT '',
    first_seen_at  TEXT NOT NULL,
    last_seen_at   TEXT NOT NULL,
    requests       INTEGER NOT NULL DEFAULT 0,
    items_created  INTEGER NOT NULL DEFAULT 0,
    UNIQUE (workspace_id, kind, name, principal_id)
);

CREATE TABLE channels (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    provider     TEXT NOT NULL,                       -- registry id: custom, mastodon, bluesky, ...
    name         TEXT NOT NULL,                       -- what people call it ("Brand on Mastodon")
    handle       TEXT NOT NULL DEFAULT '',            -- @handle / page name from the provider
    avatar_url   TEXT NOT NULL DEFAULT '',
    profile_url  TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'connected' CHECK (status IN ('connected', 'needs_reconnect', 'disabled')),
    credentials  TEXT NOT NULL DEFAULT '',            -- AES-256-GCM sealed JSON (tokens, refresh token, expiry, instance URL)
    settings     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(settings)),
    last_error   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    group_id     TEXT NOT NULL DEFAULT ''
);

CREATE INDEX channels_workspace ON channels (workspace_id, provider);

CREATE TABLE provider_apps (
    workspace_id  TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    provider      TEXT NOT NULL,
    credentials   TEXT NOT NULL,                       -- sealed JSON {client_id, client_secret}
    updated_at    TEXT NOT NULL,
    PRIMARY KEY (workspace_id, provider)
);

CREATE TABLE posts (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    collection_id   TEXT NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
    content         TEXT NOT NULL DEFAULT '',
    media           TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(media)),     -- ordered media ids
    status          TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'scheduled', 'publishing', 'published', 'failed')),
    publish_at      TEXT,
    published_at    TEXT,
    tags            TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tags)),
    metadata        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata)),
    source          TEXT NOT NULL DEFAULT '',
    external_ref    TEXT NOT NULL DEFAULT '',
    version         INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    repeat_every    TEXT NOT NULL DEFAULT '',
    repeat_of       TEXT NOT NULL DEFAULT '',
    thread          TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(thread)),
    created_by_user TEXT NOT NULL DEFAULT ''
);

CREATE INDEX posts_calendar ON posts (workspace_id, publish_at);

CREATE INDEX posts_collection ON posts (workspace_id, collection_id, status);

CREATE INDEX posts_created ON posts (workspace_id, created_at, id);

CREATE INDEX posts_repeat_of ON posts (repeat_of) WHERE repeat_of <> '';

CREATE TABLE post_targets (
    post_id       TEXT NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    channel_id    TEXT NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    content       TEXT,                                -- override of the post's content (null = base)
    settings      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(settings)),
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'publishing', 'published', 'failed')),
    external_id   TEXT NOT NULL DEFAULT '',
    external_url  TEXT NOT NULL DEFAULT '',
    error         TEXT NOT NULL DEFAULT '',
    attempts      INTEGER NOT NULL DEFAULT 0,
    published_at  TEXT,
    updated_at    TEXT NOT NULL, replies TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(replies)), metrics TEXT NOT NULL DEFAULT '', metrics_at TEXT,
    PRIMARY KEY (post_id, channel_id)
);

CREATE INDEX post_targets_channel ON post_targets (channel_id, status);

CREATE TABLE schedules (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    collection_id   TEXT NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    channels        TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(channels)),
    kind            TEXT NOT NULL CHECK (kind IN ('once', 'recurring')),
    run_at          TEXT,
    rule            TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(rule)),
    pick            TEXT NOT NULL DEFAULT 'next' CHECK (pick IN ('next', 'selected', 'all')),
    post_ids        TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(post_ids)),
    source          TEXT NOT NULL DEFAULT 'none' CHECK (source IN ('none', 'agent', 'endpoint')),
    endpoint_url    TEXT NOT NULL DEFAULT '',
    endpoint_secret TEXT NOT NULL DEFAULT '',
    enabled         INTEGER NOT NULL DEFAULT 1,
    next_run_at     TEXT,
    last_run_at     TEXT,
    last_result     TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX schedules_due ON schedules (enabled, next_run_at);

CREATE INDEX schedules_collection ON schedules (workspace_id, collection_id);

CREATE TABLE jobs (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    kind         TEXT NOT NULL,                       -- publish_post, publish_target, run_schedule, refresh_channel
    key          TEXT NOT NULL UNIQUE,                -- one live job per subject
    payload      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload)),
    status       TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed', 'cancelled')),
    run_at       TEXT NOT NULL,
    attempts     INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    lease_until  TEXT,
    leased_by    TEXT NOT NULL DEFAULT '',
    last_error   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    done_at      TEXT,
    lease_token  TEXT NOT NULL DEFAULT '',
    requeued     INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX jobs_due ON jobs (status, run_at);

CREATE TABLE user_identities (
    provider      TEXT NOT NULL,
    subject       TEXT NOT NULL,
    user_id       TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email         TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    last_login_at TEXT,
    PRIMARY KEY (provider, subject)
);

CREATE INDEX user_identities_user ON user_identities (user_id);

CREATE TABLE workspace_members (
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role         TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'editor', 'viewer')),
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    PRIMARY KEY (workspace_id, user_id)
);

CREATE INDEX workspace_members_user ON workspace_members (user_id);

CREATE TABLE workspace_invitations (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email        TEXT NOT NULL,
    role         TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'editor', 'viewer')),
    token_hash   TEXT NOT NULL UNIQUE,
    invited_by   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    accepted_at  TEXT,
    accepted_by  TEXT,
    revoked_at   TEXT
);

CREATE UNIQUE INDEX workspace_invitations_open ON workspace_invitations (workspace_id, email)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

CREATE TABLE email_codes (
    id          TEXT PRIMARY KEY,
    email       TEXT NOT NULL,
    purpose     TEXT NOT NULL CHECK (purpose IN ('sign_in', 'link', 'change_current', 'change_new')),
    user_id     TEXT NOT NULL DEFAULT '',
    code_hash   TEXT NOT NULL,
    attempts    INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL,
    verified_at TEXT,
    used_at     TEXT
);

CREATE INDEX email_codes_email ON email_codes (email, created_at);

CREATE TABLE passkeys (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    credential_id TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    credential    TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    last_used_at  TEXT
);

CREATE INDEX passkeys_user ON passkeys (user_id);

CREATE TABLE passkey_challenges (
    id         TEXT PRIMARY KEY,
    purpose    TEXT NOT NULL CHECK (purpose IN ('register', 'sign_in')),
    user_id    TEXT NOT NULL DEFAULT '',
    session    TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

CREATE INDEX passkey_challenges_created ON passkey_challenges (created_at);

CREATE TABLE preview_links (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    post_id      TEXT NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    created_by   TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   TEXT,
    revoked_at   TEXT
);

CREATE INDEX preview_links_post ON preview_links (post_id);

CREATE TABLE post_comments (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    post_id      TEXT NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    preview_id   TEXT NOT NULL DEFAULT '',
    author_name  TEXT NOT NULL,
    author_email TEXT NOT NULL DEFAULT '',
    body         TEXT NOT NULL,
    quote        TEXT NOT NULL DEFAULT '',
    channel_id   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    parent_id    TEXT NOT NULL DEFAULT '',
    part         INTEGER NOT NULL DEFAULT 0,
    anchor_start INTEGER NOT NULL DEFAULT -1,
    anchor_end   INTEGER NOT NULL DEFAULT -1,
    resolved_at  TEXT,
    resolved_by  TEXT NOT NULL DEFAULT '',
    author_user  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX post_comments_post ON post_comments (post_id, created_at);

CREATE TABLE job_requeues (
    key           TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL,
    kind          TEXT NOT NULL,
    payload       TEXT NOT NULL DEFAULT '{}',
    run_at        TEXT NOT NULL,
    max_attempts  INTEGER NOT NULL DEFAULT 3,
    created_at    TEXT NOT NULL
);

CREATE INDEX job_requeues_workspace ON job_requeues (workspace_id);

CREATE TABLE channel_groups (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    color        TEXT NOT NULL DEFAULT '',
    position     INTEGER NOT NULL DEFAULT 0,
    is_default   INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE INDEX channel_groups_workspace ON channel_groups (workspace_id, position);

CREATE UNIQUE INDEX channel_groups_default ON channel_groups (workspace_id) WHERE is_default = 1;

CREATE TABLE channel_links (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    note         TEXT NOT NULL DEFAULT '',
    providers    TEXT NOT NULL DEFAULT '[]',
    group_id     TEXT NOT NULL DEFAULT '',
    max_uses     INTEGER NOT NULL DEFAULT 1,
    uses         INTEGER NOT NULL DEFAULT 0,
    created_by   TEXT NOT NULL,
    created_by_name TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    revoked_at   TEXT
);

CREATE INDEX channel_links_workspace ON channel_links (workspace_id, created_at);

CREATE TABLE media_folders (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    parent_id    TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL,
    position     INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE INDEX media_folders_parent ON media_folders (workspace_id, parent_id);

CREATE TABLE media_projects (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  source_ids TEXT NOT NULL DEFAULT '[]',
  output_id TEXT NOT NULL DEFAULT '',
  storage_name TEXT NOT NULL,
  size_bytes INTEGER NOT NULL,
  created_by TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX media_projects_workspace_updated ON media_projects (workspace_id, updated_at);

CREATE TABLE workspace_ai (
  workspace_id TEXT PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  credentials TEXT NOT NULL,
  key_hint TEXT NOT NULL,
  updated_by TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);

-- usage_events: one row per published post or thread reply to a metered provider (X); the primary key makes
-- metering idempotent (a retried job counts once). units: posts as the provider bills them (a post with a live
-- link counts more on X); included and credits: where they were drawn from; overage: neither had room.
CREATE TABLE usage_events (
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    provider     TEXT NOT NULL,
    ref          TEXT NOT NULL,
    period       TEXT NOT NULL,           -- YYYY-MM, the included quota's month
    units        INTEGER NOT NULL,
    included     INTEGER NOT NULL,
    credits      INTEGER NOT NULL,
    overage      INTEGER NOT NULL,
    created_at   TEXT NOT NULL,
    PRIMARY KEY (workspace_id, provider, ref)
);

-- credit_grants: bought bundles (or an operator's grant). source is unique (the Stripe Checkout session), so a
-- retried webhook grants once. They expire at the end of the billing period they were bought in.
CREATE TABLE credit_grants (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    provider     TEXT NOT NULL,
    units        INTEGER NOT NULL,
    used         INTEGER NOT NULL DEFAULT 0,
    source       TEXT NOT NULL UNIQUE,
    expires_at   TEXT NOT NULL,
    created_at   TEXT NOT NULL
);
CREATE INDEX credit_grants_live ON credit_grants (workspace_id, provider, expires_at);
