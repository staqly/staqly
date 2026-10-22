-- name: ListMediaFolders :many
SELECT * FROM media_folders WHERE workspace_id = ? ORDER BY position, name COLLATE NOCASE, id;

-- name: GetMediaFolder :one
SELECT * FROM media_folders WHERE workspace_id = ? AND id = ?;

-- name: CreateMediaFolder :exec
INSERT INTO media_folders (id, workspace_id, parent_id, name, position, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateMediaFolder :execrows
UPDATE media_folders SET parent_id = @parent_id, name = @name, updated_at = @updated_at
WHERE workspace_id = @workspace_id AND id = @id;

-- name: DeleteMediaFolder :execrows
DELETE FROM media_folders WHERE workspace_id = ? AND id = ?;

-- name: ReparentMediaFolders :exec
-- A deleted folder's subfolders move up to its parent.
UPDATE media_folders SET parent_id = @to_parent, updated_at = @updated_at
WHERE workspace_id = @workspace_id AND parent_id = @from_parent;

-- name: MoveFolderMedia :exec
-- A deleted folder's files move up to its parent.
UPDATE media SET folder_id = @to_folder WHERE workspace_id = @workspace_id AND folder_id = @from_folder;

-- name: MediaFolderCounts :many
-- Files per folder ('' = the top level).
SELECT folder_id, COUNT(*) AS files FROM media WHERE workspace_id = ? AND deleted_at IS NULL GROUP BY folder_id;

-- name: MediaTagsAll :many
-- Every file's tags (JSON arrays), to count tags in Go.
SELECT tags FROM media WHERE workspace_id = ? AND tags <> '[]' AND deleted_at IS NULL;

-- name: SetMediaFolder :execrows
UPDATE media SET folder_id = @folder_id WHERE workspace_id = @workspace_id AND id = @id;

-- name: SetMediaTags :execrows
UPDATE media SET tags = @tags WHERE workspace_id = @workspace_id AND id = @id;

-- name: TrashMedia :execrows
UPDATE media SET deleted_at = @deleted_at WHERE workspace_id = @workspace_id AND id = @id AND deleted_at IS NULL;

-- name: RestoreMedia :execrows
UPDATE media SET deleted_at = NULL WHERE workspace_id = @workspace_id AND id = @id AND deleted_at IS NOT NULL;

-- name: TrashedMediaCount :one
SELECT COUNT(*) FROM media WHERE workspace_id = ? AND deleted_at IS NOT NULL;

-- name: PurgeableMedia :many
-- Files in Deleted for longer than the grace period, across workspaces (the retention sweep).
SELECT id, workspace_id, storage_name FROM media WHERE deleted_at IS NOT NULL AND deleted_at < @cutoff
ORDER BY deleted_at LIMIT @lim;

-- name: DuplicateMediaCount :one
-- Files (not deleted) that have at least one identical twin (same sha256).
SELECT COUNT(*) FROM media m WHERE m.workspace_id = @workspace_id AND m.deleted_at IS NULL AND EXISTS (
  SELECT 1 FROM media d WHERE d.workspace_id = m.workspace_id AND d.sha256 = m.sha256 AND d.id <> m.id AND d.deleted_at IS NULL);

-- name: OldestTwin :one
-- The oldest other file (not deleted) with the same content.
SELECT id FROM media WHERE workspace_id = @workspace_id AND sha256 = @sha256 AND id <> @id AND deleted_at IS NULL
ORDER BY created_at, id LIMIT 1;

-- name: RepointPostMedia :exec
-- Posts that carry one file (in their files or a comment) carry another instead (ids are quoted in the JSON).
UPDATE posts SET media = replace(media, @from_quoted, @to_quoted), thread = replace(thread, @from_quoted, @to_quoted),
  version = version + 1, updated_at = @updated_at
WHERE workspace_id = @workspace_id AND (instr(media, @from_quoted) > 0 OR instr(thread, @from_quoted) > 0);

-- name: PostsCarryingMedia :many
-- Posts with a file in their files or a comment (the id quoted, as it is in the JSON).
SELECT id, media, thread FROM posts WHERE workspace_id = @workspace_id AND (instr(media, @quoted) > 0 OR instr(thread, @quoted) > 0);

-- name: SetPostMediaLists :exec
UPDATE posts SET media = @media, thread = @thread, updated_at = @updated_at WHERE workspace_id = @workspace_id AND id = @id;
