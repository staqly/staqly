package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Post statuses: draft (no time, or not yet scheduled) → scheduled → publishing → published | failed.
var PostStatuses = []string{"draft", "scheduled", "publishing", "published", "failed"}

const (
	maxPostContent = 50_000
	maxPostMedia   = 20
	maxTags        = 20
	maxTagLen      = 40
	maxTargets     = 50
)

// PostTarget is one post on one channel.
type PostTarget struct {
	ChannelID   string            `json:"channel_id"`
	ChannelName string            `json:"channel_name"`
	Provider    string            `json:"provider"`
	AvatarURL   string            `json:"avatar_url"`
	Content     *string           `json:"content"`
	Settings    map[string]string `json:"settings"`
	Status      string            `json:"status"`
	ExternalID  string            `json:"external_id"`
	ExternalURL string            `json:"external_url"`
	Error       string            `json:"error"`
	Attempts    int64             `json:"attempts"`
	PublishedAt *string           `json:"published_at"`
	Replies     []Reply           `json:"replies"` // one per thread part
}

type Post struct {
	ID             string          `json:"id"`
	CollectionID   string          `json:"collection_id"`
	CollectionName string          `json:"collection_name"`
	Content        string          `json:"content"`
	Media          []string        `json:"media"`
	MediaItems     []Media         `json:"media_items"`
	Status         string          `json:"status"`
	PublishAt      *string         `json:"publish_at"`
	PublishedAt    *string         `json:"published_at"`
	Tags           []string        `json:"tags"`
	Metadata       json.RawMessage `json:"metadata"`
	Source         string          `json:"source"`
	ExternalRef    string          `json:"external_ref"`
	RepeatEvery    string          `json:"repeat_every"`
	Thread         []ThreadPartOut `json:"thread"`
	Version        int64           `json:"version"`
	Targets        []PostTarget    `json:"targets"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
}

type PostTargetInput struct {
	ChannelID string            `json:"channel_id"`
	Content   *string           `json:"content"`
	Settings  map[string]string `json:"settings"`
}

type PostInput struct {
	Collection  string            `json:"collection"`
	Content     string            `json:"content"`
	Media       []string          `json:"media"`
	Targets     []PostTargetInput `json:"targets"`
	PublishAt   *string           `json:"publish_at"`
	Status      string            `json:"status"` // draft (default) or scheduled (needs publish_at and a target)
	PublishNow  bool              `json:"publish_now"`
	Tags        []string          `json:"tags"`
	Metadata    json.RawMessage   `json:"metadata"`
	Source      string            `json:"source"`
	ExternalRef string            `json:"external_ref"`
	RepeatEvery string            `json:"repeat_every"` // '' (no repeat), 1d..6d, 1w, 2w or 1mo
	Thread      []ThreadPart      `json:"thread"`       // follow-up parts published as replies
	repeatOf    string            // the post this one repeats (set by the repeat worker only)
}

type PostPatch struct {
	Collection      *string            `json:"collection"`
	Content         *string            `json:"content"`
	Media           *[]string          `json:"media"`
	Targets         *[]PostTargetInput `json:"targets"`
	PublishAt       json.RawMessage    `json:"publish_at"` // a time, or null to clear
	Tags            *[]string          `json:"tags"`
	Metadata        json.RawMessage    `json:"metadata"`
	ExternalRef     *string            `json:"external_ref"`
	RepeatEvery     json.RawMessage    `json:"repeat_every"` // a choice, or "" / null to stop repeating
	Thread          *[]ThreadPart      `json:"thread"`       // replaces the thread; [] clears it
	ExpectedVersion *int64             `json:"expected_version"`
}

type PostFilter struct {
	CollectionID, Status, ChannelID, Q, Tag string
	PublishAfter, PublishBefore             string
	Sort                                    string
	Limit                                   int
	Cursor                                  string
}

type PostPage struct {
	Data       []Post `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func targetOut(t db.ListTargetsRow) PostTarget {
	out := PostTarget{ChannelID: t.ChannelID, ChannelName: t.ChannelName, Provider: t.Provider, AvatarURL: t.ChannelAvatarUrl, Content: ptr(t.Content),
		Settings: map[string]string{}, Status: t.Status, ExternalID: t.ExternalID, ExternalURL: t.ExternalUrl, Error: t.Error,
		Attempts: t.Attempts, PublishedAt: ptr(t.PublishedAt)}
	_ = json.Unmarshal([]byte(t.Settings), &out.Settings)
	return out
}

func (s *Service) toPost(ctx context.Context, q *db.Queries, p db.Post, targets []db.ListTargetsRow, collectionName string) Post {
	out := Post{ID: p.ID, CollectionID: p.CollectionID, CollectionName: collectionName, Content: p.Content, Media: []string{}, MediaItems: []Media{},
		Status: p.Status, PublishAt: ptr(p.PublishAt), PublishedAt: ptr(p.PublishedAt), Tags: []string{},
		Metadata: json.RawMessage(p.Metadata), Source: p.Source, ExternalRef: p.ExternalRef, RepeatEvery: p.RepeatEvery, Version: p.Version,
		Thread: s.threadOut(ctx, q, p.WorkspaceID, p.Thread), Targets: []PostTarget{}, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
	_ = json.Unmarshal([]byte(p.Media), &out.Media)
	_ = json.Unmarshal([]byte(p.Tags), &out.Tags)
	for _, id := range out.Media {
		// ponytail: one lookup per file; join in SQL if list pages get slow.
		if m, err := q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id}); err == nil {
			out.MediaItems = append(out.MediaItems, s.toMedia(m))
		}
	}
	for _, t := range targets {
		pt := targetOut(t)
		pt.Replies = repliesFor(len(out.Thread), t.Replies)
		out.Targets = append(out.Targets, pt)
	}
	return out
}

// loadPost reads a post with its targets and collection name (any Queries: inside or outside a tx).
func (s *Service) loadPost(ctx context.Context, q *db.Queries, workspaceID, id string) (Post, db.Post, error) {
	row, err := q.GetPost(ctx, db.GetPostParams{WorkspaceID: workspaceID, ID: id})
	if isNoRows(err) {
		return Post{}, row, notFound("post")
	}
	if err != nil {
		return Post{}, row, err
	}
	targets, err := q.ListTargets(ctx, id)
	if err != nil {
		return Post{}, row, err
	}
	name := ""
	if c, err := q.GetCollectionByID(ctx, row.CollectionID); err == nil {
		name = c.Name
	}
	return s.toPost(ctx, q, row, targets, name), row, nil
}

func normalizeTags(in []string) ([]string, error) {
	out := []string{}
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if len([]rune(t)) > maxTagLen {
			return nil, invalid("tags", fmt.Sprintf("each tag is at most %d characters", maxTagLen))
		}
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	if len(out) > maxTags {
		return nil, invalid("tags", fmt.Sprintf("at most %d tags", maxTags))
	}
	return out, nil
}

// parsePublishAt accepts RFC3339 (any offset) and returns the stored form; it must be in the future.
func (s *Service) parsePublishAt(v string) (string, error) {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		if t, err = time.Parse(time.RFC3339, v); err != nil {
			return "", invalid("publish_at", "must be an RFC3339 time")
		}
	}
	if !t.After(s.Now()) {
		return "", invalid("publish_at", "must be in the future")
	}
	if t.After(s.Now().Add(366 * 24 * time.Hour)) {
		return "", invalid("publish_at", "must be within a year")
	}
	return formatTime(t), nil
}

func (s *Service) checkMedia(ctx context.Context, q *db.Queries, workspaceID string, ids []string) error {
	if len(ids) > maxPostMedia {
		return invalid("media", fmt.Sprintf("at most %d files", maxPostMedia))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return invalid("media", "the same file is listed twice")
		}
		seen[id] = true
		if _, err := q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: workspaceID, ID: id}); err != nil {
			if isNoRows(err) {
				return invalid("media", "unknown media id "+id)
			}
			return err
		}
	}
	return nil
}

// resolveTargets checks the channels exist, are usable and accept the post (provider limits and settings).
func (s *Service) resolveTargets(ctx context.Context, q *db.Queries, workspaceID, content string, media []string, in []PostTargetInput) (map[string]db.Channel, error) {
	if len(in) > maxTargets {
		return nil, invalid("targets", fmt.Sprintf("at most %d channels", maxTargets))
	}
	ids := make([]string, 0, len(in))
	for _, t := range in {
		if t.ChannelID == "" {
			return nil, invalid("targets", "channel_id is required")
		}
		if slices.Contains(ids, t.ChannelID) {
			return nil, invalid("targets", "channel "+t.ChannelID+" is listed twice")
		}
		ids = append(ids, t.ChannelID)
	}
	chans := map[string]db.Channel{}
	if len(ids) == 0 {
		return chans, nil
	}
	rows, err := q.ChannelsByIDs(ctx, db.ChannelsByIDsParams{WorkspaceID: workspaceID, Ids: mustJSON(ids)})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		chans[r.ID] = r
	}
	files, err := s.mediaFiles(ctx, q, workspaceID, media)
	if err != nil {
		return nil, err
	}
	for _, t := range in {
		ch, ok := chans[t.ChannelID]
		if !ok {
			return nil, invalid("targets."+t.ChannelID, "unknown channel")
		}
		if ch.Status == "disabled" {
			return nil, invalid("targets."+t.ChannelID, "this channel is turned off")
		}
		e, ok := providers.Get(ch.Provider)
		if !ok {
			return nil, invalid("targets."+t.ChannelID, "this channel's provider is not part of this build")
		}
		text := content
		if t.Content != nil {
			text = *t.Content
		}
		if err := e.Validate(providers.PostContent{Text: text, Media: files}, providers.Settings(t.Settings)); err != nil {
			return nil, targetError(t.ChannelID, err)
		}
	}
	return chans, nil
}

// targetError places a provider's objection where the composer shows it: the content (text and media) or a
// settings field.
func targetError(channelID string, err error) error {
	var ve *providers.ValidationError
	if errors.As(err, &ve) {
		if ve.Field == "content" || ve.Field == "media" {
			return invalid("targets."+channelID+".content", ve.Msg)
		}
		return invalid("targets."+channelID+".settings."+ve.Field, ve.Msg)
	}
	return invalid("targets."+channelID+".content", err.Error())
}

func (s *Service) mediaFiles(ctx context.Context, q *db.Queries, workspaceID string, ids []string) ([]providers.MediaFile, error) {
	out := make([]providers.MediaFile, 0, len(ids))
	for _, id := range ids {
		m, err := q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: workspaceID, ID: id})
		if err != nil {
			if isNoRows(err) {
				continue
			}
			return nil, err
		}
		out = append(out, providers.MediaFile{ID: m.ID, Name: m.OriginalName, MimeType: m.MimeType, Size: m.SizeBytes,
			Path: s.mediaPath(m.StorageName), URL: strings.TrimRight(s.Cfg.PublicURL, "/") + "/v1/media/" + m.ID})
	}
	return out, nil
}

func (s *Service) writeTargets(ctx context.Context, q *db.Queries, postID string, in []PostTargetInput, now string) error {
	keep := make([]string, 0, len(in))
	for _, t := range in {
		settings := t.Settings
		if settings == nil {
			settings = map[string]string{}
		}
		var content string
		if t.Content != nil {
			content = *t.Content
		}
		if err := q.UpsertTarget(ctx, db.UpsertTargetParams{PostID: postID, ChannelID: t.ChannelID, Content: nullStr(content),
			Settings: mustJSON(settings), UpdatedAt: now}); err != nil {
			return err
		}
		keep = append(keep, t.ChannelID)
	}
	return q.DeleteTargetsNotIn(ctx, db.DeleteTargetsNotInParams{PostID: postID, Keep: mustJSON(keep)})
}

func (s *Service) postEventData(ctx context.Context, q *db.Queries, workspaceID, id string, extra map[string]any) map[string]any {
	post, row, err := s.loadPost(ctx, q, workspaceID, id)
	data := map[string]any{}
	if err == nil {
		data["post"] = post
		data["collection"] = map[string]any{"id": row.CollectionID, "name": post.CollectionName}
	}
	for k, v := range extra {
		data[k] = v
	}
	return data
}

// ---- create ----

func (s *Service) CreatePost(ctx context.Context, p Principal, in PostInput) (Post, error) {
	if err := p.require(ScopePostsWrite); err != nil {
		return Post{}, err
	}
	if in.Collection == "" {
		return Post{}, invalid("collection", "is required")
	}
	col, err := s.findCollection(ctx, p, in.Collection)
	if err != nil {
		return Post{}, err
	}
	if col.Archived {
		return Post{}, conflict("collection is archived")
	}
	if in.Status == "" {
		in.Status = "draft"
	}
	if in.Status != "draft" && in.Status != "scheduled" {
		return Post{}, invalid("status", "a new post is draft or scheduled")
	}
	if len([]rune(in.Content)) > maxPostContent {
		return Post{}, invalid("content", fmt.Sprintf("at most %d characters", maxPostContent))
	}
	tags, err := normalizeTags(in.Tags)
	if err != nil {
		return Post{}, err
	}
	metadata := "{}"
	if len(in.Metadata) > 0 && string(in.Metadata) != "null" {
		var m map[string]any
		if err := json.Unmarshal(in.Metadata, &m); err != nil {
			return Post{}, invalid("metadata", "must be a JSON object")
		}
		metadata = string(in.Metadata)
	}
	source, err := trimmed(in.Source, "source", 200, false)
	if err != nil {
		return Post{}, err
	}
	if source == "" {
		source = agentFrom(ctx)
		if source == "" && p.Kind == PrincipalSession {
			source = "dashboard"
		}
	}
	externalRef, err := trimmed(in.ExternalRef, "external_ref", 200, false)
	if err != nil {
		return Post{}, err
	}
	repeatEvery, err := checkRepeat(in.RepeatEvery)
	if err != nil {
		return Post{}, err
	}
	var publishAt string
	if in.PublishAt != nil && *in.PublishAt != "" {
		if publishAt, err = s.parsePublishAt(*in.PublishAt); err != nil {
			return Post{}, err
		}
	}
	if in.Status == "scheduled" || in.PublishNow {
		if err := p.require(ScopePostsPublish); err != nil {
			return Post{}, err
		}
	}
	if in.Status == "scheduled" {
		if publishAt == "" {
			return Post{}, invalid("publish_at", "a scheduled post needs a time")
		}
		if len(in.Targets) == 0 {
			return Post{}, invalid("targets", "a scheduled post needs at least one channel")
		}
	}
	if in.PublishNow && len(in.Targets) == 0 {
		return Post{}, invalid("targets", "publishing needs at least one channel")
	}
	if in.Media == nil {
		in.Media = []string{}
	}
	now := s.now()
	row := db.Post{ID: newID("post"), WorkspaceID: p.WorkspaceID, CollectionID: col.ID, Content: in.Content, Media: mustJSON(in.Media),
		Status: in.Status, PublishAt: nullStr(publishAt), Tags: mustJSON(tags), Metadata: metadata, Source: source,
		ExternalRef: externalRef, RepeatEvery: repeatEvery, RepeatOf: in.repeatOf, Version: 1, CreatedAt: now, UpdatedAt: now}
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := s.checkMedia(ctx, q, p.WorkspaceID, in.Media); err != nil {
			return err
		}
		chans, err := s.resolveTargets(ctx, q, p.WorkspaceID, in.Content, in.Media, in.Targets)
		if err != nil {
			return err
		}
		thread, err := s.checkThread(ctx, q, p.WorkspaceID, in.Thread)
		if err != nil {
			return err
		}
		if err := checkThreadLimits(thread, chans); err != nil {
			return err
		}
		if err := s.usePostQuota(ctx, q, p.WorkspaceID); err != nil {
			return err
		}
		if err := q.CreatePost(ctx, db.CreatePostParams{ID: row.ID, WorkspaceID: row.WorkspaceID, CollectionID: row.CollectionID,
			Content: row.Content, Media: row.Media, Status: row.Status, PublishAt: row.PublishAt, Tags: row.Tags, Metadata: row.Metadata,
			Source: row.Source, ExternalRef: row.ExternalRef, RepeatEvery: row.RepeatEvery, RepeatOf: row.RepeatOf,
			CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		if len(thread) > 0 {
			if err := q.SetPostThread(ctx, db.SetPostThreadParams{Thread: mustJSON(thread), ID: row.ID}); err != nil {
				return err
			}
		}
		if err := s.writeTargets(ctx, q, row.ID, in.Targets, now); err != nil {
			return err
		}
		if _, err := s.emit(ctx, q, p, "post.created", col.ID, row.ID, s.postEventData(ctx, q, p.WorkspaceID, row.ID, nil)); err != nil {
			return err
		}
		if row.Status == "scheduled" {
			if err := s.schedulePublish(ctx, q, p, row.WorkspaceID, row.ID, publishAt, nil); err != nil {
				return err
			}
		}
		if in.PublishNow {
			return s.startPublish(ctx, q, p, p.WorkspaceID, row.ID)
		}
		return nil
	})
	if err != nil {
		return Post{}, err
	}
	return s.GetPost(ctx, p, row.ID)
}

// schedulePublish queues the publish_post job at publishAt and records post.scheduled.
func (s *Service) schedulePublish(ctx context.Context, q *db.Queries, p Principal, workspaceID, postID, publishAt string, previous *string) error {
	at, _ := time.Parse(timeFormat, publishAt)
	if err := s.enqueue(ctx, q, workspaceID, "publish_post", "publish_post:"+postID, map[string]any{"post_id": postID}, at, 1); err != nil {
		return err
	}
	extra := map[string]any{"publish_at": publishAt}
	if previous != nil {
		extra["previous_publish_at"] = *previous
	}
	_, err := s.emit(ctx, q, p, "post.scheduled", "", postID, s.postEventData(ctx, q, workspaceID, postID, extra))
	return err
}

// ---- read ----

func (s *Service) GetPost(ctx context.Context, p Principal, id string) (Post, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return Post{}, err
	}
	post, _, err := s.loadPost(ctx, s.Q, p.WorkspaceID, id)
	return post, err
}

func (s *Service) ListPosts(ctx context.Context, p Principal, f PostFilter) (PostPage, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return PostPage{}, err
	}
	if f.Status != "" && !slices.Contains(PostStatuses, f.Status) {
		return PostPage{}, invalid("status", "must be one of "+strings.Join(PostStatuses, ", "))
	}
	if f.Sort == "" {
		f.Sort = "publish_asc"
	}
	if !db.ValidPostSort(f.Sort) {
		return PostPage{}, invalid("sort", "unknown sort")
	}
	search := db.PostSearch{WorkspaceID: p.WorkspaceID, CollectionID: f.CollectionID, Status: f.Status, ChannelID: f.ChannelID,
		Tag: f.Tag, Query: f.Q, Sort: f.Sort, Cursor: f.Cursor, Limit: clampLimit(f.Limit, 50, 200)}
	for _, t := range []struct {
		field, v string
		dst      *string
	}{{"publish_after", f.PublishAfter, &search.PublishAfter}, {"publish_before", f.PublishBefore, &search.PublishBefore}} {
		v, err := ParseTimeFilter(t.field, t.v)
		if err != nil {
			return PostPage{}, err
		}
		if v != nil {
			*t.dst = v.(string)
		}
	}
	timeout := s.SearchTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	qctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rows, next, err := s.Q.SearchPosts(qctx, search)
	if errors.Is(err, db.ErrBadCursor) {
		return PostPage{}, invalid("cursor", "is invalid")
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return PostPage{}, &Error{Code: "unavailable", Status: 503, Message: "the listing took too long; try again"}
		}
		return PostPage{}, err
	}
	page := PostPage{Data: []Post{}, NextCursor: next}
	if len(rows) == 0 {
		return page, nil
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	targets, err := s.Q.ListTargetsForPosts(ctx, mustJSON(ids))
	if err != nil {
		return PostPage{}, err
	}
	byPost := map[string][]db.ListTargetsRow{}
	for _, t := range targets {
		byPost[t.PostID] = append(byPost[t.PostID], db.ListTargetsRow(t))
	}
	names := map[string]string{}
	if cols, err := s.Q.ListCollections(ctx, db.ListCollectionsParams{WorkspaceID: p.WorkspaceID, IncludeArchived: 1}); err == nil {
		for _, c := range cols {
			names[c.ID] = c.Name
		}
	}
	for _, r := range rows {
		page.Data = append(page.Data, s.toPost(ctx, s.Q, r, byPost[r.ID], names[r.CollectionID]))
	}
	return page, nil
}

type Tag struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

func (s *Service) ListTags(ctx context.Context, p Principal) ([]Tag, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return nil, err
	}
	rows, err := s.Q.PostTagCounts(ctx, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]Tag, 0, len(rows))
	for _, r := range rows {
		out = append(out, Tag{Name: r.Tag, Count: r.N})
	}
	return out, nil
}

// ---- update ----

func editable(status string) error {
	switch status {
	case "published":
		return conflict("this post is published; duplicate it to post it again")
	case "publishing":
		return conflict("this post is being published right now")
	}
	return nil
}

func (s *Service) UpdatePost(ctx context.Context, p Principal, id string, in PostPatch) (Post, error) {
	if err := p.require(ScopePostsWrite); err != nil {
		return Post{}, err
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		row, err := q.GetPost(ctx, db.GetPostParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("post")
		}
		if err != nil {
			return err
		}
		if in.ExpectedVersion != nil && *in.ExpectedVersion != row.Version {
			return conflict(fmt.Sprintf("post was modified (version %d, expected %d)", row.Version, *in.ExpectedVersion))
		}
		if err := editable(row.Status); err != nil {
			return err
		}
		before := row
		if in.Collection != nil {
			col, err := s.findCollection(ctx, p, *in.Collection)
			if err != nil {
				return err
			}
			if col.Archived {
				return conflict("collection is archived")
			}
			row.CollectionID = col.ID
		}
		if in.Content != nil {
			if len([]rune(*in.Content)) > maxPostContent {
				return invalid("content", fmt.Sprintf("at most %d characters", maxPostContent))
			}
			row.Content = *in.Content
		}
		media := []string{}
		_ = json.Unmarshal([]byte(row.Media), &media)
		if in.Media != nil {
			media = *in.Media
			if media == nil {
				media = []string{}
			}
			if err := s.checkMedia(ctx, q, p.WorkspaceID, media); err != nil {
				return err
			}
			row.Media = mustJSON(media)
		}
		if in.Tags != nil {
			tags, err := normalizeTags(*in.Tags)
			if err != nil {
				return err
			}
			row.Tags = mustJSON(tags)
		}
		if len(in.Metadata) > 0 && string(in.Metadata) != "null" {
			var m map[string]any
			if err := json.Unmarshal(in.Metadata, &m); err != nil {
				return invalid("metadata", "must be a JSON object")
			}
			row.Metadata = string(in.Metadata)
		}
		if in.ExternalRef != nil {
			if row.ExternalRef, err = trimmed(*in.ExternalRef, "external_ref", 200, false); err != nil {
				return err
			}
		}
		if len(in.RepeatEvery) > 0 {
			var v string
			if string(in.RepeatEvery) != "null" {
				if err := json.Unmarshal(in.RepeatEvery, &v); err != nil {
					return invalid("repeat_every", repeatChoices)
				}
			}
			if row.RepeatEvery, err = checkRepeat(v); err != nil {
				return err
			}
		}
		var previous *string
		if len(in.PublishAt) > 0 {
			if string(in.PublishAt) == "null" || string(in.PublishAt) == `""` {
				row.PublishAt = nullStr("")
			} else {
				var v string
				if err := json.Unmarshal(in.PublishAt, &v); err != nil {
					return invalid("publish_at", "must be an RFC3339 time or null")
				}
				at, err := s.parsePublishAt(v)
				if err != nil {
					return err
				}
				if before.PublishAt.Valid && before.PublishAt.String != at {
					prev := before.PublishAt.String
					previous = &prev
				}
				row.PublishAt = nullStr(at)
			}
		}
		// Targets: the current ones re-validated against the new content, or the new list.
		current, err := q.ListTargets(ctx, id)
		if err != nil {
			return err
		}
		targets := make([]PostTargetInput, 0, len(current))
		for _, t := range current {
			settings := map[string]string{}
			_ = json.Unmarshal([]byte(t.Settings), &settings)
			targets = append(targets, PostTargetInput{ChannelID: t.ChannelID, Content: ptr(t.Content), Settings: settings})
		}
		if in.Targets != nil {
			for _, t := range current {
				if t.Status == "published" && !slices.ContainsFunc(*in.Targets, func(n PostTargetInput) bool { return n.ChannelID == t.ChannelID }) {
					return conflict("a channel this post is already published on cannot be removed")
				}
			}
			targets = *in.Targets
		}
		chans, err := s.resolveTargets(ctx, q, p.WorkspaceID, row.Content, media, targets)
		if err != nil {
			return err
		}
		thread := parseThread(row.Thread)
		if in.Thread != nil {
			if thread, err = s.checkThread(ctx, q, p.WorkspaceID, *in.Thread); err != nil {
				return err
			}
		}
		if err := checkThreadLimits(thread, chans); err != nil {
			return err
		}
		if row.Status == "scheduled" {
			if !row.PublishAt.Valid {
				row.Status = "draft" // the time went away: no longer scheduled
			}
			if len(targets) == 0 {
				return invalid("targets", "a scheduled post needs at least one channel")
			}
		}
		row.UpdatedAt = s.now()
		n, err := q.UpdatePost(ctx, db.UpdatePostParams{CollectionID: row.CollectionID, Content: row.Content, Media: row.Media,
			Status: row.Status, PublishAt: row.PublishAt, PublishedAt: row.PublishedAt, Tags: row.Tags, Metadata: row.Metadata,
			Source: row.Source, ExternalRef: row.ExternalRef, RepeatEvery: row.RepeatEvery, UpdatedAt: row.UpdatedAt, WorkspaceID: p.WorkspaceID, ID: id, Version: before.Version})
		if err != nil {
			return err
		}
		if n == 0 {
			return conflict("post was modified concurrently")
		}
		if in.Thread != nil {
			if err := q.SetPostThread(ctx, db.SetPostThreadParams{Thread: mustJSON(thread), ID: id}); err != nil {
				return err
			}
		}
		if in.Targets != nil {
			if err := s.writeTargets(ctx, q, id, targets, row.UpdatedAt); err != nil {
				return err
			}
		}
		if _, err := s.emit(ctx, q, p, "post.updated", row.CollectionID, id, s.postEventData(ctx, q, p.WorkspaceID, id, nil)); err != nil {
			return err
		}
		switch {
		case before.Status == "scheduled" && row.Status == "draft":
			if _, err := q.CancelJob(ctx, db.CancelJobParams{Now: nullStr(row.UpdatedAt), Key: "publish_post:" + id}); err != nil {
				return err
			}
			_, err = s.emit(ctx, q, p, "post.unscheduled", row.CollectionID, id, s.postEventData(ctx, q, p.WorkspaceID, id, nil))
			return err
		case row.Status == "scheduled" && before.PublishAt != row.PublishAt:
			return s.schedulePublish(ctx, q, p, p.WorkspaceID, id, row.PublishAt.String, previous)
		}
		return nil
	})
	if err != nil {
		return Post{}, err
	}
	return s.GetPost(ctx, p, id)
}

// SchedulePost sets the time and makes the post scheduled (from draft, scheduled or failed).
func (s *Service) SchedulePost(ctx context.Context, p Principal, id, publishAt string) (Post, error) {
	if err := p.require(ScopePostsPublish); err != nil {
		return Post{}, err
	}
	at, err := s.parsePublishAt(publishAt)
	if err != nil {
		return Post{}, err
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		row, err := q.GetPost(ctx, db.GetPostParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("post")
		}
		if err != nil {
			return err
		}
		if err := editable(row.Status); err != nil {
			return err
		}
		targets, err := q.ListTargets(ctx, id)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			return invalid("targets", "add at least one channel before scheduling")
		}
		var previous *string
		if row.PublishAt.Valid {
			prev := row.PublishAt.String
			previous = &prev
		}
		now := s.now()
		if row.Status == "failed" {
			if err := q.ResetTargets(ctx, db.ResetTargetsParams{UpdatedAt: now, PostID: id}); err != nil {
				return err
			}
		}
		if n, err := q.UpdatePost(ctx, db.UpdatePostParams{CollectionID: row.CollectionID, Content: row.Content, Media: row.Media,
			Status: "scheduled", PublishAt: nullStr(at), PublishedAt: row.PublishedAt, Tags: row.Tags, Metadata: row.Metadata,
			Source: row.Source, ExternalRef: row.ExternalRef, RepeatEvery: row.RepeatEvery, UpdatedAt: now, WorkspaceID: p.WorkspaceID, ID: id, Version: row.Version}); err != nil || n == 0 {
			if err == nil {
				err = conflict("post was modified concurrently")
			}
			return err
		}
		return s.schedulePublish(ctx, q, p, p.WorkspaceID, id, at, previous)
	})
	if err != nil {
		return Post{}, err
	}
	return s.GetPost(ctx, p, id)
}

// PublishPost publishes now (from draft, scheduled or failed).
func (s *Service) PublishPost(ctx context.Context, p Principal, id string) (Post, error) {
	if err := p.require(ScopePostsPublish); err != nil {
		return Post{}, err
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		row, err := q.GetPost(ctx, db.GetPostParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("post")
		}
		if err != nil {
			return err
		}
		if err := editable(row.Status); err != nil {
			return err
		}
		targets, err := q.ListTargets(ctx, id)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			return invalid("targets", "add at least one channel before publishing")
		}
		if _, err := q.CancelJob(ctx, db.CancelJobParams{Now: nullStr(s.now()), Key: "publish_post:" + id}); err != nil {
			return err
		}
		return s.startPublish(ctx, q, p, p.WorkspaceID, id)
	})
	if err != nil {
		return Post{}, err
	}
	return s.GetPost(ctx, p, id)
}

// CancelPost takes a scheduled post back to draft (its time stays as a note; the job goes).
func (s *Service) CancelPost(ctx context.Context, p Principal, id string) (Post, error) {
	if err := p.require(ScopePostsPublish); err != nil {
		return Post{}, err
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		row, err := q.GetPost(ctx, db.GetPostParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("post")
		}
		if err != nil {
			return err
		}
		if row.Status != "scheduled" {
			return conflict("only a scheduled post can be taken off the schedule")
		}
		now := s.now()
		if _, err := q.CancelJob(ctx, db.CancelJobParams{Now: nullStr(now), Key: "publish_post:" + id}); err != nil {
			return err
		}
		if err := q.SetPostStatus(ctx, db.SetPostStatusParams{Status: "draft", PublishedAt: nullStr(""), UpdatedAt: now, ID: id}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "post.unscheduled", row.CollectionID, id, s.postEventData(ctx, q, p.WorkspaceID, id, nil))
		return err
	})
	if err != nil {
		return Post{}, err
	}
	return s.GetPost(ctx, p, id)
}

// DuplicatePost copies content, media, tags, metadata and targets into a new draft.
func (s *Service) DuplicatePost(ctx context.Context, p Principal, id string) (Post, error) {
	if err := p.require(ScopePostsWrite); err != nil {
		return Post{}, err
	}
	src, row, err := s.loadPost(ctx, s.Q, p.WorkspaceID, id)
	if err != nil {
		return Post{}, err
	}
	in := PostInput{Collection: row.CollectionID, Content: src.Content, Media: src.Media, Tags: src.Tags, Metadata: src.Metadata,
		ExternalRef: src.ExternalRef, RepeatEvery: src.RepeatEvery, Thread: parseThread(row.Thread), Status: "draft"}
	for _, t := range src.Targets {
		in.Targets = append(in.Targets, PostTargetInput{ChannelID: t.ChannelID, Content: t.Content, Settings: t.Settings})
	}
	return s.CreatePost(ctx, p, in)
}

func (s *Service) DeletePost(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopePostsDelete); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		row, err := q.GetPost(ctx, db.GetPostParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("post")
		}
		if err != nil {
			return err
		}
		data := s.postEventData(ctx, q, p.WorkspaceID, id, nil)
		targets, _ := q.ListTargets(ctx, id)
		now := nullStr(s.now())
		for _, t := range targets {
			for _, k := range []string{"publish_target:", "target_timeout:"} {
				if _, err := q.CancelJob(ctx, db.CancelJobParams{Now: now, Key: k + id + ":" + t.ChannelID}); err != nil {
					return err
				}
			}
		}
		if _, err := q.CancelJob(ctx, db.CancelJobParams{Now: now, Key: "publish_post:" + id}); err != nil {
			return err
		}
		if _, err := q.DeletePost(ctx, db.DeletePostParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "post.deleted", row.CollectionID, id, data)
		return err
	})
}

// ---- publishing ----

// startPublish moves a post and its unpublished targets to publishing and queues one job per target.
func (s *Service) startPublish(ctx context.Context, q *db.Queries, p Principal, workspaceID, id string) error {
	now := s.now()
	if err := q.SetPostStatus(ctx, db.SetPostStatusParams{Status: "publishing", PublishedAt: nullStr(""), UpdatedAt: now, ID: id}); err != nil {
		return err
	}
	targets, err := q.ListTargets(ctx, id)
	if err != nil {
		return err
	}
	queued := 0
	for _, t := range targets {
		if t.Status == "published" {
			continue
		}
		if err := q.SetTargetStatus(ctx, db.SetTargetStatusParams{Status: "publishing", ExternalID: t.ExternalID, ExternalUrl: t.ExternalUrl,
			Error: "", Attempts: t.Attempts, PublishedAt: t.PublishedAt, UpdatedAt: now, PostID: id, ChannelID: t.ChannelID}); err != nil {
			return err
		}
		if err := s.enqueue(ctx, q, workspaceID, "publish_target", "publish_target:"+id+":"+t.ChannelID,
			map[string]any{"post_id": id, "channel_id": t.ChannelID}, s.Now(), 3); err != nil {
			return err
		}
		queued++
	}
	if queued == 0 {
		return s.settlePost(ctx, q, p, workspaceID, id)
	}
	return nil
}

// publishTarget is the publish_target job: one try to publish one target.
func (s *Service) publishTarget(ctx context.Context, postID, channelID string) error {
	row, err := s.Q.GetPostByID(ctx, postID)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	target, err := s.Q.GetTarget(ctx, db.GetTargetParams{PostID: postID, ChannelID: channelID})
	if isNoRows(err) || (err == nil && target.Status != "publishing") {
		return nil // removed, or settled by a result report meanwhile
	}
	if err != nil {
		return err
	}
	ch, err := s.Q.GetChannelByID(ctx, channelID)
	if err != nil {
		return s.finishTarget(ctx, postID, channelID, "failed", providers.Result{}, "channel was removed")
	}
	sys := Principal{WorkspaceID: row.WorkspaceID, Kind: PrincipalSystem, ID: "publisher", Name: "Publisher"}
	if ch.Status == "disabled" {
		return s.finishTarget(ctx, postID, channelID, "failed", providers.Result{}, "channel is turned off")
	}
	if ch.Status == "needs_reconnect" {
		return s.finishTarget(ctx, postID, channelID, "failed", providers.Result{}, "channel needs reconnecting")
	}
	e, ok := providers.Get(ch.Provider)
	if !ok {
		return s.finishTarget(ctx, postID, channelID, "failed", providers.Result{}, "provider not available in this build")
	}
	var media []string
	_ = json.Unmarshal([]byte(row.Media), &media)
	files, err := s.mediaFiles(ctx, s.Q, row.WorkspaceID, media)
	if err != nil {
		return err
	}
	text := row.Content
	if target.Content.Valid {
		text = target.Content.String
	}
	var tags []string
	_ = json.Unmarshal([]byte(row.Tags), &tags)
	thread, err := s.threadContent(ctx, s.Q, row.WorkspaceID, row.Thread)
	if err != nil {
		return err
	}
	content := providers.PostContent{PostID: postID, ChannelID: channelID, Text: text, Media: files, Tags: tags, Thread: thread}
	settings := providers.Settings{}
	_ = json.Unmarshal([]byte(target.Settings), &settings)
	if err := e.Validate(content, settings); err != nil {
		return s.finishTarget(ctx, postID, channelID, "failed", providers.Result{}, err.Error())
	}
	var creds providers.Credentials
	if err := s.unseal(ch.Credentials, &creds); err != nil {
		return s.finishTarget(ctx, postID, channelID, "failed", providers.Result{}, "stored credentials cannot be read: "+err.Error())
	}
	if creds, err = s.refreshIfDue(ctx, ch, e, creds); err != nil {
		if errors.Is(err, providers.ErrReauth) {
			return s.finishTarget(ctx, postID, channelID, "failed", providers.Result{}, err.Error())
		}
		return err
	}
	// The run's budget comes from the job (jobRunTimeout["publish_target"]: uploads and the platform's
	// processing included). Platform calls (and your own endpoint) go through the same SSRF guard as webhooks.
	pctx := providers.WithHTTPClient(ctx, s.safeClient())
	result, err := e.Publish(pctx, creds, content, settings)
	// Whatever the platform answered is recorded even if the budget ran out or the lease was lost meanwhile:
	// the post may already be live, and forgetting that would publish it twice on the next attempt.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobWriteTimeout)
	defer cancel()
	switch {
	case errors.Is(err, providers.ErrReauth):
		// The platform no longer accepts the sign-in: the channel needs reconnecting, retrying would not help.
		_ = s.channelNeedsReconnect(wctx, ch, err)
		return s.finishTarget(wctx, postID, channelID, "failed", providers.Result{}, err.Error())
	case errors.Is(err, providers.ErrRejected):
		return s.finishTarget(wctx, postID, channelID, "failed", providers.Result{}, err.Error())
	case errors.Is(err, providers.ErrAsync):
		// Your own automation: tell it, then wait for its result (or the timeout).
		col, _ := s.Q.GetCollectionByID(wctx, row.CollectionID)
		timeout := s.processingTimeout(col)
		return s.tx(wctx, func(q *db.Queries) error {
			now := s.now()
			if err := q.SetTargetStatus(wctx, db.SetTargetStatusParams{Status: "publishing", ExternalID: "", ExternalUrl: "", Error: "",
				Attempts: target.Attempts + 1, PublishedAt: nullStr(""), UpdatedAt: now, PostID: postID, ChannelID: channelID}); err != nil {
				return err
			}
			data := s.postEventData(wctx, q, row.WorkspaceID, postID, map[string]any{
				"channel": toChannelOut(ch), "target": map[string]any{"channel_id": channelID, "content": text, "settings": settings,
					"thread": s.threadOut(wctx, q, row.WorkspaceID, row.Thread)},
				"timeout_seconds": int64(timeout / time.Second)})
			if _, err := s.emitRef(wctx, q, sys, "post.publish_requested", Ref{Collection: row.CollectionID, Post: postID, Channel: channelID}, data); err != nil {
				return err
			}
			if timeout > 0 {
				return s.enqueue(wctx, q, row.WorkspaceID, "target_timeout", "target_timeout:"+postID+":"+channelID,
					map[string]any{"post_id": postID, "channel_id": channelID}, s.Now().Add(timeout), 1)
			}
			return nil
		})
	case err != nil:
		_ = s.W.SetTargetStatus(wctx, db.SetTargetStatusParams{Status: "publishing", ExternalID: "", ExternalUrl: "",
			Error: truncateRunes(err.Error(), 500), Attempts: target.Attempts + 1, PublishedAt: nullStr(""), UpdatedAt: s.now(), PostID: postID, ChannelID: channelID})
		return err // the job retries with backoff; the last failure settles the target (jobFailed)
	}
	return s.finishTarget(wctx, postID, channelID, "published", result, "")
}

// targetTimedOut is the target_timeout job: a custom target nobody reported on fails.
func (s *Service) targetTimedOut(ctx context.Context, postID, channelID string) error {
	t, err := s.Q.GetTarget(ctx, db.GetTargetParams{PostID: postID, ChannelID: channelID})
	if isNoRows(err) || (err == nil && t.Status != "publishing") {
		return nil
	}
	if err != nil {
		return err
	}
	return s.finishTarget(ctx, postID, channelID, "failed", providers.Result{}, "No result reported in time")
}

// finishTarget records a target's outcome and settles the post when every target is done.
func (s *Service) finishTarget(ctx context.Context, postID, channelID, status string, result providers.Result, errMsg string) error {
	row, err := s.Q.GetPostByID(ctx, postID)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	sys := Principal{WorkspaceID: row.WorkspaceID, Kind: PrincipalSystem, ID: "publisher", Name: "Publisher"}
	return s.tx(ctx, func(q *db.Queries) error {
		t, err := q.GetTarget(ctx, db.GetTargetParams{PostID: postID, ChannelID: channelID})
		if isNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		now := s.now()
		published := nullStr("")
		if status == "published" {
			published = nullStr(now)
		}
		if err := q.SetTargetStatus(ctx, db.SetTargetStatusParams{Status: status, ExternalID: result.ExternalID, ExternalUrl: result.URL,
			Error: truncateRunes(errMsg, 500), Attempts: max(t.Attempts, 1), PublishedAt: published, UpdatedAt: now, PostID: postID, ChannelID: channelID}); err != nil {
			return err
		}
		if _, err := q.CancelJob(ctx, db.CancelJobParams{Now: nullStr(now), Key: "target_timeout:" + postID + ":" + channelID}); err != nil {
			return err
		}
		if status == "published" {
			if err := s.startThread(ctx, q, row, channelID, result, now); err != nil {
				return err
			}
		}
		typ := "post.target_published"
		if status == "failed" {
			typ = "post.target_failed"
		}
		data := s.postEventData(ctx, q, row.WorkspaceID, postID, map[string]any{"channel_id": channelID, "url": result.URL, "external_id": result.ExternalID, "error": errMsg})
		if _, err := s.emitRef(ctx, q, sys, typ, Ref{Collection: row.CollectionID, Post: postID, Channel: channelID}, data); err != nil {
			return err
		}
		return s.settlePost(ctx, q, sys, row.WorkspaceID, postID)
	})
}

// settlePost: with no target left pending or publishing, the post is published (all did) or failed (one did not).
func (s *Service) settlePost(ctx context.Context, q *db.Queries, p Principal, workspaceID, postID string) error {
	counts, err := q.CountTargetsByStatus(ctx, postID)
	if err != nil {
		return err
	}
	open, failed := int64(0), int64(0)
	for _, c := range counts {
		switch c.Status {
		case "pending", "publishing":
			open += c.N
		case "failed":
			failed += c.N
		}
	}
	if open > 0 {
		return nil
	}
	row, err := q.GetPostByID(ctx, postID)
	if err != nil {
		return err
	}
	status, typ := "published", "post.published"
	if failed > 0 {
		status, typ = "failed", "post.failed"
	}
	now := s.now()
	published := nullStr("")
	if status == "published" {
		published = nullStr(now)
	}
	if err := q.SetPostStatus(ctx, db.SetPostStatusParams{Status: status, PublishedAt: published, UpdatedAt: now, ID: postID}); err != nil {
		return err
	}
	if status == "published" && row.RepeatEvery != "" {
		// The next occurrence is made by a job (outside this transaction's provider calls, retried like any job).
		if err := s.enqueue(ctx, q, workspaceID, "repeat_post", "repeat_post:"+postID, map[string]any{"post_id": postID}, s.Now(), 0); err != nil {
			return err
		}
	}
	_, err = s.emit(ctx, q, p, typ, row.CollectionID, postID, s.postEventData(ctx, q, workspaceID, postID, nil))
	return err
}

// ResultInput is what your automation reports for a custom target.
type ResultInput struct {
	Status     string `json:"status"` // published or failed
	URL        string `json:"url"`
	ExternalID string `json:"external_id"`
	Error      string `json:"error"`
}

// ReportTargetResult closes a custom target that is waiting for your automation.
func (s *Service) ReportTargetResult(ctx context.Context, p Principal, postID, channelID string, in ResultInput) (Post, error) {
	if err := p.require(ScopePostsPublish); err != nil {
		return Post{}, err
	}
	if in.Status != "published" && in.Status != "failed" {
		return Post{}, invalid("status", "must be published or failed")
	}
	if _, _, err := s.loadPost(ctx, s.Q, p.WorkspaceID, postID); err != nil {
		return Post{}, err
	}
	t, err := s.Q.GetTarget(ctx, db.GetTargetParams{PostID: postID, ChannelID: channelID})
	if isNoRows(err) {
		return Post{}, notFound("target")
	}
	if err != nil {
		return Post{}, err
	}
	ch, err := s.Q.GetChannelByID(ctx, channelID)
	if err == nil && ch.Provider != "custom" {
		return Post{}, conflict("only targets on your own automation take reported results")
	}
	if t.Status != "publishing" {
		return Post{}, conflict("this target is not waiting for a result (it is " + t.Status + ")")
	}
	if in.Status == "published" && in.URL != "" {
		if err := validateWebhookURL(in.URL); err != nil && !strings.Contains(err.Error(), "private") && !strings.Contains(err.Error(), "loopback") {
			return Post{}, invalid("url", "must be an absolute http(s) URL")
		}
	}
	if err := s.finishTarget(ctx, postID, channelID, in.Status, providers.Result{ExternalID: in.ExternalID, URL: in.URL}, in.Error); err != nil {
		return Post{}, err
	}
	return s.GetPost(ctx, p, postID)
}

// RetryTarget tries a failed target once more, now.
func (s *Service) RetryTarget(ctx context.Context, p Principal, postID, channelID string) (Post, error) {
	if err := p.require(ScopePostsPublish); err != nil {
		return Post{}, err
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		row, err := q.GetPost(ctx, db.GetPostParams{WorkspaceID: p.WorkspaceID, ID: postID})
		if isNoRows(err) {
			return notFound("post")
		}
		if err != nil {
			return err
		}
		t, err := q.GetTarget(ctx, db.GetTargetParams{PostID: postID, ChannelID: channelID})
		if isNoRows(err) {
			return notFound("target")
		}
		if err != nil {
			return err
		}
		if t.Status != "failed" {
			return conflict("only a failed target can be retried")
		}
		now := s.now()
		if err := q.SetTargetStatus(ctx, db.SetTargetStatusParams{Status: "publishing", ExternalID: "", ExternalUrl: "", Error: "",
			Attempts: t.Attempts, PublishedAt: nullStr(""), UpdatedAt: now, PostID: postID, ChannelID: channelID}); err != nil {
			return err
		}
		if err := q.SetPostStatus(ctx, db.SetPostStatusParams{Status: "publishing", PublishedAt: nullStr(""), UpdatedAt: now, ID: postID}); err != nil {
			return err
		}
		return s.enqueue(ctx, q, row.WorkspaceID, "publish_target", "publish_target:"+postID+":"+channelID,
			map[string]any{"post_id": postID, "channel_id": channelID}, s.Now(), 3)
	})
	if err != nil {
		return Post{}, err
	}
	return s.GetPost(ctx, p, postID)
}

// ---- preview ----

type TargetPreview struct {
	ChannelID string   `json:"channel_id"`
	Text      string   `json:"text"`
	Warnings  []string `json:"warnings"`
	OK        bool     `json:"ok"`
}

type PostPreview struct {
	Targets []TargetPreview `json:"targets"`
}

// PreviewPost shows the text every target will carry and what its provider objects to.
func (s *Service) PreviewPost(ctx context.Context, p Principal, id string) (PostPreview, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return PostPreview{}, err
	}
	post, row, err := s.loadPost(ctx, s.Q, p.WorkspaceID, id)
	if err != nil {
		return PostPreview{}, err
	}
	var media []string
	_ = json.Unmarshal([]byte(row.Media), &media)
	files, err := s.mediaFiles(ctx, s.Q, p.WorkspaceID, media)
	if err != nil {
		return PostPreview{}, err
	}
	out := PostPreview{Targets: []TargetPreview{}}
	for _, t := range post.Targets {
		text := post.Content
		if t.Content != nil {
			text = *t.Content
		}
		tp := TargetPreview{ChannelID: t.ChannelID, Text: text, Warnings: []string{}, OK: true}
		if e, ok := providers.Get(t.Provider); ok {
			if err := e.Validate(providers.PostContent{Text: text, Media: files}, providers.Settings(t.Settings)); err != nil {
				tp.Warnings, tp.OK = append(tp.Warnings, err.Error()), false
			}
		}
		out.Targets = append(out.Targets, tp)
	}
	return out, nil
}

// ---- requested work ----

// WorkItem is something an agent is asked to do: publish a custom target, or write posts for a schedule.
type WorkItem struct {
	Kind        string     `json:"kind"` // target | posts
	Post        *Post      `json:"post,omitempty"`
	Channel     *Channel   `json:"channel,omitempty"`
	Schedule    *Schedule  `json:"schedule,omitempty"`
	Collection  Collection `json:"collection"`
	RequestedAt string     `json:"requested_at"`
}

func (s *Service) ListRequestedWork(ctx context.Context, p Principal) ([]WorkItem, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return nil, err
	}
	out := []WorkItem{}
	cols := map[string]Collection{}
	collection := func(id string) Collection {
		if c, ok := cols[id]; ok {
			return c
		}
		c, err := s.getCollection(ctx, p, id)
		if err != nil {
			c = Collection{ID: id}
		}
		cols[id] = c
		return c
	}
	rows, err := s.Q.WaitingCustomTargets(ctx, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		post, _, err := s.loadPost(ctx, s.Q, p.WorkspaceID, r.PostID)
		if err != nil {
			continue
		}
		ch, err := s.Q.GetChannelByID(ctx, r.ChannelID)
		if err != nil {
			continue
		}
		chOut := toChannelOut(ch)
		out = append(out, WorkItem{Kind: "target", Post: &post, Channel: &chOut, Collection: collection(r.CollectionID), RequestedAt: r.UpdatedAt})
	}
	schedules, err := s.Q.ListSchedules(ctx, db.ListSchedulesParams{WorkspaceID: p.WorkspaceID})
	if err != nil {
		return nil, err
	}
	for _, sc := range schedules {
		if sc.Source != "agent" || sc.Enabled != 1 || !strings.HasPrefix(sc.LastResult, "asked") {
			continue
		}
		waiting, err := s.Q.WaitingPosts(ctx, db.WaitingPostsParams{WorkspaceID: p.WorkspaceID, CollectionID: sc.CollectionID, Limit: 1})
		if err != nil || len(waiting) > 0 {
			continue
		}
		so := s.toSchedule(ctx, sc)
		out = append(out, WorkItem{Kind: "posts", Schedule: &so, Collection: collection(sc.CollectionID), RequestedAt: sc.LastRunAt.String})
	}
	return out, nil
}
