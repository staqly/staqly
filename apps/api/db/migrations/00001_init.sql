-- staq.ly database schema (SQLite).
-- Conventions: timestamps are fixed-width ISO-8601 UTC text (sortable as
-- strings); JSON columns are TEXT checked with json_valid(); every row is
-- scoped by workspace_id. Add future changes as new numbered migrations.

-- +goose Up

-- ═══ Core: workspaces, auth, collections, items, media, events, webhooks, idempotency ═══

-- Timestamps are ISO-8601 UTC text (lexicographically sortable).
-- JSON columns are TEXT validated with json_valid().

-- Single-row guard that makes first-run setup race-safe: a second insert fails.
CREATE TABLE installation (
    id           INTEGER PRIMARY KEY CHECK (id = 1),
    workspace_id TEXT NOT NULL,
    created_at   TEXT NOT NULL
);

CREATE TABLE workspaces (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL,
    recovery_verifier   TEXT NOT NULL,
    recovery_rotated_at TEXT NOT NULL,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    plan                TEXT NOT NULL DEFAULT 'free'  -- cloud plan; local installs are always unlocked
);

CREATE TABLE api_keys (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    prefix       TEXT NOT NULL,           -- non-secret display prefix
    key_hash     TEXT NOT NULL UNIQUE,    -- sha256 of the full key
    scopes       TEXT NOT NULL CHECK (json_valid(scopes)),
    created_at   TEXT NOT NULL,
    last_used_at TEXT,
    expires_at   TEXT,
    revoked_at   TEXT
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
    revoked_at   TEXT
);
CREATE INDEX sessions_workspace ON sessions (workspace_id);

CREATE TABLE collections (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    icon         TEXT NOT NULL DEFAULT '',
    color        TEXT NOT NULL DEFAULT '',
    default_view TEXT NOT NULL DEFAULT 'gallery' CHECK (default_view IN ('gallery', 'table', 'list')),
    actions      TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(actions)),
    archived_at  TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);
CREATE INDEX collections_workspace ON collections (workspace_id, archived_at);
-- Agents address collections by name, so names are unique per workspace.
CREATE UNIQUE INDEX collections_name ON collections (workspace_id, name COLLATE NOCASE);

CREATE TABLE media (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    storage_name  TEXT NOT NULL UNIQUE,   -- server-generated file name, never user input
    original_name TEXT NOT NULL,
    mime_type     TEXT NOT NULL,
    size_bytes    INTEGER NOT NULL,
    sha256        TEXT NOT NULL,
    created_at    TEXT NOT NULL
);
CREATE INDEX media_workspace ON media (workspace_id);

CREATE TABLE items (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    collection_id TEXT NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
    title         TEXT NOT NULL,
    type          TEXT NOT NULL CHECK (type IN ('image', 'video', 'text', 'link', 'json', 'file')),
    content       TEXT NOT NULL DEFAULT '',
    url           TEXT NOT NULL DEFAULT '',
    media_id      TEXT REFERENCES media (id) ON DELETE SET NULL,
    metadata      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata)),
    status        TEXT NOT NULL,
    source        TEXT NOT NULL DEFAULT '',
    external_ref  TEXT NOT NULL DEFAULT '',
    feedback      TEXT NOT NULL DEFAULT '',
    version       INTEGER NOT NULL DEFAULT 1,
    decided_at    TEXT,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    expires_at    TEXT,                  -- pending items expire after this (expires_in / expire rule)
    editable      TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(editable))  -- fields editable before approval
);
CREATE INDEX items_collection ON items (collection_id, created_at);
CREATE INDEX items_workspace_status ON items (workspace_id, status, created_at);
CREATE INDEX items_workspace_decided ON items (workspace_id, decided_at);
CREATE INDEX items_external_ref ON items (workspace_id, external_ref) WHERE external_ref <> '';

-- Snapshot of an item taken before every change.
CREATE TABLE item_versions (
    item_id    TEXT NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    version    INTEGER NOT NULL,
    snapshot   TEXT NOT NULL CHECK (json_valid(snapshot)),
    actor      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (item_id, version)
);

-- Immutable activity log. No FK to items so history outlives deleted items.
CREATE TABLE events (
    seq           INTEGER PRIMARY KEY AUTOINCREMENT,
    id            TEXT NOT NULL UNIQUE,
    workspace_id  TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    type          TEXT NOT NULL,
    collection_id TEXT,
    item_id       TEXT,
    actor_type    TEXT NOT NULL,          -- session | api_key | system
    actor_id      TEXT NOT NULL,
    actor_name    TEXT NOT NULL,
    data          TEXT NOT NULL CHECK (json_valid(data)),
    created_at    TEXT NOT NULL
);
CREATE INDEX events_workspace ON events (workspace_id, seq);
CREATE INDEX events_item ON events (item_id, seq);

-- +goose StatementBegin
CREATE TRIGGER events_no_update BEFORE UPDATE ON events
BEGIN
    SELECT RAISE(ABORT, 'events are immutable');
END;
-- +goose StatementEnd

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

-- Transactional outbox: rows are inserted in the same transaction as the event.
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

-- ═══ Search (FTS5), plans and usage ═══

-- Full-text search over items. External-content FTS5 keyed by the items
-- rowid; triggers keep it in sync. (Backups made with VACUUM INTO rebuild the
-- index in the copy, because VACUUM may renumber rowids.)
CREATE VIRTUAL TABLE items_fts USING fts5(
    title, content, metadata,
    content = 'items', content_rowid = 'rowid',
    tokenize = 'unicode61 remove_diacritics 2'
);

-- +goose StatementBegin
CREATE TRIGGER items_fts_ai AFTER INSERT ON items BEGIN
    INSERT INTO items_fts (rowid, title, content, metadata) VALUES (new.rowid, new.title, new.content, new.metadata);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER items_fts_ad AFTER DELETE ON items BEGIN
    INSERT INTO items_fts (items_fts, rowid, title, content, metadata) VALUES ('delete', old.rowid, old.title, old.content, old.metadata);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER items_fts_au AFTER UPDATE OF title, content, metadata ON items BEGIN
    INSERT INTO items_fts (items_fts, rowid, title, content, metadata) VALUES ('delete', old.rowid, old.title, old.content, old.metadata);
    INSERT INTO items_fts (rowid, title, content, metadata) VALUES (new.rowid, new.title, new.content, new.metadata);
END;
-- +goose StatementEnd

-- Metered usage per workspace and calendar month (UTC), e.g. items created.
CREATE TABLE usage_counters (
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    period       TEXT NOT NULL,           -- YYYY-MM
    metric       TEXT NOT NULL,
    count        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (workspace_id, period, metric)
);

-- ═══ Device links ("Connect a device" QR) ═══

-- One-time links (shown as QR codes) that sign another device into a workspace.
CREATE TABLE device_links (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    created_by   TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    used_at      TEXT
);

-- ═══ Rules and watchers ═══

-- Approval rules: evaluated once when an item is created, by priority; the
-- first match decides (auto-approve, auto-reject, set status, expiry, escalate).
CREATE TABLE rules (
    id             TEXT PRIMARY KEY,
    workspace_id   TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    priority       INTEGER NOT NULL DEFAULT 100,
    enabled        INTEGER NOT NULL DEFAULT 1,
    match          TEXT NOT NULL CHECK (json_valid(match)),   -- {collection_ids, types, source_pattern, text_contains, conditions[]}
    action         TEXT NOT NULL,
    params         TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(params)),
    times_applied  INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE INDEX rules_workspace ON rules (workspace_id, enabled, priority);

CREATE INDEX items_expiry ON items (expires_at) WHERE expires_at IS NOT NULL;

-- Watchers poll external sources (RSS/Atom, page changes) and turn new
-- entries into pending items in a collection.
CREATE TABLE watchers (
    id                TEXT PRIMARY KEY,
    workspace_id      TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    collection_id     TEXT NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    kind              TEXT NOT NULL CHECK (kind IN ('rss', 'url_diff')),
    url               TEXT NOT NULL,
    keywords          TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(keywords)),
    exclude_keywords  TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(exclude_keywords)),
    every_seconds     INTEGER NOT NULL,
    preset_id         TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'degraded', 'paused')),
    baseline_done     INTEGER NOT NULL DEFAULT 0,
    content_hash      TEXT NOT NULL DEFAULT '',
    fail_count        INTEGER NOT NULL DEFAULT 0,
    degraded_since    TEXT,
    last_error        TEXT NOT NULL DEFAULT '',
    last_run_at       TEXT,
    next_run_at       TEXT NOT NULL,
    items_created     INTEGER NOT NULL DEFAULT 0,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL
);
CREATE INDEX watchers_due ON watchers (status, next_run_at);
CREATE INDEX watchers_workspace ON watchers (workspace_id);

-- Entries a watcher has already seen (dedup); pruned after 90 days.
CREATE TABLE watcher_seen (
    watcher_id TEXT NOT NULL REFERENCES watchers (id) ON DELETE CASCADE,
    entry_hash TEXT NOT NULL,
    seen_at    TEXT NOT NULL,
    PRIMARY KEY (watcher_id, entry_hash)
);

-- ═══ Approval core ═══

-- ═══ Notification channels and outbox ═══

-- Notification channels (Telegram, Slack, Discord, ntfy, email) per workspace.
-- `config` holds non-secret settings; `secrets` is an AES-256-GCM encrypted
-- JSON blob (bot tokens, webhook URLs, SMTP passwords, signing secrets and the
-- per-channel callback key). Secrets never leave the server unmasked.
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
    last_sent_at          TEXT,
    telegram_offset       INTEGER NOT NULL DEFAULT 0, -- getUpdates offset (long-polling mode)
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL
);
CREATE INDEX notification_channels_workspace ON notification_channels (workspace_id, enabled);
CREATE INDEX notification_channels_type ON notification_channels (type, enabled);

-- Transactional outbox: one row per (channel, event), written in the event's
-- transaction. The worker batches all pending rows of a channel into one
-- message once the oldest is due (digest window), with retries and backoff.
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

-- ═══ One-tap decision links ═══

-- One-tap decision links sent in notifications. Only the SHA-256 of the token
-- is stored; a link is single-use (used_at) and expires.
CREATE TABLE decision_links (
    token_hash   TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    item_id      TEXT NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    channel_id   TEXT REFERENCES notification_channels (id) ON DELETE SET NULL,
    channel_name TEXT NOT NULL,
    action       TEXT NOT NULL CHECK (action IN ('approve', 'reject')),
    expires_at   TEXT NOT NULL,
    used_at      TEXT,
    created_at   TEXT NOT NULL
);
CREATE INDEX decision_links_expiry ON decision_links (expires_at);
CREATE INDEX decision_links_item ON decision_links (item_id);

-- ═══ Workspace exports and retention indexes ═══

-- Workspace data exports (GDPR portability): built asynchronously into a ZIP
-- under DATA_DIR/exports and deleted after expires_at.
CREATE TABLE workspace_exports (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'ready', 'failed', 'expired')),
    actor_type   TEXT NOT NULL,
    actor_id     TEXT NOT NULL,
    actor_name   TEXT NOT NULL,
    size_bytes   INTEGER NOT NULL DEFAULT 0,
    error        TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    started_at   TEXT,
    finished_at  TEXT,
    expires_at   TEXT NOT NULL
);
CREATE INDEX workspace_exports_workspace ON workspace_exports (workspace_id, created_at);
CREATE INDEX workspace_exports_status ON workspace_exports (status, created_at);
-- At most one export in progress per workspace.
CREATE UNIQUE INDEX workspace_exports_active ON workspace_exports (workspace_id) WHERE status IN ('pending', 'running');

-- Retention prunes by age.
CREATE INDEX events_workspace_created ON events (workspace_id, created_at);
CREATE INDEX webhook_deliveries_event ON webhook_deliveries (event_id);

-- +goose Down
DROP TRIGGER items_fts_au;
DROP TRIGGER items_fts_ad;
DROP TRIGGER items_fts_ai;
DROP TRIGGER events_no_update;
DROP TABLE workspace_exports;
DROP TABLE decision_links;
DROP TABLE notification_outbox;
DROP TABLE notification_channels;
DROP TABLE watcher_seen;
DROP TABLE watchers;
DROP TABLE rules;
DROP TABLE device_links;
DROP TABLE usage_counters;
DROP TABLE items_fts;
DROP TABLE idempotency_keys;
DROP TABLE webhook_deliveries;
DROP TABLE webhooks;
DROP TABLE events;
DROP TABLE item_versions;
DROP TABLE items;
DROP TABLE media;
DROP TABLE collections;
DROP TABLE sessions;
DROP TABLE api_keys;
DROP TABLE workspaces;
DROP TABLE installation;
