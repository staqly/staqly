-- +goose Up
-- Repeated free trials (cloud with billing only). trial_fingerprints: who a workspace's channels belong to, and the
-- cards it paid with, as an HMAC with a key derived from the server's secret key: never the platform's id or
-- Stripe's card fingerprint itself. kind: account (a personal account: an X user, a Bluesky DID...), person (the
-- platform user who authorized a shared space: a Slack workspace, a Facebook Page...; a space without one has no
-- fingerprint) or card. Server-wide and kept when the workspace is deleted, so a trial that ended unpaid is still
-- known: unpaid_trial_at is set on every row of a workspace whose trial ended without a payment (cleared if it pays
-- later); only those within 180 days count.
CREATE TABLE trial_fingerprints (
    kind            TEXT NOT NULL,
    fingerprint     TEXT NOT NULL,
    workspace_id    TEXT NOT NULL,
    unpaid_trial_at TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    PRIMARY KEY (kind, fingerprint, workspace_id)
);

CREATE INDEX trial_fingerprints_workspace ON trial_fingerprints (workspace_id);

-- workspace_trials: a workspace's trial as far as repeated trials go (a row: its trial or a payment was seen).
-- paid_at: when it was first seen paying (an active subscription); unpaid_trial_at: when its trial ended without a
-- payment; revoked_at / revoked_reason (card): its trial was ended at once because its card was used in an unpaid
-- trial; review_at / review_reason (channels): its trial is paused (no publishing, no new channels) because it
-- connected accounts of an unpaid trial, until it pays or the operator clears it (cleared_at: the operator vouched
-- for it, and it isn't checked again).
CREATE TABLE workspace_trials (
    workspace_id    TEXT PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    paid_at         TEXT,
    unpaid_trial_at TEXT,
    revoked_at      TEXT,
    revoked_reason  TEXT,
    review_at       TEXT,
    review_reason   TEXT,
    cleared_at      TEXT,
    updated_at      TEXT NOT NULL
);

-- A trial seen here is the only kind that can end unpaid, so a subscription from before this shipped (ended, or
-- paid at some point) is never taken for one. Workspaces in their trial now get their row; those past it count as
-- paid (past_due and unpaid too: whether they paid isn't known).
INSERT INTO workspace_trials (workspace_id, updated_at)
SELECT id, updated_at FROM workspaces WHERE subscription_status = 'trialing';
INSERT INTO workspace_trials (workspace_id, paid_at, updated_at)
SELECT id, updated_at, updated_at FROM workspaces WHERE subscription_status IN ('active', 'past_due', 'unpaid');

-- +goose Down
DROP TABLE workspace_trials;
DROP TABLE trial_fingerprints;
