-- name: ListChannelGroups :many
SELECT * FROM channel_groups WHERE workspace_id = ? ORDER BY is_default DESC, position, name COLLATE NOCASE, id;

-- name: GetChannelGroup :one
SELECT * FROM channel_groups WHERE workspace_id = ? AND id = ?;

-- name: GetDefaultChannelGroup :one
SELECT * FROM channel_groups WHERE workspace_id = ? AND is_default = 1;

-- name: InsertDefaultChannelGroup :exec
-- Concurrent first reads may both try: the unique index on the default keeps one.
INSERT OR IGNORE INTO channel_groups (id, workspace_id, name, color, position, is_default, created_at, updated_at)
VALUES (?, ?, 'Default', '', 0, 1, ?, ?);

-- name: CreateChannelGroup :exec
INSERT INTO channel_groups (id, workspace_id, name, color, icon, position, is_default, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?);

-- name: UpdateChannelGroup :exec
UPDATE channel_groups SET name = ?, color = ?, icon = ?, position = ?, updated_at = ? WHERE workspace_id = ? AND id = ?;

-- name: DeleteChannelGroup :execrows
DELETE FROM channel_groups WHERE workspace_id = ? AND id = ? AND is_default = 0;

-- name: MoveGroupChannels :exec
UPDATE channels SET group_id = @to_group, updated_at = @updated_at WHERE workspace_id = @workspace_id AND group_id = @from_group;

-- name: SetChannelGroup :exec
UPDATE channels SET group_id = ?, updated_at = ? WHERE workspace_id = ? AND id = ?;

-- name: CountChannelsByGroup :many
SELECT group_id, COUNT(*) AS channels FROM channels WHERE workspace_id = ? AND deleted_at IS NULL GROUP BY group_id;
