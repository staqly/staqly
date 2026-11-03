-- name: CreateProductFeedback :exec
INSERT INTO product_feedback (id, workspace_id, reporter_id, kind, description, page_path, attachment_key, attachment_type, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: CountProductFeedbackSince :one
SELECT COUNT(*) FROM product_feedback WHERE reporter_id = ? AND created_at >= ?;

-- name: CountWorkspaceProductFeedbackSince :one
SELECT COUNT(*) FROM product_feedback WHERE workspace_id = ? AND created_at >= ?;

-- name: GetProductFeedback :one
SELECT * FROM product_feedback WHERE id = ?;

-- name: SetProductFeedbackStatus :execrows
UPDATE product_feedback SET status = ? WHERE id = ?;

-- name: AdminListProductFeedback :many
SELECT f.*, COALESCE(u.name, '') AS reporter_name, COALESCE(u.email, '') AS reporter_email, w.name AS workspace_name
FROM product_feedback f
JOIN workspaces w ON w.id = f.workspace_id
LEFT JOIN users u ON u.id = f.reporter_id
WHERE (CAST(sqlc.arg(cursor_at) AS TEXT) = '' OR f.created_at < CAST(sqlc.arg(cursor_at) AS TEXT)
    OR (f.created_at = CAST(sqlc.arg(cursor_at) AS TEXT) AND f.id < sqlc.arg(cursor_id)))
ORDER BY f.created_at DESC, f.id DESC LIMIT sqlc.arg(page_limit);
