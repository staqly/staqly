-- name: UpsertAgent :exec
-- A new agent is added only while the workspace has fewer than @max_agents; known ones are always updated.
INSERT INTO agents (id, workspace_id, kind, name, version, principal_id, principal_name, first_seen_at, last_seen_at, requests, items_created)
SELECT @id, @workspace_id, @kind, @name, @version, @principal_id, @principal_name, @seen_at, @seen_at, @requests, @items_created
WHERE (SELECT COUNT(*) FROM agents WHERE workspace_id = @workspace_id) < CAST(@max_agents AS INTEGER)
   OR EXISTS (SELECT 1 FROM agents WHERE workspace_id = @workspace_id AND kind = @kind AND name = @name AND principal_id = @principal_id)
ON CONFLICT (workspace_id, kind, name, principal_id) DO UPDATE SET
    version = excluded.version, principal_name = excluded.principal_name, last_seen_at = excluded.last_seen_at,
    requests = agents.requests + excluded.requests, items_created = agents.items_created + excluded.items_created;

-- name: ListAgents :many
SELECT * FROM agents WHERE workspace_id = @workspace_id ORDER BY last_seen_at DESC LIMIT 200;

-- name: PruneAgents :execrows
-- Agents and API connections not seen since @cutoff disappear (they come back if they talk again).
DELETE FROM agents WHERE last_seen_at < @cutoff;

-- name: DeleteAgent :execrows
DELETE FROM agents WHERE workspace_id = @workspace_id AND id = @id;
