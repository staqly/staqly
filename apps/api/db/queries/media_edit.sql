-- name: RepointDraftMedia :exec
-- The design editor's "Replace the original": drafts that carry the file carry the edited one instead.
UPDATE posts SET media = replace(media, @from_quoted, @to_quoted), thread = replace(thread, @from_quoted, @to_quoted),
  version = version + 1, updated_at = @updated_at
WHERE workspace_id = @workspace_id AND status = 'draft' AND (instr(media, @from_quoted) > 0 OR instr(thread, @from_quoted) > 0);

-- name: MediaUsedOutsideDrafts :one
-- Posts other than drafts that carry the file (scheduled, publishing, out or failed): replacing it would change them.
SELECT COUNT(*) FROM posts
WHERE workspace_id = @workspace_id AND status <> 'draft' AND (instr(media, @quoted) > 0 OR instr(thread, @quoted) > 0);

-- name: CreateMediaProject :exec
INSERT INTO media_projects (id, workspace_id, name, source_ids, output_id, page_ids, storage_name, size_bytes, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateMediaProject :execrows
UPDATE media_projects SET name = @name, source_ids = @source_ids, output_id = @output_id, page_ids = @page_ids, storage_name = @storage_name,
  size_bytes = @size_bytes, updated_at = @updated_at
WHERE workspace_id = @workspace_id AND id = @id;

-- name: GetMediaProject :one
SELECT * FROM media_projects WHERE workspace_id = ? AND id = ?;

-- name: ListMediaProjects :many
-- A workspace's projects, newest first; with a media id, the ones made from it or saved as it. A project whose
-- saved file is in Deleted (or gone) is left out with it: restoring the file brings the project back.
SELECT p.* FROM media_projects p
WHERE p.workspace_id = @workspace_id
  AND (CAST(@media AS TEXT) = '' OR p.output_id = @media OR instr(p.source_ids, '"' || @media || '"') > 0)
  AND (p.output_id = '' OR EXISTS (
    SELECT 1 FROM media m WHERE m.workspace_id = p.workspace_id AND m.id = p.output_id AND m.deleted_at IS NULL))
ORDER BY p.updated_at DESC LIMIT 100;

-- name: DeleteMediaProject :execrows
DELETE FROM media_projects WHERE workspace_id = ? AND id = ?;


-- name: AllMediaProjectFiles :many
-- Every project file of every workspace, for moving files to the workspace folders once (MoveLegacyFiles).
SELECT workspace_id, storage_name FROM media_projects;

-- name: GetWorkspaceDesignBrand :one
SELECT design_brand FROM workspaces WHERE id = ?;

-- name: UpdateWorkspaceDesignBrand :exec
UPDATE workspaces SET design_brand = ?, updated_at = ? WHERE id = ?;
