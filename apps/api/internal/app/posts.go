package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Post statuses: draft (no time, or not yet scheduled) → scheduled → publishing → published | failed.
var PostStatuses = []string{"draft", "scheduled", "publishing", "published", "failed"}

const (
	maxPostContent = 50_000
	maxPostTitle   = 300 // Reddit's limit, the longest; a platform with a shorter one refuses a longer title per channel
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
	Title          string          `json:"title"`
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
	// CreatedBy names who created the post: a person, or an API key (an agent, an automation); empty when unknown.
	CreatedBy string `json:"created_by"`
	// OpenComments counts reviewers' comment threads not resolved yet.
	OpenComments int64 `json:"open_comments"`
}

type PostTargetInput struct {
	ChannelID string            `json:"channel_id"`
	Content   *string           `json:"content"`
	Settings  map[string]string `json:"settings"`
}

type PostInput struct {
	Collection  string            `json:"collection"`
	Title       string            `json:"title"` // required; platforms with a title publish it, the rest show it in lists
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
	Title           *string            `json:"title"`
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
	HidePast                                bool // leave out posts that went out (published); failed ones stay
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

// toPost builds a post for the API; media holds its files and its thread's (postsMedia), an id missing there is left out.
func (s *Service) toPost(p db.Post, targets []db.ListTargetsRow, collectionName string, media map[string]db.Medium) Post {
	out := Post{ID: p.ID, CollectionID: p.CollectionID, CollectionName: collectionName, Title: p.Title, Content: p.Content, Media: []string{}, MediaItems: []Media{},
		Status: p.Status, PublishAt: ptr(p.PublishAt), PublishedAt: ptr(p.PublishedAt), Tags: []string{},
		Metadata: json.RawMessage(p.Metadata), Source: p.Source, ExternalRef: p.ExternalRef, RepeatEvery: p.RepeatEvery, Version: p.Version,
		Thread: s.threadPartsOut(parseThread(p.Thread), media), Targets: []PostTarget{}, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
	_ = json.Unmarshal([]byte(p.Media), &out.Media)
	_ = json.Unmarshal([]byte(p.Tags), &out.Tags)
	for _, id := range out.Media {
		if m, ok := media[id]; ok {
			out.MediaItems = append(out.MediaItems, s.toMedia(m))
		}
	}
	for _, t := range targets {
		pt := targetOut(t)
		pt.Replies = repliesFor(len(targetThread(p.Thread, t.Settings)), t.Replies)
		out.Targets = append(out.Targets, pt)
	}
	return out
}

// uniqueIDs drops repeated ids (sorted): PostsByIDs and MediaByIDs return a row per id given.
func uniqueIDs(ids []string) []string {
	return slices.Compact(slices.Sorted(slices.Values(ids)))
}

// mediaByID reads these files in one statement (best effort, like a missing file: a failed read leaves them out).
func (s *Service) mediaByID(ctx context.Context, q *db.Queries, workspaceID string, ids []string) map[string]db.Medium {
	out := map[string]db.Medium{}
	if len(ids) == 0 {
		return out
	}
	rows, err := q.MediaByIDs(ctx, db.MediaByIDsParams{WorkspaceID: workspaceID, Ids: mustJSON(uniqueIDs(ids))})
	if err != nil {
		return out
	}
	for _, m := range rows {
		out[m.ID] = m
	}
	return out
}

// postsMedia reads the files of these posts and of their threads in one statement.
func (s *Service) postsMedia(ctx context.Context, q *db.Queries, workspaceID string, rows ...db.Post) map[string]db.Medium {
	var ids []string
	for _, r := range rows {
		var media []string
		_ = json.Unmarshal([]byte(r.Media), &media)
		ids = append(ids, media...)
		for _, part := range parseThread(r.Thread) {
			ids = append(ids, part.Media...)
		}
	}
	return s.mediaByID(ctx, q, workspaceID, ids)
}

// postsOut builds a page of posts with their targets, collection names, media and extras in a fixed number of reads.
func (s *Service) postsOut(ctx context.Context, q *db.Queries, workspaceID string, rows []db.Post) ([]Post, error) {
	out := []Post{}
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	targets, err := q.ListTargetsForPosts(ctx, mustJSON(ids))
	if err != nil {
		return nil, err
	}
	byPost := map[string][]db.ListTargetsRow{}
	for _, t := range targets {
		byPost[t.PostID] = append(byPost[t.PostID], db.ListTargetsRow(t))
	}
	names := map[string]string{}
	if cols, err := q.ListCollections(ctx, db.ListCollectionsParams{WorkspaceID: workspaceID, IncludeArchived: 1}); err == nil {
		for _, c := range cols {
			names[c.ID] = c.Name
		}
	}
	media := s.postsMedia(ctx, q, workspaceID, rows...)
	for _, r := range rows {
		out = append(out, s.toPost(r, byPost[r.ID], names[r.CollectionID], media))
	}
	s.addPostExtras(ctx, q, workspaceID, out)
	return out, nil
}

// loadPosts is loadPost for many ids at once, by id (ids not found are left out).
func (s *Service) loadPosts(ctx context.Context, q *db.Queries, workspaceID string, ids []string) (map[string]Post, error) {
	out := map[string]Post{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.PostsByIDs(ctx, db.PostsByIDsParams{WorkspaceID: workspaceID, Ids: mustJSON(uniqueIDs(ids))})
	if err != nil {
		return nil, err
	}
	posts, err := s.postsOut(ctx, q, workspaceID, rows)
	if err != nil {
		return nil, err
	}
	for _, p := range posts {
		out[p.ID] = p
	}
	return out, nil
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
	out := []Post{s.toPost(row, targets, name, s.postsMedia(ctx, q, workspaceID, row))}
	s.addPostExtras(ctx, q, workspaceID, out)
	return out[0], row, nil
}

// addPostExtras fills who created each post and its open comment threads (best effort: a failed read leaves them
// empty rather than failing the list).
func (s *Service) addPostExtras(ctx context.Context, q *db.Queries, workspaceID string, posts []Post) {
	ids := make([]string, len(posts))
	for i := range posts {
		ids[i] = posts[i].ID
	}
	extras, err := q.PostExtras(ctx, workspaceID, ids)
	if err != nil {
		return
	}
	for i := range posts {
		posts[i].CreatedBy = extras[posts[i].ID].CreatedBy
		posts[i].OpenComments = extras[posts[i].ID].OpenComments
	}
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

// checkMedia validates a post's (or a thread part's) files: in the workspace, not listed twice, and not in
// Deleted unless kept lists it (a file the post already carried stays while it is restorable).
func (s *Service) checkMedia(ctx context.Context, q *db.Queries, workspaceID string, ids, kept []string) error {
	if len(ids) > maxPostMedia {
		return invalid("media", fmt.Sprintf("at most %d files", maxPostMedia))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return invalid("media", "the same file is listed twice")
		}
		seen[id] = true
		m, err := q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: workspaceID, ID: id})
		if err != nil {
			if isNoRows(err) {
				return invalid("media", "unknown media id "+id)
			}
			return err
		}
		if m.DeletedAt.Valid && !slices.Contains(kept, id) {
			return invalid("media", deletedMediaMsg(m))
		}
	}
	return nil
}

func deletedMediaMsg(m db.Medium) string {
	return m.OriginalName + " (" + m.ID + ") is in Deleted: restore it to use it again"
}

// checkNoDeletedMedia refuses to schedule or publish a post that carries a file in Deleted (files or thread):
// the sweep would remove it before the post goes out. Every path that queues a publish of an existing post
// calls it (schedule, publish now, retry a target, a schedule run).
func checkNoDeletedMedia(ctx context.Context, q *db.Queries, workspaceID string, row db.Post) error {
	var ids []string
	_ = json.Unmarshal([]byte(row.Media), &ids)
	for _, part := range parseThread(row.Thread) {
		ids = append(ids, part.Media...)
	}
	for _, id := range ids {
		if m, err := q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: workspaceID, ID: id}); err == nil && m.DeletedAt.Valid {
			return invalid("media", deletedMediaMsg(m))
		} else if err != nil && !isNoRows(err) {
			return err
		}
	}
	return nil
}

// resolveTargets checks the channels exist, are usable and accept the post (provider limits and settings).
// resolveTargets checks the targets of a post. `kept` are channels the post already had: they stay allowed when
// their platform was turned off since (PROVIDER_<ID>_ENABLED=false), so what was scheduled still goes out.
func (s *Service) resolveTargets(ctx context.Context, q *db.Queries, workspaceID, title string, tags []string, content string, media []string,
	in []PostTargetInput, kept map[string]bool) (map[string]db.Channel, error) {
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
		if !kept[t.ChannelID] && !s.providerEnabled(ch.Provider) {
			return nil, &Error{Code: "provider_disabled", Status: 409, Message: e.Name() + " is turned off on this server"}
		}
		text := content
		if t.Content != nil {
			text = *t.Content
		}
		text, _ = providers.ApplyLinks(channelLinks(ch), text)
		settings := providers.WithPost(e.SettingsFields(), e.Limits(), title, tags, t.Settings)
		if err := e.Validate(providers.PostContent{Text: text, Media: files}, settings); err != nil {
			return nil, targetError(t.ChannelID, err)
		}
		if err := s.checkChannelSettings(ctx, q, workspaceID, ch, e.SettingsFields(), t.Settings); err != nil {
			return nil, err
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
		storage := m.StorageName
		f := providers.MediaFile{ID: m.ID, Name: m.OriginalName, MimeType: m.MimeType, Size: m.SizeBytes,
			URL:  strings.TrimRight(s.Cfg.PublicURL, "/") + "/v1/media/" + m.ID,
			Open: func(ctx context.Context) (io.ReadCloser, error) { return s.openMediaFile(ctx, workspaceID, storage) }}
		d, disk := s.Cfg.Blob.(*blob.Disk)
		if disk {
			f.Path = filepath.Join(d.Root, filepath.FromSlash(mediaKey(workspaceID, storage)))
		}
		if link := s.MediaLink(workspaceID, m.ID); link != "" {
			f.SignedURL = link // PUBLIC_MEDIA_URL: platforms download it from this server
		} else if !disk {
			if u, err := s.Cfg.Blob.SignedURL(ctx, mediaKey(workspaceID, storage), signedMediaTTL); err == nil {
				f.SignedURL = u // platforms that pull media fetch it here, not from the API
			}
		}
		out = append(out, f)
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
	title, err := trimmed(in.Title, "title", maxPostTitle, true)
	if err != nil {
		return Post{}, err
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
		if err := s.checkTrialReview(ctx, p.WorkspaceID); err != nil {
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
	row := db.Post{ID: newID("post"), WorkspaceID: p.WorkspaceID, CollectionID: col.ID, Title: title, Content: in.Content, Media: mustJSON(in.Media),
		Status: in.Status, PublishAt: nullStr(publishAt), Tags: mustJSON(tags), Metadata: metadata, Source: source,
		ExternalRef: externalRef, RepeatEvery: repeatEvery, RepeatOf: in.repeatOf, Version: 1, CreatedAt: now, UpdatedAt: now}
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := s.checkMedia(ctx, q, p.WorkspaceID, in.Media, nil); err != nil {
			return err
		}
		chans, err := s.resolveTargets(ctx, q, p.WorkspaceID, title, tags, in.Content, in.Media, in.Targets, nil)
		if err != nil {
			return err
		}
		thread, err := s.checkThread(ctx, q, p.WorkspaceID, in.Thread, nil)
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
			Title: row.Title, Content: row.Content, Media: row.Media, Status: row.Status, PublishAt: row.PublishAt, Tags: row.Tags, Metadata: row.Metadata,
			Source: row.Source, ExternalRef: row.ExternalRef, RepeatEvery: row.RepeatEvery, RepeatOf: row.RepeatOf,
			CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		if p.UserID != "" {
			// The person behind it (an API key counts as its creator): their "my posts" notifications.
			if err := q.SetPostAuthor(ctx, db.SetPostAuthorParams{CreatedByUser: p.UserID, ID: row.ID, WorkspaceID: row.WorkspaceID}); err != nil {
				return err
			}
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
			if err := s.checkAllowancesForPost(ctx, q, row.ID, s.Now()); err != nil {
				return err
			}
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
	// X and LinkedIn posts are metered: refuse now when this period's allowances can't cover the post
	// (publishing checks again).
	if err := s.checkAllowancesForPost(ctx, q, postID, at); err != nil {
		return err
	}
	// Retried like any job (a database that is briefly busy at the scheduled minute): the run checks the
	// post's state again inside its write, so a retry never publishes twice; the last failure fails the post.
	if err := s.enqueue(ctx, q, workspaceID, "publish_post", "publish_post:"+postID, map[string]any{"post_id": postID}, at, 0); err != nil {
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
	// One status or several, comma-separated (the dashboard's Upcoming: draft,scheduled,publishing).
	for _, st := range strings.Split(f.Status, ",") {
		if f.Status != "" && !slices.Contains(PostStatuses, st) {
			return PostPage{}, invalid("status", "must be one of "+strings.Join(PostStatuses, ", ")+", or several comma-separated")
		}
	}
	if f.Sort == "" {
		f.Sort = "publish_asc"
	}
	if !db.ValidPostSort(f.Sort) {
		return PostPage{}, invalid("sort", "unknown sort")
	}
	search := db.PostSearch{WorkspaceID: p.WorkspaceID, CollectionID: f.CollectionID, Status: f.Status, ChannelID: f.ChannelID,
		Tag: f.Tag, Query: f.Q, Sort: f.Sort, Cursor: f.Cursor, Limit: clampLimit(f.Limit, 50, 200)}
	if f.HidePast {
		search.HidePast = s.now()
	}
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
	posts, err := s.postsOut(ctx, s.Q, p.WorkspaceID, rows)
	if err != nil {
		return PostPage{}, err
	}
	return PostPage{Data: posts, NextCursor: next}, nil
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
			return &Error{Code: "post_changed", Status: 409, Message: fmt.Sprintf("post was modified (version %d, expected %d)", row.Version, *in.ExpectedVersion)}
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
		if in.Title != nil {
			if row.Title, err = trimmed(*in.Title, "title", maxPostTitle, true); err != nil {
				return err
			}
		}
		if in.Content != nil {
			if len([]rune(*in.Content)) > maxPostContent {
				return invalid("content", fmt.Sprintf("at most %d characters", maxPostContent))
			}
			row.Content = *in.Content
		}
		media := []string{}
		_ = json.Unmarshal([]byte(row.Media), &media)
		had := slices.Clone(media) // files the post already carries (one in Deleted may stay)
		for _, part := range parseThread(row.Thread) {
			had = append(had, part.Media...)
		}
		if in.Media != nil {
			media = *in.Media
			if media == nil {
				media = []string{}
			}
			if err := s.checkMedia(ctx, q, p.WorkspaceID, media, had); err != nil {
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
		// A target on a removed channel is history (only published ones stay): kept as it is, never re-validated,
		// and an edit that leaves it out (the dashboard no longer lists that channel) doesn't drop it.
		targets := make([]PostTargetInput, 0, len(current))
		var history []PostTargetInput
		for _, t := range current {
			settings := map[string]string{}
			_ = json.Unmarshal([]byte(t.Settings), &settings)
			ti := PostTargetInput{ChannelID: t.ChannelID, Content: ptr(t.Content), Settings: settings}
			if t.ChannelDeletedAt.Valid {
				history = append(history, ti)
				continue
			}
			targets = append(targets, ti)
		}
		if in.Targets != nil {
			for _, t := range current {
				if t.Status == "published" && !t.ChannelDeletedAt.Valid && !slices.ContainsFunc(*in.Targets, func(n PostTargetInput) bool { return n.ChannelID == t.ChannelID }) {
					return conflict("a channel this post is already published on cannot be removed")
				}
			}
			targets = slices.DeleteFunc(slices.Clone(*in.Targets), func(n PostTargetInput) bool {
				return slices.ContainsFunc(history, func(h PostTargetInput) bool { return h.ChannelID == n.ChannelID })
			})
		}
		kept := map[string]bool{}
		for _, t := range current {
			kept[t.ChannelID] = true
		}
		var tags []string
		_ = json.Unmarshal([]byte(row.Tags), &tags)
		chans, err := s.resolveTargets(ctx, q, p.WorkspaceID, row.Title, tags, row.Content, media, targets, kept)
		if err != nil {
			return err
		}
		thread := parseThread(row.Thread)
		if in.Thread != nil {
			was := mustJSON(thread)
			if thread, err = s.checkThread(ctx, q, p.WorkspaceID, *in.Thread, had); err != nil {
				return err
			}
			// Replies follow the post's thread part by part: once the post is out on a channel, a changed thread
			// would rewrite (or drop) the comments still going out under it there.
			if mustJSON(thread) != was && slices.ContainsFunc(current, func(t db.ListTargetsRow) bool { return t.Status == "published" }) {
				return conflict("this post is already published on a channel, so its comments can't be changed")
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
		n, err := q.UpdatePost(ctx, db.UpdatePostParams{CollectionID: row.CollectionID, Title: row.Title, Content: row.Content, Media: row.Media,
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
			if err := s.writeTargets(ctx, q, id, append(slices.Clone(targets), history...), row.UpdatedAt); err != nil {
				return err
			}
		}
		if _, err := s.emit(ctx, q, p, "post.updated", row.CollectionID, id, s.postEventData(ctx, q, p.WorkspaceID, id, nil)); err != nil {
			return err
		}
		switch {
		case before.Status == "scheduled" && row.Status == "draft":
			if err := s.cancelJobs(ctx, q, "publish_post:"+id); err != nil {
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
	if err := s.checkTrialReview(ctx, p.WorkspaceID); err != nil {
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
		if err := checkNoDeletedMedia(ctx, q, p.WorkspaceID, row); err != nil {
			return err
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
		if n, err := q.UpdatePost(ctx, db.UpdatePostParams{CollectionID: row.CollectionID, Title: row.Title, Content: row.Content, Media: row.Media,
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
	if err := s.checkTrialReview(ctx, p.WorkspaceID); err != nil {
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
		if err := checkNoDeletedMedia(ctx, q, p.WorkspaceID, row); err != nil {
			return err
		}
		if err := s.cancelJobs(ctx, q, "publish_post:"+id); err != nil {
			return err
		}
		if err := s.checkAllowancesForPost(ctx, q, id, s.Now()); err != nil {
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
		if err := s.cancelJobs(ctx, q, "publish_post:"+id); err != nil {
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

// titleOr is the post's title, or for a post from before titles were required its first line (at most 100
// characters, YouTube's limit), else "Untitled".
func titleOr(title, content string) string {
	if t := strings.TrimSpace(title); t != "" {
		return t
	}
	line, _, _ := strings.Cut(strings.TrimSpace(content), "\n")
	if line = strings.TrimSpace(line); line != "" {
		return truncateRunes(line, 100)
	}
	return "Untitled"
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
	in := PostInput{Collection: row.CollectionID, Title: titleOr(src.Title, src.Content), Content: src.Content, Media: src.Media, Tags: src.Tags,
		Metadata: src.Metadata, ExternalRef: src.ExternalRef, RepeatEvery: src.RepeatEvery, Thread: parseThread(row.Thread), Status: "draft"}
	for _, t := range src.Targets {
		in.Targets = append(in.Targets, PostTargetInput{ChannelID: t.ChannelID, Content: t.Content, Settings: t.Settings})
	}
	return s.CreatePost(ctx, p, in)
}

// ErrPostWentOut: a post that went out (published, being published, or published on some of its channels) is
// history and stays; purge (admin), retention and deleting the collection or the workspace still remove it.
var ErrPostWentOut = conflict("this post went out and is kept as history; it cannot be deleted")

// deletable refuses a post that went out, in full or on any of its channels, or is going out right now.
func deletable(status string, targets []db.ListTargetsRow) error {
	if status == "published" || status == "publishing" ||
		slices.ContainsFunc(targets, func(t db.ListTargetsRow) bool { return t.Status == "published" }) {
		return ErrPostWentOut
	}
	return nil
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
		targets, err := q.ListTargets(ctx, id)
		if err != nil {
			return err
		}
		if err := deletable(row.Status, targets); err != nil {
			return err
		}
		data := s.postEventData(ctx, q, p.WorkspaceID, id, nil)
		for _, t := range targets {
			for _, k := range []string{"publish_target:", "target_timeout:"} {
				if err := s.cancelJobs(ctx, q, k+id+":"+t.ChannelID); err != nil {
					return err
				}
			}
		}
		if err := s.cancelJobs(ctx, q, "publish_post:"+id); err != nil {
			return err
		}
		if _, err := q.DeletePost(ctx, db.DeletePostParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "post.deleted", row.CollectionID, id, data)
		return err
	})
}

// DeletedPosts is what DeletePosts did with each id.
type DeletedPosts struct {
	Deleted []string `json:"deleted"`
	Missing []string `json:"missing"` // unknown ids, or ids of another workspace
	Kept    []string `json:"kept"`    // posts that went out (or are going out): history, not deleted
}

// DeletePosts deletes several posts (posts:delete, like one); unknown ids, or ids of another workspace, are
// reported as missing, and posts that went out as kept, not fatal.
func (s *Service) DeletePosts(ctx context.Context, p Principal, ids []string) (DeletedPosts, error) {
	if err := p.require(ScopePostsDelete); err != nil {
		return DeletedPosts{}, err
	}
	if len(ids) == 0 || len(ids) > 200 {
		return DeletedPosts{}, invalid("ids", "1 to 200 post ids")
	}
	ids = uniqueIDs(ids) // an id listed twice is deleted once, not reported as missing the second time
	out := DeletedPosts{Deleted: []string{}, Missing: []string{}, Kept: []string{}}
	for _, id := range ids {
		switch err := s.DeletePost(ctx, p, id); {
		case err == nil:
			out.Deleted = append(out.Deleted, id)
		case isNotFound(err):
			out.Missing = append(out.Missing, id)
		case errors.Is(err, ErrPostWentOut):
			out.Kept = append(out.Kept, id)
		default:
			return out, err
		}
	}
	return out, nil
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
	if blocked := s.publishBlocked(ctx, row.WorkspaceID); blocked != nil {
		return s.finishTarget(ctx, postID, channelID, "failed", providers.Result{}, blocked.(*Error).Message)
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
	// The channel's links mode applies to what it receives; the post keeps its text as written.
	links := channelLinks(ch)
	text, _ = providers.ApplyLinks(links, text)
	// Your own automation gets the tags as they are, in "tags"; a platform with tags of its own gets them there.
	if ch.Provider != "custom" && !providers.NativeTags(e.SettingsFields()) {
		text = providers.AppendHashtags(text, tags)
	}
	for i := range thread {
		thread[i].Text, _ = providers.ApplyLinks(links, thread[i].Text)
	}
	content := providers.PostContent{PostID: postID, ChannelID: channelID, Text: text, Media: files, Tags: tags, Thread: thread}
	settings := providers.Settings{}
	_ = json.Unmarshal([]byte(target.Settings), &settings)
	settings = providers.WithPost(e.SettingsFields(), e.Limits(), row.Title, tags, settings)
	if err := e.Validate(content, settings); err != nil {
		return s.finishTarget(ctx, postID, channelID, "failed", providers.Result{}, err.Error())
	}
	if ch.Provider == ProviderX {
		if err := s.checkX(ctx, s.Q, row.WorkspaceID, xUnits(text)); err != nil {
			return s.finishTarget(ctx, postID, channelID, "failed", providers.Result{}, errorMessage(err))
		}
	}
	if isLinkedIn(ch.Provider) {
		var videos int64
		if slices.ContainsFunc(files, func(f providers.MediaFile) bool { return strings.HasPrefix(f.MimeType, "video/") }) {
			videos = 1
		}
		if err := s.checkLinkedIn(ctx, s.Q, row.WorkspaceID, 1, videos); err != nil {
			return s.finishTarget(ctx, postID, channelID, "failed", providers.Result{}, errorMessage(err))
		}
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
	pctx := s.providerCtx(ctx)
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

// errTargetSettled: the target was no longer publishing when its outcome came in (another report, the
// timeout or a run got there first).
var errTargetSettled = errors.New("target already settled")

// finishTarget records a target's outcome and settles the post when every target is done. An outcome for a
// target that is not publishing any more is dropped: it was settled meanwhile, once.
func (s *Service) finishTarget(ctx context.Context, postID, channelID, status string, result providers.Result, errMsg string) error {
	if err := s.settleTarget(ctx, postID, channelID, status, result, errMsg); !errors.Is(err, errTargetSettled) {
		return err
	}
	return nil
}

// settleTarget is finishTarget that says errTargetSettled when the target was not publishing (checked inside
// the transaction, so two outcomes at once settle it once).
func (s *Service) settleTarget(ctx context.Context, postID, channelID, status string, result providers.Result, errMsg string) error {
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
		if t.Status != "publishing" {
			return errTargetSettled
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
		if err := s.cancelJobs(ctx, q, "target_timeout:"+postID+":"+channelID); err != nil {
			return err
		}
		if status == "published" {
			if err := s.meterTarget(ctx, q, row, t, channelID); err != nil {
				return err
			}
			if err := s.startThread(ctx, q, row, channelID, result, now); err != nil {
				return err
			}
			if err := s.enqueueReposts(ctx, q, row.WorkspaceID, postID, channelID, t.Settings); err != nil {
				return err
			}
		}
		typ := "post.target_published"
		if status == "failed" {
			typ = "post.target_failed"
		}
		extra := map[string]any{"channel_id": channelID, "url": result.URL, "external_id": result.ExternalID, "error": errMsg}
		// The channel as it is now (name, platform, picture), so a notification can say where it went.
		if ch, err := q.GetChannel(ctx, db.GetChannelParams{WorkspaceID: row.WorkspaceID, ID: channelID}); err == nil {
			extra["channel"] = targetChannel(ch)
		} else if !isNoRows(err) {
			return err
		}
		data := s.postEventData(ctx, q, row.WorkspaceID, postID, extra)
		if _, err := s.emitRef(ctx, q, sys, typ, Ref{Collection: row.CollectionID, Post: postID, Channel: channelID}, data); err != nil {
			return err
		}
		return s.settlePost(ctx, q, sys, row.WorkspaceID, postID)
	})
}

// eventChannel is a channel in a per-target event: the channel plus its platform's display name.
type eventChannel struct {
	Channel
	ProviderName string `json:"provider_name"`
}

func targetChannel(c db.Channel) eventChannel {
	out := eventChannel{Channel: toChannelOut(c), ProviderName: c.Provider}
	if e, ok := providers.Get(c.Provider); ok {
		out.ProviderName = e.Name()
	}
	return out
}

// settlePost: with no target left pending or publishing, the post is published (all did) or failed (one did not).
func (s *Service) settlePost(ctx context.Context, q *db.Queries, p Principal, workspaceID, postID string) error {
	counts, err := q.CountTargetsByStatus(ctx, postID)
	if err != nil {
		return err
	}
	open, failed, total := int64(0), int64(0), int64(0)
	for _, c := range counts {
		total += c.N
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
	if failed > 0 || total == 0 { // no targets left: every channel it was for was removed, so nothing went out
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
	data := s.postEventData(ctx, q, workspaceID, postID, nil)
	if total == 0 {
		data["error"] = "none of its channels exist any more"
	}
	_, err = s.emit(ctx, q, p, typ, row.CollectionID, postID, data)
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
	// Both are stored on the target and copied into events and webhooks.
	if len(in.ExternalID) > 500 {
		return Post{}, invalid("external_id", "must be at most 500 characters")
	}
	if len(in.URL) > 2048 {
		return Post{}, invalid("url", "must be at most 2048 characters")
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
	err = s.settleTarget(ctx, postID, channelID, in.Status, providers.Result{ExternalID: in.ExternalID, URL: in.URL}, in.Error)
	if errors.Is(err, errTargetSettled) {
		return Post{}, conflict("this target is not waiting for a result any more (another result or the timeout settled it)")
	}
	if err != nil {
		return Post{}, err
	}
	return s.GetPost(ctx, p, postID)
}

// RetryTarget tries a failed target once more, now.
func (s *Service) RetryTarget(ctx context.Context, p Principal, postID, channelID string) (Post, error) {
	if err := p.require(ScopePostsPublish); err != nil {
		return Post{}, err
	}
	if err := s.checkTrialReview(ctx, p.WorkspaceID); err != nil {
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
		if err := checkNoDeletedMedia(ctx, q, p.WorkspaceID, row); err != nil {
			return err
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
	ChannelID string `json:"channel_id"`
	// Text is what the channel receives: its own version if it has one, with the channel's links mode applied.
	Text string `json:"text"`
	// Links is the channel's links mode (keep, remove, defang); LinksFound lists the links it rewrote, as
	// written (each also gets a warning that names the channel).
	Links      string   `json:"links"`
	LinksFound []string `json:"links_found"`
	Warnings   []string `json:"warnings"`
	OK         bool     `json:"ok"`
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
		tp := TargetPreview{ChannelID: t.ChannelID, Text: text, Links: providers.LinksKeep, LinksFound: []string{},
			Warnings: []string{}, OK: true}
		if ch, err := s.Q.GetChannel(ctx, db.GetChannelParams{WorkspaceID: p.WorkspaceID, ID: t.ChannelID}); err == nil {
			tp.Links = channelLinks(ch)
			var found []string
			tp.Text, found = providers.ApplyLinks(tp.Links, text)
			if e, ok := providers.Get(ch.Provider); ch.Provider != "custom" && (!ok || !providers.NativeTags(e.SettingsFields())) {
				tp.Text = providers.AppendHashtags(tp.Text, post.Tags)
			}
			text = tp.Text
			if len(found) > 0 {
				tp.LinksFound = found
				tp.Warnings = append(tp.Warnings, linksWarning(tp.Links, ch.Name, found))
			}
		}
		if e, ok := providers.Get(t.Provider); ok {
			settings := providers.WithPost(e.SettingsFields(), e.Limits(), post.Title, post.Tags, t.Settings)
			if err := e.Validate(providers.PostContent{Text: text, Media: files}, settings); err != nil {
				tp.Warnings, tp.OK = append(tp.Warnings, err.Error()), false
			}
		}
		out.Targets = append(out.Targets, tp)
	}
	return out, nil
}

// linksWarning says what a channel's links mode does to the links found in a post.
func linksWarning(mode, channel string, found []string) string {
	what := "Links will be removed from: "
	if mode == providers.LinksDefang {
		what = "Links will be shown as plain text on: "
	}
	return what + channel + " (" + strings.Join(found, ", ") + ")"
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

// ListRequestedWork lists what your automation owes, oldest first: posts waiting for a custom target's result
// (the oldest limit of them: 100 by default, at most 500; it is a queue that drains as results come in) and
// schedules asking for posts.
func (s *Service) ListRequestedWork(ctx context.Context, p Principal, limit int) ([]WorkItem, error) {
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
	rows, err := s.Q.WaitingCustomTargets(ctx, db.WaitingCustomTargetsParams{WorkspaceID: p.WorkspaceID, Lim: int64(clampLimit(limit, 100, 500))})
	if err != nil {
		return nil, err
	}
	postIDs, channelIDs := make([]string, 0, len(rows)), make([]string, 0, len(rows))
	for _, r := range rows {
		postIDs, channelIDs = append(postIDs, r.PostID), append(channelIDs, r.ChannelID)
	}
	posts, err := s.loadPosts(ctx, s.Q, p.WorkspaceID, postIDs)
	if err != nil {
		return nil, err
	}
	channels := map[string]db.Channel{}
	if len(channelIDs) > 0 {
		chs, err := s.Q.ChannelsByIDs(ctx, db.ChannelsByIDsParams{WorkspaceID: p.WorkspaceID, Ids: mustJSON(channelIDs)})
		if err != nil {
			return nil, err
		}
		for _, ch := range chs {
			channels[ch.ID] = ch
		}
	}
	for _, r := range rows {
		post, ok := posts[r.PostID]
		if !ok {
			continue
		}
		ch, ok := channels[r.ChannelID]
		if !ok {
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
