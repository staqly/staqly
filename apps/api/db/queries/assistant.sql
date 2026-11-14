-- name: SaveAssistantChat :exec
-- Creates the chat, or saves it again when it is the same owner's (another owner's id changes nothing).
INSERT INTO assistant_chats (id, workspace_id, owner, title, messages, pending, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET messages = excluded.messages, pending = excluded.pending, updated_at = excluded.updated_at
WHERE assistant_chats.workspace_id = excluded.workspace_id AND assistant_chats.owner = excluded.owner;

-- name: ListAssistantChats :many
-- One page, newest first, after the cursor (updated_at, id); q (lower case) is found in the title.
SELECT id, title, created_at, updated_at FROM assistant_chats
WHERE workspace_id = sqlc.arg(workspace_id) AND owner = sqlc.arg(owner)
  AND (sqlc.arg(after_at) = '' OR updated_at < sqlc.arg(after_at) OR (updated_at = sqlc.arg(after_at) AND id < sqlc.arg(after_id)))
  AND (sqlc.arg(q) = '' OR instr(lower(title), sqlc.arg(q)) > 0)
ORDER BY updated_at DESC, id DESC
LIMIT sqlc.arg(lim);

-- name: GetAssistantChat :one
SELECT * FROM assistant_chats WHERE workspace_id = ? AND owner = ? AND id = ?;

-- name: DeleteAssistantChat :execrows
DELETE FROM assistant_chats WHERE workspace_id = ? AND owner = ? AND id = ?;
