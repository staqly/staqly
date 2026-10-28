package db

import (
	"context"
	"database/sql"
)

// The analytics page's queries (GET /v1/analytics), written by hand: the filters are optional and the
// grouping is the same on SQLite and PostgreSQL (dates stay text: fixed-width UTC, cut with substr). Every query
// is scoped by workspace_id; only fixed SQL fragments are concatenated, values are bound.
//
// A target's outcome time is when it went out (published_at) or, for a failed one, when it failed (updated_at:
// nothing touches a failed target until it is retried, which makes it pending again).

// AnalyticsFilter narrows the queries: Collection here; channel and source are applied by the caller on the
// grouped rows where a view needs them unfiltered (the by-source view ignores the source filter).
type AnalyticsFilter struct {
	WorkspaceID string
	Collection  string
	// Channel and Source filter the post-level counts (created, queue), which have no channel or source column
	// to group by.
	Channel, Source string
}

// TargetBucket is one group of finished targets: a channel, the post's source, published or failed, and the
// UTC minute it happened ("2026-10-27T14:05"; a minute, so the caller can move it into any time zone).
type TargetBucket struct {
	ChannelID string
	Source    string
	Status    string
	Minute    string
	N         int64
	Retried   int64 // attempts > 1
	Last      string
}

const outcomeAt = "COALESCE(t.published_at, t.updated_at)"

// AnalyticsTargetBuckets groups the workspace's published and failed targets in [from, to).
func (q *Queries) AnalyticsTargetBuckets(ctx context.Context, f AnalyticsFilter, from, to string) ([]TargetBucket, error) {
	query := `SELECT t.channel_id, p.source, t.status, substr(` + outcomeAt + `, 1, 16) AS minute, COUNT(*),
		SUM(t.attempts > 1), MAX(` + outcomeAt + `)
		FROM posts p JOIN post_targets t ON t.post_id = p.id
		WHERE p.workspace_id = ? AND t.status IN ('published', 'failed') AND ` + outcomeAt + ` >= ? AND ` + outcomeAt + ` < ?`
	args := []any{f.WorkspaceID, from, to}
	if f.Collection != "" {
		query += ` AND p.collection_id = ?`
		args = append(args, f.Collection)
	}
	query += ` GROUP BY t.channel_id, p.source, t.status, substr(` + outcomeAt + `, 1, 16)`
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TargetBucket
	for rows.Next() {
		var b TargetBucket
		var retried sql.NullInt64
		if err := rows.Scan(&b.ChannelID, &b.Source, &b.Status, &b.Minute, &b.N, &retried, &b.Last); err != nil {
			return nil, err
		}
		b.Retried = retried.Int64
		out = append(out, b)
	}
	return out, rows.Err()
}

// PublishedTarget is one target that went out: what the delay, distinct posts, metrics and top posts need.
type PublishedTarget struct {
	PostID      string
	ChannelID   string
	Source      string
	PublishAt   sql.NullString
	PublishedAt string
	Metrics     string // cached providers.Metrics JSON, "" when none was read or reported
	MetricsAt   sql.NullString
	ExternalURL string
}

// AnalyticsPublishedTargets lists the workspace's targets published in [from, to), oldest first.
// ponytail: one row per published target (about 100 bytes); fine to tens of thousands a year, beyond that the
// delay percentile and metric sums move into SQL per dialect.
func (q *Queries) AnalyticsPublishedTargets(ctx context.Context, f AnalyticsFilter, from, to string) ([]PublishedTarget, error) {
	query := `SELECT t.post_id, t.channel_id, p.source, p.publish_at, t.published_at, t.metrics, t.metrics_at, t.external_url
		FROM posts p JOIN post_targets t ON t.post_id = p.id
		WHERE p.workspace_id = ? AND t.status = 'published' AND t.published_at >= ? AND t.published_at < ?`
	args := []any{f.WorkspaceID, from, to}
	if f.Collection != "" {
		query += ` AND p.collection_id = ?`
		args = append(args, f.Collection)
	}
	query += ` ORDER BY t.published_at, t.post_id, t.channel_id`
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublishedTarget
	for rows.Next() {
		var t PublishedTarget
		if err := rows.Scan(&t.PostID, &t.ChannelID, &t.Source, &t.PublishAt, &t.PublishedAt, &t.Metrics, &t.MetricsAt, &t.ExternalURL); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// FailureGroup is one error text on one channel for posts of one source, with how often and when last.
type FailureGroup struct {
	Error     string
	ChannelID string
	Source    string
	N         int64
	Last      string
	PostID    string // one post that failed this way (an example to open)
}

// AnalyticsFailures groups the targets that failed in [from, to) by their error.
func (q *Queries) AnalyticsFailures(ctx context.Context, f AnalyticsFilter, from, to string) ([]FailureGroup, error) {
	query := `SELECT t.error, t.channel_id, p.source, COUNT(*), MAX(t.updated_at), MAX(t.post_id)
		FROM posts p JOIN post_targets t ON t.post_id = p.id
		WHERE p.workspace_id = ? AND t.status = 'failed' AND t.updated_at >= ? AND t.updated_at < ?`
	args := []any{f.WorkspaceID, from, to}
	if f.Collection != "" {
		query += ` AND p.collection_id = ?`
		args = append(args, f.Collection)
	}
	query += ` GROUP BY t.error, t.channel_id, p.source`
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FailureGroup
	for rows.Next() {
		var g FailureGroup
		if err := rows.Scan(&g.Error, &g.ChannelID, &g.Source, &g.N, &g.Last, &g.PostID); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// postFilter is the WHERE tail for queries on posts p alone: collection, source and a target on the channel.
func postFilter(f AnalyticsFilter, args []any) (string, []any) {
	query := ""
	if f.Collection != "" {
		query += ` AND p.collection_id = ?`
		args = append(args, f.Collection)
	}
	if f.Source != "" {
		query += ` AND p.source = ?`
		args = append(args, f.Source)
	}
	if f.Channel != "" {
		query += ` AND EXISTS (SELECT 1 FROM post_targets x WHERE x.post_id = p.id AND x.channel_id = ?)`
		args = append(args, f.Channel)
	}
	return query, args
}

// SourceCount is how many posts one source created.
type SourceCount struct {
	Source string
	N      int64
}

// AnalyticsPostsCreated counts the posts created in [from, to) per source (the source filter is not applied:
// the by-source view compares them).
func (q *Queries) AnalyticsPostsCreated(ctx context.Context, f AnalyticsFilter, from, to string) ([]SourceCount, error) {
	f.Source = ""
	tail, args := postFilter(f, []any{f.WorkspaceID, from, to})
	rows, err := q.db.QueryContext(ctx, `SELECT p.source, COUNT(*) FROM posts p
		WHERE p.workspace_id = ? AND p.created_at >= ? AND p.created_at < ?`+tail+` GROUP BY p.source`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceCount
	for rows.Next() {
		var c SourceCount
		if err := rows.Scan(&c.Source, &c.N); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AnalyticsQueue is what waits now: scheduled posts (and when the next goes out) and drafts.
type AnalyticsQueue struct {
	Scheduled     int64
	Drafts        int64
	NextPublishAt sql.NullString
}

func (q *Queries) AnalyticsQueue(ctx context.Context, f AnalyticsFilter) (AnalyticsQueue, error) {
	tail, args := postFilter(f, []any{f.WorkspaceID})
	var out AnalyticsQueue
	var scheduled, drafts sql.NullInt64
	err := q.db.QueryRowContext(ctx, `SELECT SUM(p.status = 'scheduled'), SUM(p.status = 'draft'),
		MIN(CASE WHEN p.status = 'scheduled' THEN p.publish_at END)
		FROM posts p WHERE p.workspace_id = ? AND p.status IN ('scheduled', 'draft')`+tail, args...).
		Scan(&scheduled, &drafts, &out.NextPublishAt)
	out.Scheduled, out.Drafts = scheduled.Int64, drafts.Int64
	return out, err
}

// AnalyticsFeedback counts preview feedback threads: opened and resolved in [from, to), and open now.
type AnalyticsFeedback struct {
	Opened, Resolved, Open int64
}

func (q *Queries) AnalyticsFeedback(ctx context.Context, f AnalyticsFilter, from, to string) (AnalyticsFeedback, error) {
	f.Channel = "" // a thread is on the post (or one channel's version of it); the channel filter doesn't apply
	tail, args := postFilter(f, []any{from, to, from, to, f.WorkspaceID})
	// COUNT(CASE ...), not SUM(a comparison): the PostgreSQL rewrite turns SUM(x = y) into a CASE of its own.
	var out AnalyticsFeedback
	err := q.db.QueryRowContext(ctx, `SELECT
		COUNT(CASE WHEN c.created_at >= ? AND c.created_at < ? THEN 1 END),
		COUNT(CASE WHEN c.resolved_at >= ? AND c.resolved_at < ? THEN 1 END),
		COUNT(CASE WHEN c.resolved_at IS NULL THEN 1 END)
		FROM post_comments c JOIN posts p ON p.id = c.post_id
		WHERE c.workspace_id = ? AND c.parent_id = ''`+tail, args...).Scan(&out.Opened, &out.Resolved, &out.Open)
	return out, err
}

// PostExcerpt is the start of a post's text, for the top posts list.
type PostExcerpt struct {
	ID      string
	Content string
}

// AnalyticsPostExcerpts reads the first 200 characters of a few posts of the workspace.
func (q *Queries) AnalyticsPostExcerpts(ctx context.Context, workspaceID string, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	query := `SELECT id, substr(content, 1, 200) FROM posts WHERE workspace_id = ? AND id IN (?` + repeatPlaceholder(len(ids)-1) + `)`
	args := []any{workspaceID}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e PostExcerpt
		if err := rows.Scan(&e.ID, &e.Content); err != nil {
			return nil, err
		}
		out[e.ID] = e.Content
	}
	return out, rows.Err()
}

func repeatPlaceholder(n int) string {
	s := ""
	for range n {
		s += ", ?"
	}
	return s
}
