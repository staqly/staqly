-- name: CreateNotifyChannel :exec
INSERT INTO notification_channels (id, workspace_id, name, type, enabled, config, secrets, events, collection_ids,
                                   digest_window_seconds, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetNotifyChannel :one
SELECT * FROM notification_channels WHERE workspace_id = ? AND id = ?;

-- name: GetNotifyChannelByID :one
-- Public integration endpoints only: the caller must verify the request
-- against this channel's own secret before acting on it.
SELECT * FROM notification_channels WHERE id = ?;

-- name: ListNotifyChannels :many
SELECT * FROM notification_channels WHERE workspace_id = ? ORDER BY created_at;

-- name: ListEnabledNotifyChannels :many
SELECT id, events, collection_ids, digest_window_seconds FROM notification_channels
WHERE workspace_id = ? AND enabled = 1;

-- name: ListEnabledNotifyChannelsByType :many
SELECT * FROM notification_channels WHERE type = ? AND enabled = 1 ORDER BY created_at;

-- name: CountNotifyChannels :one
SELECT COUNT(*) FROM notification_channels WHERE workspace_id = ?;

-- name: UpdateNotifyChannel :exec
UPDATE notification_channels
SET name = ?, enabled = ?, config = ?, secrets = ?, events = ?, collection_ids = ?, digest_window_seconds = ?,
    fail_count = ?, last_error = ?, disabled_reason = ?, disabled_at = ?, updated_at = ?
WHERE workspace_id = ? AND id = ?;

-- name: DeleteNotifyChannel :execrows
DELETE FROM notification_channels WHERE workspace_id = ? AND id = ?;

-- name: RecordNotifySuccess :exec
UPDATE notification_channels SET fail_count = 0, last_error = '', last_sent_at = ? WHERE id = ?;

-- name: RecordNotifyFailure :one
UPDATE notification_channels SET fail_count = fail_count + 1, last_error = ? WHERE id = ?
RETURNING fail_count;

-- name: DisableNotifyChannel :exec
UPDATE notification_channels
SET enabled = 0, disabled_reason = @disabled_reason, disabled_at = COALESCE(disabled_at, @now), updated_at = @now
WHERE id = @id;

-- name: SetNotifyChannelError :exec
UPDATE notification_channels SET last_error = ? WHERE id = ?;

-- name: SetTelegramOffset :exec
UPDATE notification_channels SET telegram_offset = ? WHERE id = ?;

-- name: InsertNotifyOutbox :exec
INSERT INTO notification_outbox (id, workspace_id, channel_id, event_id, event_type, item_id, next_attempt_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (channel_id, event_id) DO NOTHING;

-- name: DueNotifyChannels :many
SELECT DISTINCT o.channel_id FROM notification_outbox o
JOIN notification_channels c ON c.id = o.channel_id
WHERE o.status = 'pending' AND o.next_attempt_at <= ? AND c.enabled = 1
LIMIT ?;

-- name: PendingNotifyOutbox :many
SELECT * FROM notification_outbox WHERE channel_id = ? AND status = 'pending' ORDER BY created_at, id LIMIT ?;

-- name: FinishNotifyOutbox :exec
UPDATE notification_outbox
SET status = ?, attempts = ?, next_attempt_at = ?, last_error = ?, sent_at = ?
WHERE id = ?;

-- name: ListNotifyOutbox :many
SELECT * FROM notification_outbox WHERE workspace_id = ? AND channel_id = ? ORDER BY created_at DESC, id DESC LIMIT ?;

-- name: PruneNotifyOutbox :execrows
-- Finished rows older than the cutoff, in batches.
DELETE FROM notification_outbox
WHERE id IN (SELECT o.id FROM notification_outbox o
             WHERE o.status IN ('sent', 'skipped', 'failed') AND o.created_at < @cutoff LIMIT @lim);

-- name: SkipDisabledChannelOutbox :execrows
-- Pending rows of a disabled channel wait (the worker only sends for enabled
-- channels) so re-enabling delivers them. Once the channel has been disabled
-- since before @cutoff (now - NOTIFY_DISABLED_GRACE) they are marked skipped,
-- and later pruned like the rest.
UPDATE notification_outbox SET status = 'skipped', last_error = 'channel disabled'
WHERE notification_outbox.status = 'pending'
  AND notification_outbox.channel_id IN (SELECT c.id FROM notification_channels c
                                         WHERE c.enabled = 0 AND COALESCE(c.disabled_at, c.updated_at) < @cutoff);

-- name: LeaseNotifyOutbox :execrows
-- PostgreSQL (several server processes): claims one pending row by pushing next_attempt_at forward, like
-- webhook deliveries; only one process's UPDATE matches. A crash mid-send leaves it pending until then.
UPDATE notification_outbox SET next_attempt_at = @lease_until
WHERE id = @id AND status = 'pending' AND next_attempt_at = @expected_next;
