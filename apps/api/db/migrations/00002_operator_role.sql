-- +goose Up
ALTER TABLE api_keys ADD COLUMN guest_session_id TEXT REFERENCES sessions (id) ON DELETE CASCADE;

-- +sqlite
CREATE TABLE workspace_members_new (
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role         TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'operator', 'editor', 'viewer')),
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    PRIMARY KEY (workspace_id, user_id)
);
INSERT INTO workspace_members_new (workspace_id, user_id, role, created_at, updated_at) SELECT workspace_id, user_id, role, created_at, updated_at FROM workspace_members;
DROP TABLE workspace_members;
ALTER TABLE workspace_members_new RENAME TO workspace_members;

CREATE INDEX workspace_members_user ON workspace_members (user_id);

CREATE TABLE workspace_invitations_new (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email        TEXT NOT NULL,
    role         TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'operator', 'editor', 'viewer')),
    token_hash   TEXT NOT NULL UNIQUE,
    invited_by   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    accepted_at  TEXT,
    accepted_by  TEXT,
    revoked_at   TEXT
);
INSERT INTO workspace_invitations_new (id, workspace_id, email, role, token_hash, invited_by, created_at, updated_at, expires_at, accepted_at, accepted_by, revoked_at) SELECT id, workspace_id, email, role, token_hash, invited_by, created_at, updated_at, expires_at, accepted_at, accepted_by, revoked_at FROM workspace_invitations;
DROP TABLE workspace_invitations;
ALTER TABLE workspace_invitations_new RENAME TO workspace_invitations;

CREATE UNIQUE INDEX workspace_invitations_open ON workspace_invitations (workspace_id, email)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

-- +end
-- +postgres
-- ALTER TABLE workspace_members DROP CONSTRAINT workspace_members_role_check;
-- ALTER TABLE workspace_members ADD CONSTRAINT workspace_members_role_check CHECK (role IN ('owner', 'admin', 'operator', 'editor', 'viewer'));
-- ALTER TABLE workspace_invitations DROP CONSTRAINT workspace_invitations_role_check;
-- ALTER TABLE workspace_invitations ADD CONSTRAINT workspace_invitations_role_check CHECK (role IN ('owner', 'admin', 'operator', 'editor', 'viewer'));
-- +end

-- Existing editable review links gain operator access, including already-redeemed sessions.
UPDATE device_links SET guest_role = 'operator' WHERE guest_role = 'editor';
UPDATE sessions SET guest_role = 'operator' WHERE guest_role = 'editor';

-- +goose Down
DELETE FROM api_keys WHERE guest_session_id IS NOT NULL;
ALTER TABLE api_keys DROP COLUMN guest_session_id;

UPDATE workspace_members SET role = 'editor' WHERE role = 'operator';
UPDATE workspace_invitations SET role = 'editor' WHERE role = 'operator';
UPDATE device_links SET guest_role = 'editor' WHERE guest_role = 'operator';
UPDATE sessions SET guest_role = 'editor' WHERE guest_role = 'operator';

-- +sqlite
CREATE TABLE workspace_members_new (
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role         TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'editor', 'viewer')),
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    PRIMARY KEY (workspace_id, user_id)
);
INSERT INTO workspace_members_new (workspace_id, user_id, role, created_at, updated_at) SELECT workspace_id, user_id, role, created_at, updated_at FROM workspace_members;
DROP TABLE workspace_members;
ALTER TABLE workspace_members_new RENAME TO workspace_members;

CREATE INDEX workspace_members_user ON workspace_members (user_id);

CREATE TABLE workspace_invitations_new (
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
INSERT INTO workspace_invitations_new (id, workspace_id, email, role, token_hash, invited_by, created_at, updated_at, expires_at, accepted_at, accepted_by, revoked_at) SELECT id, workspace_id, email, role, token_hash, invited_by, created_at, updated_at, expires_at, accepted_at, accepted_by, revoked_at FROM workspace_invitations;
DROP TABLE workspace_invitations;
ALTER TABLE workspace_invitations_new RENAME TO workspace_invitations;

CREATE UNIQUE INDEX workspace_invitations_open ON workspace_invitations (workspace_id, email)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

-- +end
-- +postgres
-- ALTER TABLE workspace_members DROP CONSTRAINT workspace_members_role_check;
-- ALTER TABLE workspace_members ADD CONSTRAINT workspace_members_role_check CHECK (role IN ('owner', 'admin', 'editor', 'viewer'));
-- ALTER TABLE workspace_invitations DROP CONSTRAINT workspace_invitations_role_check;
-- ALTER TABLE workspace_invitations ADD CONSTRAINT workspace_invitations_role_check CHECK (role IN ('owner', 'admin', 'editor', 'viewer'));
-- +end
