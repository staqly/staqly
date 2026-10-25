-- name: RepointDraftMedia :exec
-- The design editor's "Replace the original": drafts that carry the file carry the edited one instead.
UPDATE posts SET media = replace(media, @from_quoted, @to_quoted), thread = replace(thread, @from_quoted, @to_quoted),
  updated_at = @updated_at
WHERE workspace_id = @workspace_id AND status = 'draft' AND (instr(media, @from_quoted) > 0 OR instr(thread, @from_quoted) > 0);

-- name: MediaUsedOutsideDrafts :one
-- Posts other than drafts that carry the file (scheduled, publishing, out or failed): replacing it would change them.
SELECT COUNT(*) FROM posts
WHERE workspace_id = @workspace_id AND status <> 'draft' AND (instr(media, @quoted) > 0 OR instr(thread, @quoted) > 0);

-- name: CreateMediaProject :exec
INSERT INTO media_projects (id, workspace_id, name, source_ids, output_id, storage_name, size_bytes, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateMediaProject :execrows
UPDATE media_projects SET name = @name, source_ids = @source_ids, output_id = @output_id, storage_name = @storage_name,
  size_bytes = @size_bytes, updated_at = @updated_at
WHERE workspace_id = @workspace_id AND id = @id;

-- name: GetMediaProject :one
SELECT * FROM media_projects WHERE workspace_id = ? AND id = ?;

-- name: ListMediaProjects :many
-- A workspace's projects, newest first; with a media id, the ones made from it or saved as it.
SELECT * FROM media_projects
WHERE workspace_id = @workspace_id
  AND (CAST(@media AS TEXT) = '' OR output_id = @media OR instr(source_ids, '"' || @media || '"') > 0)
ORDER BY updated_at DESC LIMIT 100;

-- name: DeleteMediaProject :execrows
DELETE FROM media_projects WHERE workspace_id = ? AND id = ?;

-- name: GetWorkspaceAI :one
SELECT * FROM workspace_ai WHERE workspace_id = ?;

-- name: UpsertWorkspaceAI :exec
INSERT INTO workspace_ai (workspace_id, provider, model, credentials, key_hint, updated_by, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (workspace_id) DO UPDATE SET provider = excluded.provider, model = excluded.model,
  credentials = excluded.credentials, key_hint = excluded.key_hint, updated_by = excluded.updated_by,
  updated_at = excluded.updated_at;

-- name: DeleteWorkspaceAI :execrows
DELETE FROM workspace_ai WHERE workspace_id = ?;

-- name: AllMediaProjectFiles :many
-- Every project file of every workspace, for moving files to the workspace folders once (MoveLegacyFiles).
SELECT workspace_id, storage_name FROM media_projects;
