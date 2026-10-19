-- +goose Up
-- The posts pivot (docs/posts-pivot.md): staqly schedules posts to channels instead of reviewing
-- agent items. Clean start: the review-era tables go, the posts domain arrives. Collections stay
-- (expires_in is unread from now on; processing_timeout_seconds bounds the wait for a custom target).
DROP TABLE IF EXISTS decision_links;
DROP TABLE IF EXISTS watcher_seen;
DROP TABLE IF EXISTS watcher_collections;
DROP TABLE IF EXISTS watchers;
DROP TABLE IF EXISTS rules;
DROP TABLE IF EXISTS item_tags;
DROP TABLE IF EXISTS item_versions;
DROP TABLE IF EXISTS items;
DROP TABLE IF EXISTS items_fts;

CREATE TABLE channels (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    provider      TEXT NOT NULL,                       -- registry id: custom, mastodon, bluesky, ...
    name          TEXT NOT NULL,                       -- what people call it ("Brand on Mastodon")
    handle        TEXT NOT NULL DEFAULT '',            -- @handle / page name from the provider
    avatar_url    TEXT NOT NULL DEFAULT '',
    profile_url   TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'connected' CHECK (status IN ('connected', 'needs_reconnect', 'disabled')),
    credentials   TEXT NOT NULL DEFAULT '',            -- AES-256-GCM sealed JSON (tokens, refresh token, expiry, instance URL)
    settings      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(settings)),
    last_error    TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE INDEX channels_workspace ON channels (workspace_id, provider);

-- Developer apps: a workspace's own OAuth client per provider (local mode); cloud uses server env.
CREATE TABLE provider_apps (
    workspace_id  TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    provider      TEXT NOT NULL,
    credentials   TEXT NOT NULL,                       -- sealed JSON {client_id, client_secret}
    updated_at    TEXT NOT NULL,
    PRIMARY KEY (workspace_id, provider)
);

CREATE TABLE posts (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    collection_id TEXT NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
    content       TEXT NOT NULL DEFAULT '',
    media         TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(media)),     -- ordered media ids
    status        TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'scheduled', 'publishing', 'published', 'failed')),
    publish_at    TEXT,
    published_at  TEXT,
    tags          TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tags)),
    metadata      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata)),
    source        TEXT NOT NULL DEFAULT '',
    external_ref  TEXT NOT NULL DEFAULT '',
    version       INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE INDEX posts_calendar ON posts (workspace_id, publish_at);
CREATE INDEX posts_collection ON posts (workspace_id, collection_id, status);
CREATE INDEX posts_created ON posts (workspace_id, created_at, id);

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
    updated_at    TEXT NOT NULL,
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
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL,
    kind          TEXT NOT NULL,                       -- publish_post, publish_target, run_schedule, refresh_channel
    key           TEXT NOT NULL UNIQUE,                -- one live job per subject
    payload       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload)),
    status        TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed', 'cancelled')),
    run_at        TEXT NOT NULL,
    attempts      INTEGER NOT NULL DEFAULT 0,
    max_attempts  INTEGER NOT NULL DEFAULT 3,
    lease_until   TEXT,
    leased_by     TEXT NOT NULL DEFAULT '',
    last_error    TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    done_at       TEXT
);
CREATE INDEX jobs_due ON jobs (status, run_at);

-- Events name the channel or schedule they are about (item_id holds post ids from now on).
ALTER TABLE events ADD COLUMN channel_id TEXT;
ALTER TABLE events ADD COLUMN schedule_id TEXT;

-- +goose Down
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS schedules;
DROP TABLE IF EXISTS post_targets;
DROP TABLE IF EXISTS posts;
DROP TABLE IF EXISTS provider_apps;
DROP TABLE IF EXISTS channels;
