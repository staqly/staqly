-- name: RecordTrialFingerprint :exec
-- A fingerprint seen in a workspace; a workspace whose trial already ended unpaid marks it at once.
INSERT INTO trial_fingerprints (kind, fingerprint, workspace_id, unpaid_trial_at, created_at, updated_at)
VALUES (sqlc.arg(kind), sqlc.arg(fingerprint), sqlc.arg(workspace_id),
  (SELECT t.unpaid_trial_at FROM workspace_trials t WHERE t.workspace_id = sqlc.arg(workspace_id)),
  sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (kind, fingerprint, workspace_id) DO UPDATE SET updated_at = excluded.updated_at;

-- name: CountUnpaidTrialFingerprint :one
-- Other workspaces that had this fingerprint and whose trial ended unpaid since `since`.
SELECT COUNT(*) FROM trial_fingerprints
WHERE kind = sqlc.arg(kind) AND fingerprint = sqlc.arg(fingerprint) AND workspace_id <> sqlc.arg(workspace_id)
  AND unpaid_trial_at >= sqlc.arg(since);

-- name: CountWorkspaceUnpaidMatches :one
-- This workspace's fingerprints of a kind that another workspace's unpaid trial (ended since `since`) also had.
SELECT COUNT(DISTINCT f.fingerprint) FROM trial_fingerprints f
WHERE f.workspace_id = sqlc.arg(workspace_id) AND f.kind = sqlc.arg(kind)
  AND EXISTS (SELECT 1 FROM trial_fingerprints o WHERE o.kind = f.kind AND o.fingerprint = f.fingerprint
    AND o.workspace_id <> f.workspace_id AND o.unpaid_trial_at >= sqlc.arg(since));

-- name: HasTrialFingerprint :one
SELECT COUNT(*) FROM trial_fingerprints WHERE kind = ? AND fingerprint = ? AND workspace_id = ?;

-- name: MarkTrialFingerprintsUnpaid :exec
UPDATE trial_fingerprints SET unpaid_trial_at = sqlc.arg(now), updated_at = sqlc.arg(now)
WHERE workspace_id = sqlc.arg(workspace_id) AND unpaid_trial_at IS NULL;

-- name: ClearTrialFingerprintsUnpaid :exec
UPDATE trial_fingerprints SET unpaid_trial_at = NULL, updated_at = sqlc.arg(now)
WHERE workspace_id = sqlc.arg(workspace_id) AND unpaid_trial_at IS NOT NULL;

-- name: DeleteTrialFingerprints :exec
DELETE FROM trial_fingerprints WHERE workspace_id = ?;

-- name: GetWorkspaceTrial :one
SELECT * FROM workspace_trials WHERE workspace_id = ?;

-- name: SetWorkspaceTrialSeen :exec
INSERT INTO workspace_trials (workspace_id, updated_at) VALUES (sqlc.arg(workspace_id), sqlc.arg(now))
ON CONFLICT (workspace_id) DO NOTHING;

-- name: SetWorkspacePaid :exec
-- Paying: the first time is kept; an unpaid trial before it no longer counts.
INSERT INTO workspace_trials (workspace_id, paid_at, updated_at) VALUES (sqlc.arg(workspace_id), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (workspace_id) DO UPDATE SET paid_at = COALESCE(workspace_trials.paid_at, excluded.paid_at),
  unpaid_trial_at = NULL, updated_at = excluded.updated_at;

-- name: SetWorkspaceTrialUnpaid :exec
INSERT INTO workspace_trials (workspace_id, unpaid_trial_at, updated_at) VALUES (sqlc.arg(workspace_id), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (workspace_id) DO UPDATE SET unpaid_trial_at = COALESCE(workspace_trials.unpaid_trial_at, excluded.unpaid_trial_at),
  updated_at = excluded.updated_at;

-- name: SetWorkspaceTrialRevoked :exec
INSERT INTO workspace_trials (workspace_id, revoked_at, revoked_reason, updated_at)
VALUES (sqlc.arg(workspace_id), sqlc.arg(now), sqlc.arg(reason), sqlc.arg(now))
ON CONFLICT (workspace_id) DO UPDATE SET revoked_at = excluded.revoked_at, revoked_reason = excluded.revoked_reason,
  updated_at = excluded.updated_at;

-- name: SetWorkspaceTrialReview :exec
INSERT INTO workspace_trials (workspace_id, review_at, review_reason, updated_at)
VALUES (sqlc.arg(workspace_id), sqlc.arg(now), sqlc.arg(reason), sqlc.arg(now))
ON CONFLICT (workspace_id) DO UPDATE SET review_at = excluded.review_at, review_reason = excluded.review_reason,
  updated_at = excluded.updated_at;

-- name: ClearWorkspaceTrialReview :execrows
-- The operator vouches for the workspace: no review, no early end, and it isn't checked again.
INSERT INTO workspace_trials (workspace_id, cleared_at, updated_at) VALUES (sqlc.arg(workspace_id), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (workspace_id) DO UPDATE SET review_at = NULL, review_reason = NULL, revoked_at = NULL, revoked_reason = NULL,
  cleared_at = excluded.cleared_at, updated_at = excluded.updated_at;
