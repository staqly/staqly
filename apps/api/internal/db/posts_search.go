package db

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// Post listing is the one dynamic query (optional filters, several sort
// orders, keyset pagination), so it is written by hand here instead of via
// sqlc. Only whitelisted SQL fragments are ever concatenated; all values are
// bound parameters.

type postSort struct {
	expr string // SQL expression; also the keyset value
	desc bool
}

var postSorts = map[string]postSort{
	// Unscheduled posts sort last either way.
	"publish_asc":  {"COALESCE(p.publish_at, '9999')", false},
	"publish_desc": {"COALESCE(p.publish_at, '')", true},
	"created_desc": {"p.created_at", true},
	"created_asc":  {"p.created_at", false},
	"updated_desc": {"p.updated_at", true},
}

func ValidPostSort(s string) bool { _, ok := postSorts[s]; return ok }

type PostSearch struct {
	WorkspaceID                 string
	CollectionID, Status        string
	ChannelID                   string // posts with a target on this channel
	Tag                         string
	Query                       string // substring of the content, case-insensitive
	PublishAfter, PublishBefore string // publish_at range; NULL never matches
	HidePast                    string // a time: posts past it are left out (PastPostSQL)
	Sort                        string
	Cursor                      string // opaque, from a previous page
	Limit                       int
}

// PastPostSQL is true for a post that is past at the bound time: it went out (published), or it failed and its
// time (none: publish now) is behind. Drafts, scheduled and publishing posts are never past.
const PastPostSQL = "(p.status = 'published' OR (p.status = 'failed' AND COALESCE(p.publish_at, '') < ?))"

var ErrBadCursor = errors.New("invalid cursor")

type postCursor struct {
	V  string `json:"v"`
	ID string `json:"id"`
}

func encodePostCursor(v, id string) string {
	b, _ := json.Marshal(postCursor{V: v, ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodePostCursor(s string) (postCursor, error) {
	var c postCursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(b, &c) != nil || c.ID == "" {
		return c, ErrBadCursor
	}
	return c, nil
}

// SearchPosts returns one page and the cursor of the next (empty on the last page).
func (q *Queries) SearchPosts(ctx context.Context, s PostSearch) ([]Post, string, error) {
	sort, ok := postSorts[s.Sort]
	if !ok {
		sort = postSorts["publish_asc"]
	}
	var where []string
	var args []any
	add := func(cond string, vals ...any) { where = append(where, cond); args = append(args, vals...) }
	add("p.workspace_id = ?", s.WorkspaceID)
	if s.CollectionID != "" {
		add("p.collection_id = ?", s.CollectionID)
	}
	if s.Status != "" {
		add("p.status = ?", s.Status)
	}
	if s.ChannelID != "" {
		add("EXISTS (SELECT 1 FROM post_targets t WHERE t.post_id = p.id AND t.channel_id = ?)", s.ChannelID)
	}
	if s.Tag != "" {
		add("instr(p.tags, ?) > 0", jsonTag(s.Tag))
	}
	if s.PublishAfter != "" {
		add("p.publish_at >= ?", s.PublishAfter)
	}
	if s.PublishBefore != "" {
		add("p.publish_at < ?", s.PublishBefore)
	}
	if s.HidePast != "" {
		add("NOT "+PastPostSQL, s.HidePast)
	}
	if qs := strings.TrimSpace(s.Query); qs != "" {
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(qs)
		add(`p.content LIKE ? ESCAPE '\' COLLATE NOCASE`, "%"+esc+"%")
	}
	if s.Cursor != "" {
		c, err := decodePostCursor(s.Cursor)
		if err != nil {
			return nil, "", err
		}
		op := ">"
		if sort.desc {
			op = "<"
		}
		add("("+sort.expr+", p.id) "+op+" (?, ?)", c.V, c.ID)
	}
	dir := "ASC"
	if sort.desc {
		dir = "DESC"
	}
	limit := s.Limit
	if limit <= 0 {
		limit = 50
	}
	sql := "SELECT p.*, " + sort.expr + " AS sort_value FROM posts p WHERE " + strings.Join(where, " AND ") +
		" ORDER BY " + sort.expr + " " + dir + ", p.id " + dir + " LIMIT ?"
	args = append(args, limit+1)
	rows, err := q.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []Post
	var lastKey string
	for rows.Next() {
		var p Post
		var key string
		if err := rows.Scan(&p.ID, &p.WorkspaceID, &p.CollectionID, &p.Content, &p.Media, &p.Status, &p.PublishAt, &p.PublishedAt,
			&p.Tags, &p.Metadata, &p.Source, &p.ExternalRef, &p.Version, &p.CreatedAt, &p.UpdatedAt, &p.RepeatEvery, &p.RepeatOf, &p.Thread, &p.CreatedByUser, &key); err != nil {
			return nil, "", err
		}
		if len(out) == limit {
			return out, encodePostCursor(lastKey, out[limit-1].ID), rows.Err()
		}
		out = append(out, p)
		lastKey = key
	}
	return out, "", rows.Err()
}

// TagCount is one tag with how many posts carry it.
type TagCount struct {
	Tag string
	N   int64
}

// PostTagCounts lists the workspace's tags, most used first (json_each is not something sqlc can type).
func (q *Queries) PostTagCounts(ctx context.Context, workspaceID string) ([]TagCount, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT value, COUNT(*) AS n FROM posts, json_each(posts.tags)
		WHERE posts.workspace_id = ? GROUP BY value ORDER BY n DESC, value LIMIT 500`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TagCount
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.Tag, &t.N); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// PostExtra is what a post list shows beside each post: who created it (the post.created event's actor) and how
// many comment threads are still open.
type PostExtra struct {
	CreatedBy    string
	OpenComments int64
}

// PostExtras reads PostExtra for a page of posts in two queries (no per-post round trips).
func (q *Queries) PostExtras(ctx context.Context, workspaceID string, ids []string) (map[string]PostExtra, error) {
	out := map[string]PostExtra{}
	if len(ids) == 0 {
		return out, nil
	}
	in := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := []any{workspaceID}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := q.db.QueryContext(ctx, `SELECT item_id, actor_name FROM events
		WHERE workspace_id = ? AND type = 'post.created' AND item_id IN (`+in+`)`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		e := out[id]
		e.CreatedBy = name
		out[id] = e
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.db.QueryContext(ctx, `SELECT post_id, COUNT(*) FROM post_comments
		WHERE workspace_id = ? AND parent_id = '' AND resolved_at IS NULL AND post_id IN (`+in+`) GROUP BY post_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		e := out[id]
		e.OpenComments = n
		out[id] = e
	}
	return out, rows.Err()
}

// jsonTag is a tag as it is stored in a JSON tags list (json.Marshal: quotes, backslashes, and &, < and > as
// \u0026...), so instr finds R&D too.
func jsonTag(tag string) string {
	b, _ := json.Marshal(tag)
	return string(b)
}
