-- +goose Up
-- The admin panel: sign-in codes, sessions and the audit trail of operator actions (no foreign keys: the audit
-- outlives a deleted workspace).
CREATE TABLE admins (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    created_by    TEXT NOT NULL DEFAULT '',
    last_login_at TEXT
);

CREATE TABLE admin_codes (
    id         TEXT PRIMARY KEY,
    email      TEXT NOT NULL,
    code_hash  TEXT NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at    TEXT
);

CREATE INDEX admin_codes_email ON admin_codes (email, created_at);

CREATE TABLE admin_sessions (
    id           TEXT PRIMARY KEY,
    email        TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    csrf_token   TEXT NOT NULL,
    user_agent   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    revoked_at   TEXT
);

CREATE TABLE admin_audit (
    id           TEXT PRIMARY KEY,
    email        TEXT NOT NULL,
    action       TEXT NOT NULL,
    workspace_id TEXT NOT NULL DEFAULT '',
    target       TEXT NOT NULL DEFAULT '',
    details      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(details)),
    created_at   TEXT NOT NULL
);

CREATE INDEX admin_audit_created ON admin_audit (created_at);

-- +goose Down
DROP TABLE admin_audit;
DROP TABLE admin_sessions;
DROP TABLE admin_codes;
DROP TABLE admins;
