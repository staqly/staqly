-- name: CreatePost :exec
INSERT INTO posts (id, workspace_id, collection_id, content, media, status, publish_at, tags, metadata, source, external_ref,
    repeat_every, repeat_of, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetPost :one
SELECT * FROM posts WHERE workspace_id = ? AND id = ?;

-- name: GetPostByID :one
SELECT * FROM posts WHERE id = ?;

-- name: UpdatePost :execrows
UPDATE posts
SET collection_id = ?, content = ?, media = ?, status = ?, publish_at = ?, published_at = ?, tags = ?, metadata = ?,
    source = ?, external_ref = ?, repeat_every = ?, version = version + 1, updated_at = ?
WHERE workspace_id = ? AND id = ? AND version = ?;

-- name: SetPostStatus :exec
-- Worker-side status moves (publishing, published, failed) bump no version: they are not edits.
UPDATE posts SET status = ?, published_at = COALESCE(?, published_at), updated_at = ? WHERE id = ?;

-- name: RepeatOfPost :one
-- The copy a repeating post already made (none: sql.ErrNoRows).
-- The repeat_of <> '' term lets the partial index posts_repeat_of answer it (a bound value alone can't).
SELECT id FROM posts WHERE repeat_of = ? AND repeat_of <> '' LIMIT 1;

-- name: CollectionPostIDs :many
-- The posts of one collection, oldest first (moved to Default when the collection is deleted).
SELECT id FROM posts WHERE workspace_id = ? AND collection_id = ? ORDER BY created_at, id;

-- name: MoveCollectionPosts :execrows
-- Every post of a collection into another one, whatever its status: a move, so a new version.
UPDATE posts SET collection_id = @to_collection_id, version = posts.version + 1, updated_at = @updated_at
WHERE workspace_id = @workspace_id AND collection_id = @from_collection_id;

-- name: DeletePost :execrows
DELETE FROM posts WHERE workspace_id = ? AND id = ?;

-- name: PostsReferencingMedia :one
-- media is a JSON array of quoted ids, so a quoted substring match is exact; a file in a thread part is a use too.
SELECT COUNT(*) FROM posts WHERE workspace_id = @workspace_id
  AND (instr(media, '"' || CAST(@media_id AS TEXT) || '"') > 0 OR instr(thread, '"' || CAST(@media_id AS TEXT) || '"') > 0);

-- name: PostStatusCountsSince :many
SELECT status, COUNT(*) AS n FROM posts WHERE workspace_id = ? AND created_at >= ? GROUP BY status;

-- name: CollectionPostCounts :many
SELECT collection_id, status, COUNT(*) AS n FROM posts WHERE workspace_id = ? GROUP BY collection_id, status;

-- name: WaitingPosts :many
-- Posts a schedule may take: drafts of the collection without a time of their own, oldest first.
SELECT * FROM posts WHERE workspace_id = ? AND collection_id = ? AND status = 'draft' AND publish_at IS NULL
ORDER BY created_at, id LIMIT ?;

-- name: PostsByIDs :many
-- ids: a JSON array without duplicates (a row per id given). Driven by the ids (primary key lookups) like MediaByIDs.
SELECT posts.* FROM json_each(@ids) AS j CROSS JOIN posts WHERE posts.id = j.value AND posts.workspace_id = @workspace_id
ORDER BY posts.created_at, posts.id;

-- name: ExportPostsPage :many
SELECT * FROM posts WHERE workspace_id = @workspace_id AND id > @after_id ORDER BY id LIMIT @lim;

-- ---- targets ----

-- name: UpsertTarget :exec
INSERT INTO post_targets (post_id, channel_id, content, settings, status, updated_at)
VALUES (?, ?, ?, ?, 'pending', ?)
ON CONFLICT (post_id, channel_id) DO UPDATE SET content = excluded.content, settings = excluded.settings, updated_at = excluded.updated_at;

-- name: DeleteTargetsNotIn :exec
DELETE FROM post_targets WHERE post_id = @post_id AND channel_id NOT IN (SELECT value FROM json_each(@keep));

-- name: ListTargets :many
SELECT t.*, c.name AS channel_name, c.provider, c.status AS channel_status, c.avatar_url AS channel_avatar_url FROM post_targets t
JOIN channels c ON c.id = t.channel_id WHERE t.post_id = ? ORDER BY c.name COLLATE NOCASE, c.id;

-- name: ListTargetsForPosts :many
SELECT t.*, c.name AS channel_name, c.provider, c.status AS channel_status, c.avatar_url AS channel_avatar_url FROM post_targets t
JOIN channels c ON c.id = t.channel_id WHERE t.post_id IN (SELECT value FROM json_each(@ids)) ORDER BY t.post_id, c.name COLLATE NOCASE, c.id;

-- name: GetTarget :one
SELECT * FROM post_targets WHERE post_id = ? AND channel_id = ?;

-- name: SetTargetStatus :exec
UPDATE post_targets SET status = ?, external_id = ?, external_url = ?, error = ?, attempts = ?, published_at = ?, updated_at = ?
WHERE post_id = ? AND channel_id = ?;

-- name: ResetTargets :exec
-- Back to pending (a cancelled schedule, a duplicate): links and errors of earlier tries are dropped.
UPDATE post_targets SET status = 'pending', external_id = '', external_url = '', error = '', attempts = 0, published_at = NULL, updated_at = ?
WHERE post_id = ? AND status <> 'published';

-- name: PublishingPostsOfChannel :many
-- Posts still publishing to this channel (its target open): settled when the channel is removed.
SELECT DISTINCT t.post_id FROM post_targets t JOIN posts p ON p.id = t.post_id
WHERE t.channel_id = ? AND p.status = 'publishing' AND t.status IN ('pending', 'publishing');

-- name: CountTargetsByStatus :many
SELECT status, COUNT(*) AS n FROM post_targets WHERE post_id = ? GROUP BY status;

-- name: WaitingCustomTargets :many
-- Custom targets a person's automation still owes a result for (list_requested_work, GET /work).
SELECT t.post_id, t.channel_id, t.updated_at, p.collection_id FROM post_targets t
JOIN channels c ON c.id = t.channel_id JOIN posts p ON p.id = t.post_id
WHERE p.workspace_id = @workspace_id AND t.status = 'publishing' AND c.provider = 'custom'
ORDER BY t.updated_at, t.post_id, t.channel_id
LIMIT @lim;

-- name: StripMediaFromPosts :exec
-- Removes one media id from every post's media array (media is a JSON array of quoted ids).
UPDATE posts
SET media = (SELECT json_group_array(j.value) FROM json_each(posts.media) AS j WHERE j.value <> CAST(@media_id AS TEXT)),
    updated_at = @updated_at
WHERE workspace_id = @workspace_id AND instr(media, '"' || CAST(@media_id AS TEXT) || '"') > 0;

-- name: SetPostThread :exec
-- The thread is written next to the post (create, edit, duplicate, repeat); not a version bump of its own.
UPDATE posts SET thread = ? WHERE id = ?;

-- name: SetTargetReplies :exec
UPDATE post_targets SET replies = ?, updated_at = ? WHERE post_id = ? AND channel_id = ?;

-- name: SetPostAuthor :exec
-- The person behind a new post (personal notifications for "my posts").
UPDATE posts SET created_by_user = ? WHERE id = ? AND workspace_id = ?;
