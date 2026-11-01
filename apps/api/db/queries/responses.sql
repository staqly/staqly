-- name: UpsertResponse :exec
-- A re-read updates what changes; the sentiment label stays.
INSERT INTO post_responses (workspace_id, post_id, channel_id, id, parent_id, author, author_handle, text, url, likes, created_at, fetched_at)
VALUES (@workspace_id, @post_id, @channel_id, @id, @parent_id, @author, @author_handle, @text, @url, @likes, @created_at, @fetched_at)
ON CONFLICT (post_id, channel_id, id) DO UPDATE SET text = excluded.text, likes = excluded.likes, fetched_at = excluded.fetched_at;

-- name: ListResponses :many
SELECT * FROM post_responses WHERE workspace_id = @workspace_id AND post_id = @post_id ORDER BY created_at DESC, id;

-- name: SetResponseSentiment :exec
UPDATE post_responses SET sentiment = @sentiment
WHERE workspace_id = @workspace_id AND post_id = @post_id AND channel_id = @channel_id AND id = @id;

-- name: UpsertResponseInsight :exec
INSERT INTO post_response_insights (workspace_id, post_id, channel_id, positive, neutral, negative, summary, themes, questions, status, error, responses_seen, analyzed_at)
VALUES (@workspace_id, @post_id, @channel_id, @positive, @neutral, @negative, @summary, @themes, @questions, @status, @error, @responses_seen, @analyzed_at)
ON CONFLICT (post_id, channel_id) DO UPDATE SET positive = excluded.positive, neutral = excluded.neutral, negative = excluded.negative,
  summary = excluded.summary, themes = excluded.themes, questions = excluded.questions, status = excluded.status, error = excluded.error,
  responses_seen = excluded.responses_seen, analyzed_at = excluded.analyzed_at;

-- name: ListResponseInsights :many
SELECT * FROM post_response_insights WHERE workspace_id = @workspace_id AND post_id = @post_id ORDER BY channel_id;

-- name: ListResponseTargets :many
-- Published copies still online on a connected channel, published after the cutoff (the worker's read window).
SELECT p.workspace_id, t.post_id, t.channel_id, c.provider
FROM post_targets t
JOIN posts p ON p.id = t.post_id
JOIN channels c ON c.id = t.channel_id
WHERE t.status = 'published' AND t.removal = '' AND c.status = 'connected' AND t.published_at > @published_after
ORDER BY t.post_id, t.channel_id;

-- name: DeleteOldResponses :execrows
DELETE FROM post_responses
WHERE (post_id, channel_id, id) IN (SELECT r.post_id, r.channel_id, r.id FROM post_responses r
                                     WHERE r.workspace_id = @workspace_id AND r.fetched_at < @cutoff LIMIT @lim);

-- name: DeleteOrphanResponseInsights :execrows
DELETE FROM post_response_insights
WHERE post_response_insights.workspace_id = @workspace_id
  AND NOT EXISTS (SELECT 1 FROM post_responses r WHERE r.post_id = post_response_insights.post_id);

-- name: ExportResponses :many
SELECT * FROM post_responses WHERE workspace_id = ? ORDER BY post_id, channel_id, id;

-- name: ExportResponseInsights :many
SELECT * FROM post_response_insights WHERE workspace_id = ? ORDER BY post_id, channel_id;

-- name: NewestResponseTime :one
SELECT CAST(COALESCE(MAX(created_at), '') AS TEXT) AS newest FROM post_responses WHERE workspace_id = @workspace_id AND post_id = @post_id AND channel_id = @channel_id;
