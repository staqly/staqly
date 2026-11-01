package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Sharing a post for feedback: a preview link opens a read-only page of the post as each channel will show it,
// where anyone with the link can leave a comment (no sign-in). Only the token's hash is stored; the link can
// expire and be revoked. Comments reach the team on the post and as a post.feedback_added event.

const (
	previewMaxDays            = 365
	previewCommentsPerHour    = 30 // per link
	previewCommentMaxRunes    = 2000
	previewNameMaxRunes       = 80
	previewQuoteMaxRunes      = 500
	metricsFresh              = 10 * time.Minute
	previewInvalidLinkMessage = "this preview link is not valid or no longer works"
)

type PreviewLink struct {
	ID        string  `json:"id"`
	PostID    string  `json:"post_id"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt *string `json:"expires_at"`
	RevokedAt *string `json:"revoked_at"`
	Active    bool    `json:"active"`
	// URL and Token only in the answer that creates the link: the token is never shown again.
	URL   string `json:"url,omitempty"`
	Token string `json:"token,omitempty"`
}

type PreviewLinkInput struct {
	ExpiresInDays int `json:"expires_in_days"` // 0: never
}

// PostComment is one comment of a feedback thread. A thread starts with a comment (ParentID "") that may mark a
// passage of the post: Part 0 is the post's text and n its n-th comment, ChannelID the channel's version ("" the
// shared text), Quote the marked words, AnchorStart/AnchorEnd where they are in that text now (UTF-16 code units,
// as browsers count; -1 when the comment marks nothing or the words are gone: then TextChanged).
type PostComment struct {
	ID          string  `json:"id"`
	PostID      string  `json:"post_id"`
	ParentID    string  `json:"parent_id"`
	AuthorName  string  `json:"author_name"`
	AuthorEmail string  `json:"author_email,omitempty"`
	AuthorKind  string  `json:"author_kind"` // reviewer (on a preview page) or team (signed in, an API key, an agent)
	Body        string  `json:"body"`
	Quote       string  `json:"quote"`
	ChannelID   string  `json:"channel_id"`
	Part        int64   `json:"part"`
	AnchorStart int64   `json:"anchor_start"`
	AnchorEnd   int64   `json:"anchor_end"`
	TextChanged bool    `json:"text_changed"`
	ResolvedAt  *string `json:"resolved_at"`
	ResolvedBy  string  `json:"resolved_by"`
	CreatedAt   string  `json:"created_at"`
}

// PreviewCommentInput is a reviewer's comment: a new thread (optionally on a passage: Quote with its Part,
// ChannelID and offsets) or, with ParentID, a reply.
type PreviewCommentInput struct {
	Name        string `json:"name"`
	Email       string `json:"email"`
	Body        string `json:"body"`
	Quote       string `json:"quote"`
	ChannelID   string `json:"channel_id"`
	Part        int64  `json:"part"`
	AnchorStart *int64 `json:"anchor_start"`
	AnchorEnd   *int64 `json:"anchor_end"`
	ParentID    string `json:"parent_id"`
}

// PostCommentInput is a comment by the team (dashboard, API, agents): a reply to a thread or a new one.
type PostCommentInput struct {
	Body        string `json:"body"`
	ParentID    string `json:"parent_id"`
	Quote       string `json:"quote"`
	ChannelID   string `json:"channel_id"`
	Part        int64  `json:"part"`
	AnchorStart *int64 `json:"anchor_start"`
	AnchorEnd   *int64 `json:"anchor_end"`
}

// ResolveInput: who resolves a thread on a preview page (reviewers have no account).
type ResolveInput struct {
	Name string `json:"name"`
}

// PublicPreview is what the preview page shows: no ids beyond what the page needs, no emails.
type PublicPreview struct {
	Workspace   string          `json:"workspace"`
	Content     string          `json:"content"`
	Status      string          `json:"status"`
	PublishAt   *string         `json:"publish_at"`
	PublishedAt *string         `json:"published_at"`
	Media       []PreviewMedia  `json:"media"`
	Thread      []PreviewPart   `json:"thread"`
	Targets     []PreviewTarget `json:"targets"`
	Comments    []PostComment   `json:"comments"`
	ExpiresAt   *string         `json:"expires_at"`
}

type PreviewMedia struct {
	ID       string `json:"id"`
	MimeType string `json:"mime_type"`
}

type PreviewPart struct {
	Content      string         `json:"content"`
	Media        []PreviewMedia `json:"media"`
	DelaySeconds int64          `json:"delay_seconds"`
}

type PreviewTarget struct {
	ChannelID    string `json:"channel_id"`
	ChannelName  string `json:"channel_name"`
	Handle       string `json:"handle"`
	AvatarURL    string `json:"avatar_url"`
	Provider     string `json:"provider"`
	ProviderName string `json:"provider_name"`
	Content      string `json:"content"` // the text this channel gets, links handled as the channel does
	// Hashtags: the post's tags as the platform gets them after the text ("#launch #news"); "" for none and for
	// your own automation, which gets them apart. Apart from Content so comments anchored in the text stay put.
	Hashtags    string  `json:"hashtags"`
	Status      string  `json:"status"`
	ExternalURL string  `json:"external_url"`
	PublishedAt *string `json:"published_at"`
}

func (s *Service) toPreviewLink(l db.PreviewLink) PreviewLink {
	out := PreviewLink{ID: l.ID, PostID: l.PostID, CreatedAt: l.CreatedAt}
	if l.ExpiresAt.Valid {
		out.ExpiresAt = &l.ExpiresAt.String
	}
	if l.RevokedAt.Valid {
		out.RevokedAt = &l.RevokedAt.String
	}
	out.Active = !l.RevokedAt.Valid && (!l.ExpiresAt.Valid || l.ExpiresAt.String > s.now())
	return out
}

// CreatePreviewLink makes a link to the post's preview page (posts:write: it shows the post to anyone with the
// link, so it is no read). origin is the dashboard origin of the request,
// used when PUBLIC_DASHBOARD_URL is not set.
func (s *Service) CreatePreviewLink(ctx context.Context, p Principal, postID string, in PreviewLinkInput, origin string) (PreviewLink, error) {
	if err := p.require(ScopePostsWrite); err != nil {
		return PreviewLink{}, err
	}
	if in.ExpiresInDays < 0 || in.ExpiresInDays > previewMaxDays {
		return PreviewLink{}, invalid("expires_in_days", "must be 0 (never) to 365 days")
	}
	row, err := s.Q.GetPost(ctx, db.GetPostParams{WorkspaceID: p.WorkspaceID, ID: postID})
	if isNoRows(err) {
		return PreviewLink{}, notFound("post")
	}
	if err != nil {
		return PreviewLink{}, err
	}
	token := randomString(32)
	now := s.Now().UTC()
	l := db.PreviewLink{ID: newID("prv"), WorkspaceID: p.WorkspaceID, PostID: postID, TokenHash: hashSecret(token),
		CreatedBy: p.Kind + ":" + p.ID, CreatedAt: formatTime(now)}
	if in.ExpiresInDays > 0 {
		l.ExpiresAt = nullStr(formatTime(now.Add(time.Duration(in.ExpiresInDays) * 24 * time.Hour)))
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := q.CreatePreviewLink(ctx, db.CreatePreviewLinkParams{ID: l.ID, WorkspaceID: l.WorkspaceID, PostID: l.PostID,
			TokenHash: l.TokenHash, CreatedBy: l.CreatedBy, CreatedAt: l.CreatedAt, ExpiresAt: l.ExpiresAt}); err != nil {
			return err
		}
		_, err := s.emit(ctx, q, p, "post.preview_shared", row.CollectionID, postID,
			map[string]any{"preview_id": l.ID, "expires_at": l.ExpiresAt.String})
		return err
	})
	if err != nil {
		return PreviewLink{}, err
	}
	out := s.toPreviewLink(l)
	out.Token = token
	// The token travels in the fragment: it never reaches server logs, proxies or Referer headers.
	out.URL = s.dashboardBase(origin) + "/preview#" + token
	return out, nil
}

// dashboardBase is the dashboard's public origin: PUBLIC_DASHBOARD_URL, else the request's origin.
func (s *Service) dashboardBase(origin string) string {
	base := s.Cfg.PublicDashboardURL
	if base == "" {
		base = origin
	}
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		base = "http://localhost:8080"
	}
	return strings.TrimRight(base, "/")
}

func (s *Service) ListPreviewLinks(ctx context.Context, p Principal, postID string) ([]PreviewLink, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListPreviewLinks(ctx, db.ListPreviewLinksParams{WorkspaceID: p.WorkspaceID, PostID: postID})
	if err != nil {
		return nil, err
	}
	out := make([]PreviewLink, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.toPreviewLink(r))
	}
	return out, nil
}

func (s *Service) RevokePreviewLink(ctx context.Context, p Principal, postID, id string) error {
	if err := p.require(ScopePostsWrite); err != nil {
		return err
	}
	n, err := s.Q.RevokePreviewLink(ctx, db.RevokePreviewLinkParams{Now: nullStr(s.now()), WorkspaceID: p.WorkspaceID, PostID: postID, ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound("preview link")
	}
	return nil
}

// toComment shapes a stored comment; texts (from commentTexts) re-find its passage in the post as it is now.
func toComment(c db.PostComment, withEmail bool, texts map[textKey]string) PostComment {
	out := PostComment{ID: c.ID, PostID: c.PostID, ParentID: c.ParentID, AuthorName: c.AuthorName, AuthorKind: "reviewer",
		Body: c.Body, Quote: c.Quote, ChannelID: c.ChannelID, Part: c.Part, AnchorStart: -1, AnchorEnd: -1,
		ResolvedBy: c.ResolvedBy, CreatedAt: c.CreatedAt}
	if c.PreviewID == "" {
		out.AuthorKind = "team"
	}
	if withEmail {
		out.AuthorEmail = c.AuthorEmail
	}
	if c.ResolvedAt.Valid {
		out.ResolvedAt = &c.ResolvedAt.String
	}
	if c.Quote != "" && c.ParentID == "" {
		text, ok := texts[textKey{c.ChannelID, c.Part}]
		if start, end, found := anchorIn(text, c.Quote, c.AnchorStart, c.AnchorEnd); ok && found {
			out.AnchorStart, out.AnchorEnd = start, end
		} else {
			out.TextChanged = true
		}
	}
	return out
}

// textKey names one text a comment can mark: a channel's version ("" the shared text) and a part (0 the post,
// n its n-th comment).
type textKey struct {
	channel string
	part    int64
}

// commentTexts are the texts a preview shows, by channel and part: the post as each channel gets it (links
// handled as that channel does), the shared text, and the thread's comments (the same on every channel).
func (s *Service) commentTexts(ctx context.Context, workspaceID string, post Post) map[textKey]string {
	texts := map[textKey]string{{"", 0}: post.Content}
	for i, part := range post.Thread {
		texts[textKey{"", int64(i + 1)}] = part.Content
	}
	for _, t := range post.Targets {
		text := post.Content
		if t.Content != nil {
			text = *t.Content
		}
		if ch, err := s.Q.GetChannelByID(ctx, t.ChannelID); err == nil && ch.WorkspaceID == workspaceID {
			text, _ = providers.ApplyLinks(channelLinks(ch), text)
		}
		texts[textKey{t.ChannelID, 0}] = text
		for i, part := range post.Thread {
			texts[textKey{t.ChannelID, int64(i + 1)}] = part.Content
		}
	}
	return texts
}

// anchorIn finds quote in text: at start..end (UTF-16 offsets, as the browser sent them) when the words are
// still there, else their first occurrence. Offsets are never trusted: out-of-range ones are ignored.
func anchorIn(text, quote string, start, end int64) (int64, int64, bool) {
	if quote == "" {
		return -1, -1, false
	}
	units := utf16.Encode([]rune(text))
	q := utf16.Encode([]rune(quote))
	if start >= 0 && end == start+int64(len(q)) && end <= int64(len(units)) && slices.Equal(units[start:end], q) {
		return start, end, true
	}
	for i := 0; i+len(q) <= len(units); i++ {
		if slices.Equal(units[i:i+len(q)], q) {
			return int64(i), int64(i + len(q)), true
		}
	}
	return -1, -1, false
}

// newComment checks and builds a comment for post: a reply joins its thread's first comment (and takes no
// passage), a new thread may mark a passage that must be in the text it names.
func (s *Service) newComment(ctx context.Context, workspaceID string, post Post, body, quote, channelID, parentID string,
	part int64, start, end *int64) (db.PostComment, error) {
	body, quote = strings.TrimSpace(body), strings.TrimSpace(quote)
	fields := map[string]string{}
	switch n := utf8.RuneCountInString(body); {
	case n == 0:
		fields["body"] = "required"
	case n > previewCommentMaxRunes:
		fields["body"] = "at most 2000 characters"
	}
	if utf8.RuneCountInString(quote) > previewQuoteMaxRunes {
		fields["quote"] = "at most 500 characters"
	}
	c := db.PostComment{ID: newID("cmt"), WorkspaceID: workspaceID, PostID: post.ID, Body: body, AnchorStart: -1, AnchorEnd: -1,
		CreatedAt: s.now()}
	if parentID != "" {
		parent, err := s.Q.GetPostComment(ctx, db.GetPostCommentParams{WorkspaceID: workspaceID, PostID: post.ID, ID: parentID})
		if err != nil {
			fields["parent_id"] = "no such comment on this post"
		} else {
			if parent.ParentID != "" {
				parent.ID = parent.ParentID // a reply to a reply joins the same thread
			}
			c.ParentID = parent.ID
		}
	} else {
		if channelID != "" && !slices.ContainsFunc(post.Targets, func(t PostTarget) bool { return t.ChannelID == channelID }) {
			channelID = ""
		}
		c.Quote, c.ChannelID = quote, channelID
		if quote != "" {
			if part < 0 || part > int64(len(post.Thread)) {
				fields["part"] = "no such part of the post"
			} else {
				texts := s.commentTexts(ctx, workspaceID, post)
				var st, en int64 = -1, -1
				if start != nil && end != nil {
					st, en = *start, *end
				}
				if a, b, ok := anchorIn(texts[textKey{channelID, part}], quote, st, en); ok {
					c.Part, c.AnchorStart, c.AnchorEnd = part, a, b
				} else {
					fields["quote"] = "these words are not in the post"
				}
			}
		}
	}
	if len(fields) > 0 {
		return c, &Error{Code: "validation_failed", Status: 422, Message: "check the highlighted fields", Fields: fields}
	}
	return c, nil
}

// AddPostComment: the team comments on a post's feedback (a reply, or a new thread), as the signed-in person
// or the key's name.
func (s *Service) AddPostComment(ctx context.Context, p Principal, postID string, in PostCommentInput) (PostComment, error) {
	if err := p.require(ScopePostsWrite); err != nil {
		return PostComment{}, err
	}
	post, _, err := s.loadPost(ctx, s.Q, p.WorkspaceID, postID)
	if err != nil {
		return PostComment{}, err
	}
	c, err := s.newComment(ctx, p.WorkspaceID, post, in.Body, in.Quote, in.ChannelID, in.ParentID, in.Part, in.AnchorStart, in.AnchorEnd)
	if err != nil {
		return PostComment{}, err
	}
	c.AuthorName, c.AuthorUser = strings.TrimSpace(p.Name), p.UserID
	if c.AuthorName == "" {
		c.AuthorName = "Team"
	}
	if err := s.Q.CreatePostComment(ctx, createCommentParams(c)); err != nil {
		return PostComment{}, err
	}
	return toComment(c, true, s.commentTexts(ctx, p.WorkspaceID, post)), nil
}

func createCommentParams(c db.PostComment) db.CreatePostCommentParams {
	return db.CreatePostCommentParams{ID: c.ID, WorkspaceID: c.WorkspaceID, PostID: c.PostID, PreviewID: c.PreviewID,
		AuthorName: c.AuthorName, AuthorEmail: c.AuthorEmail, Body: c.Body, Quote: c.Quote, ChannelID: c.ChannelID,
		CreatedAt: c.CreatedAt, ParentID: c.ParentID, Part: c.Part, AnchorStart: c.AnchorStart, AnchorEnd: c.AnchorEnd,
		AuthorUser: c.AuthorUser}
}

// SetPostCommentResolved resolves (or reopens) a feedback thread by its first comment.
func (s *Service) SetPostCommentResolved(ctx context.Context, p Principal, postID, id string, resolved bool) (PostComment, error) {
	if err := p.require(ScopePostsWrite); err != nil {
		return PostComment{}, err
	}
	by := strings.TrimSpace(p.Name)
	if by == "" {
		by = "Team"
	}
	return s.setResolved(ctx, p.WorkspaceID, postID, id, resolved, by, true)
}

func (s *Service) setResolved(ctx context.Context, workspaceID, postID, id string, resolved bool, by string, withEmail bool) (PostComment, error) {
	at, who := sql.NullString{}, ""
	if resolved {
		at, who = nullStr(s.now()), by
	}
	n, err := s.Q.SetCommentThreadResolved(ctx, db.SetCommentThreadResolvedParams{ResolvedAt: at, ResolvedBy: who,
		WorkspaceID: workspaceID, PostID: postID, ID: id})
	if err != nil {
		return PostComment{}, err
	}
	if n == 0 {
		return PostComment{}, notFound("comment thread")
	}
	c, err := s.Q.GetPostComment(ctx, db.GetPostCommentParams{WorkspaceID: workspaceID, PostID: postID, ID: id})
	if err != nil {
		return PostComment{}, err
	}
	post, _, err := s.loadPost(ctx, s.Q, workspaceID, postID)
	if err != nil {
		return PostComment{}, err
	}
	return toComment(c, withEmail, s.commentTexts(ctx, workspaceID, post)), nil
}

// SetPreviewCommentResolved: a reviewer resolves (or reopens) a thread on the preview page.
func (s *Service) SetPreviewCommentResolved(ctx context.Context, token, id string, in ResolveInput, resolved bool) (PostComment, error) {
	l, err := s.previewLink(ctx, token)
	if err != nil {
		return PostComment{}, err
	}
	name := strings.TrimSpace(in.Name)
	if resolved {
		switch n := utf8.RuneCountInString(name); {
		case n == 0:
			return PostComment{}, invalid("name", "required: say who you are")
		case n > previewNameMaxRunes:
			return PostComment{}, invalid("name", "at most 80 characters")
		}
	}
	return s.setResolved(ctx, l.WorkspaceID, l.PostID, id, resolved, name, false)
}

// ListPostComments: the feedback reviewers left on the post's preview pages.
// CommentPage: one page of a post's comments, oldest first; NextCursor fetches the later ones.
type CommentPage struct {
	Data       []PostComment `json:"data"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// ListPostComments lists a post's comments, oldest first, a page at a time (500 by default, at most 1000).
func (s *Service) ListPostComments(ctx context.Context, p Principal, postID string, limit int, cursor string) (CommentPage, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return CommentPage{}, err
	}
	after, err := readKeysetCursor(cursor, 2)
	if err != nil {
		return CommentPage{}, err
	}
	post, _, err := s.loadPost(ctx, s.Q, p.WorkspaceID, postID)
	if err != nil {
		return CommentPage{}, err
	}
	lim := clampLimit(limit, 500, 1000)
	rows, err := s.Q.ListPostComments(ctx, db.ListPostCommentsParams{WorkspaceID: p.WorkspaceID, PostID: postID,
		AfterAt: after[0], AfterID: after[1], Lim: int64(lim + 1)})
	if err != nil {
		return CommentPage{}, err
	}
	texts := s.commentTexts(ctx, p.WorkspaceID, post)
	page := CommentPage{Data: make([]PostComment, 0, min(len(rows), lim))}
	for i, r := range rows {
		if i == lim {
			last := rows[i-1]
			page.NextCursor = keysetCursor(last.CreatedAt, last.ID)
			break
		}
		page.Data = append(page.Data, toComment(r, true, texts))
	}
	return page, nil
}

func (s *Service) DeletePostComment(ctx context.Context, p Principal, postID, id string) error {
	if err := p.require(ScopePostsWrite); err != nil {
		return err
	}
	n, err := s.Q.DeletePostComment(ctx, db.DeletePostCommentParams{WorkspaceID: p.WorkspaceID, PostID: postID, ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound("comment")
	}
	return nil
}

// previewLink resolves a token to a live link (not revoked, not expired).
func (s *Service) previewLink(ctx context.Context, token string) (db.PreviewLink, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return db.PreviewLink{}, &Error{Code: "not_found", Status: 404, Message: previewInvalidLinkMessage}
	}
	l, err := s.Q.GetPreviewLinkByHash(ctx, hashSecret(token))
	if err != nil || l.RevokedAt.Valid || (l.ExpiresAt.Valid && l.ExpiresAt.String <= s.now()) {
		return db.PreviewLink{}, &Error{Code: "not_found", Status: 404, Message: previewInvalidLinkMessage}
	}
	return l, nil
}

func previewMedia(items []Media) []PreviewMedia {
	out := make([]PreviewMedia, 0, len(items))
	for _, m := range items {
		out = append(out, PreviewMedia{ID: m.ID, MimeType: m.MimeType})
	}
	return out
}

// OpenPreview is the preview page's content for a token.
func (s *Service) OpenPreview(ctx context.Context, token string) (PublicPreview, error) {
	l, err := s.previewLink(ctx, token)
	if err != nil {
		return PublicPreview{}, err
	}
	post, _, err := s.loadPost(ctx, s.Q, l.WorkspaceID, l.PostID)
	if err != nil {
		return PublicPreview{}, &Error{Code: "not_found", Status: 404, Message: previewInvalidLinkMessage}
	}
	out := PublicPreview{Content: post.Content, Status: post.Status, PublishAt: post.PublishAt, PublishedAt: post.PublishedAt,
		Media: previewMedia(post.MediaItems), Thread: []PreviewPart{}, Targets: []PreviewTarget{}, Comments: []PostComment{}}
	if l.ExpiresAt.Valid {
		out.ExpiresAt = &l.ExpiresAt.String
	}
	if ws, err := s.Q.GetWorkspace(ctx, l.WorkspaceID); err == nil {
		out.Workspace = ws.Name
	}
	for _, part := range post.Thread {
		out.Thread = append(out.Thread, PreviewPart{Content: part.Content, Media: previewMedia(part.MediaItems), DelaySeconds: part.DelaySeconds})
	}
	for _, t := range post.Targets {
		text := post.Content
		if t.Content != nil {
			text = *t.Content
		}
		pt := PreviewTarget{ChannelID: t.ChannelID, ChannelName: t.ChannelName, AvatarURL: t.AvatarURL, Provider: t.Provider,
			ProviderName: t.Provider, Content: text, Status: t.Status, PublishedAt: t.PublishedAt}
		if t.Status == "published" {
			pt.ExternalURL = t.ExternalURL
		}
		if e, ok := providers.Get(t.Provider); ok {
			pt.ProviderName = e.Name()
		}
		if ch, err := s.Q.GetChannelByID(ctx, t.ChannelID); err == nil && ch.WorkspaceID == l.WorkspaceID {
			pt.Handle = ch.Handle
			pt.Content, _ = providers.ApplyLinks(channelLinks(ch), text)
		}
		if e, ok := providers.Get(t.Provider); t.Provider != "custom" && (!ok || !providers.NativeTags(e.SettingsFields())) { // as publishTarget
			if full := providers.AppendHashtags(pt.Content, post.Tags); full != pt.Content {
				pt.Hashtags = strings.TrimSpace(strings.TrimPrefix(full, strings.TrimRight(pt.Content, " \t\n")))
			}
		}
		out.Targets = append(out.Targets, pt)
	}
	// The public page shows a post's first 1000 comments.
	comments, err := s.Q.ListPostComments(ctx, db.ListPostCommentsParams{WorkspaceID: l.WorkspaceID, PostID: l.PostID, AfterAt: "", Lim: 1000})
	if err != nil {
		return PublicPreview{}, err
	}
	texts := s.commentTexts(ctx, l.WorkspaceID, post)
	for _, c := range comments {
		out.Comments = append(out.Comments, toComment(c, false, texts))
	}
	return out, nil
}

// AddPreviewComment records a reviewer's comment, limited per link.
func (s *Service) AddPreviewComment(ctx context.Context, token string, in PreviewCommentInput) (PostComment, error) {
	l, err := s.previewLink(ctx, token)
	if err != nil {
		return PostComment{}, err
	}
	in.Name, in.Email = strings.TrimSpace(in.Name), strings.TrimSpace(in.Email)
	fields := map[string]string{}
	switch n := utf8.RuneCountInString(in.Name); {
	case n == 0:
		fields["name"] = "required: say who you are"
	case n > previewNameMaxRunes:
		fields["name"] = "at most 80 characters"
	}
	if in.Email != "" {
		if a, err := mail.ParseAddress(in.Email); err != nil || a.Address != in.Email || len(in.Email) > 254 {
			fields["email"] = "not an email address"
		}
	}
	post, row, err := s.loadPost(ctx, s.Q, l.WorkspaceID, l.PostID)
	if err != nil {
		return PostComment{}, &Error{Code: "not_found", Status: 404, Message: previewInvalidLinkMessage}
	}
	c, err := s.newComment(ctx, l.WorkspaceID, post, in.Body, in.Quote, in.ChannelID, in.ParentID, in.Part, in.AnchorStart, in.AnchorEnd)
	var ve *Error
	if errors.As(err, &ve) && ve.Fields != nil {
		maps.Copy(fields, ve.Fields)
	} else if err != nil {
		return PostComment{}, err
	}
	if len(fields) > 0 {
		return PostComment{}, &Error{Code: "validation_failed", Status: 422, Message: "check the highlighted fields", Fields: fields}
	}
	since := formatTime(s.Now().Add(-time.Hour))
	if n, err := s.Q.CountRecentPreviewComments(ctx, db.CountRecentPreviewCommentsParams{PreviewID: l.ID, Since: since}); err != nil {
		return PostComment{}, err
	} else if n >= previewCommentsPerHour {
		return PostComment{}, &Error{Code: "rate_limited", Status: 429, Message: "this preview got many comments in the last hour: try again later"}
	}
	c.PreviewID, c.AuthorName, c.AuthorEmail = l.ID, in.Name, in.Email
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := q.CreatePostComment(ctx, createCommentParams(c)); err != nil {
			return err
		}
		reviewer := Principal{WorkspaceID: l.WorkspaceID, Kind: PrincipalSystem, ID: "preview:" + l.ID, Name: in.Name}
		_, err := s.emitRef(ctx, q, reviewer, "post.feedback_added", Ref{Collection: row.CollectionID, Post: l.PostID, Channel: c.ChannelID},
			map[string]any{"comment_id": c.ID, "parent_id": c.ParentID, "author_name": in.Name, "body": truncateRunes(c.Body, 280), "quote": c.Quote})
		return err
	})
	if err != nil {
		return PostComment{}, err
	}
	return toComment(c, false, s.commentTexts(ctx, l.WorkspaceID, post)), nil
}

// OpenPreviewMedia serves a picture or video of the previewed post (the post's or its comments' media only).
func (s *Service) OpenPreviewMedia(ctx context.Context, token, mediaID string) (io.ReadSeekCloser, db.Medium, error) {
	l, err := s.previewLink(ctx, token)
	if err != nil {
		return nil, db.Medium{}, err
	}
	row, err := s.Q.GetPost(ctx, db.GetPostParams{WorkspaceID: l.WorkspaceID, ID: l.PostID})
	if err != nil {
		return nil, db.Medium{}, notFound("media")
	}
	var ids []string
	_ = json.Unmarshal([]byte(row.Media), &ids)
	for _, part := range parseThread(row.Thread) {
		ids = append(ids, part.Media...)
	}
	if !slices.Contains(ids, mediaID) {
		return nil, db.Medium{}, notFound("media")
	}
	sys := Principal{WorkspaceID: l.WorkspaceID, Kind: PrincipalSystem, ID: "preview", Name: "Preview", Scopes: AdminKeyScopes}
	return s.OpenMedia(ctx, sys, mediaID)
}

// ---- metrics ----

// TargetMetrics is the last numbers read for one channel of a post.
type TargetMetrics struct {
	ChannelID   string             `json:"channel_id"`
	ChannelName string             `json:"channel_name"`
	Provider    string             `json:"provider"`
	Status      string             `json:"status"`
	ExternalURL string             `json:"external_url"`
	Available   bool               `json:"available"` // the platform reports numbers (or your automation sends them)
	Metrics     *providers.Metrics `json:"metrics"`
	UpdatedAt   *string            `json:"updated_at"`
	Error       string             `json:"error,omitempty"`
}

// PostMetrics reads each published target's numbers from its platform when the cached ones are older than
// ten minutes (or refresh is set), one platform call per target outside any transaction, and stores them.
func (s *Service) PostMetrics(ctx context.Context, p Principal, postID string, refresh bool) ([]TargetMetrics, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return nil, err
	}
	if _, err := s.Q.GetPost(ctx, db.GetPostParams{WorkspaceID: p.WorkspaceID, ID: postID}); isNoRows(err) {
		return nil, notFound("post")
	} else if err != nil {
		return nil, err
	}
	rows, err := s.Q.ListTargets(ctx, postID)
	if err != nil {
		return nil, err
	}
	out := make([]TargetMetrics, 0, len(rows))
	for _, t := range rows {
		m := TargetMetrics{ChannelID: t.ChannelID, ChannelName: t.ChannelName, Provider: t.Provider, Status: t.Status, ExternalURL: t.ExternalUrl}
		if t.Metrics != "" {
			var cached providers.Metrics
			if json.Unmarshal([]byte(t.Metrics), &cached) == nil {
				m.Metrics = &cached
			}
		}
		if t.MetricsAt.Valid {
			m.UpdatedAt = &t.MetricsAt.String
		}
		e, ok := providers.Get(t.Provider)
		reader, canRead := e.Provider.(providers.MetricsReader)
		m.Available = (ok && canRead) || t.Provider == "custom"
		stale := !t.MetricsAt.Valid || t.MetricsAt.String < formatTime(s.Now().Add(-metricsFresh))
		if ok && canRead && t.Status == "published" && t.ExternalID != "" && (refresh || stale) {
			if fresh, err := s.readMetrics(ctx, p.WorkspaceID, t.ChannelID, e, reader, t.ExternalID); err != nil {
				s.logger(ctx).Info("post metrics: not read", "post_id", postID, "channel_id", t.ChannelID, "provider", t.Provider, "err", err)
				m.Error = readFailure(e.Name(), err)
			} else {
				now := s.now()
				if err := s.Q.SetTargetMetrics(ctx, db.SetTargetMetricsParams{Metrics: mustJSON(fresh), MetricsAt: nullStr(now),
					PostID: postID, ChannelID: t.ChannelID}); err != nil {
					return nil, err
				}
				m.Metrics, m.UpdatedAt = &fresh, &now
			}
		}
		out = append(out, m)
	}
	return out, nil
}

func (s *Service) readMetrics(ctx context.Context, workspaceID, channelID string, e providers.Entry, r providers.MetricsReader, externalID string) (providers.Metrics, error) {
	// Expired subscriptions can read cached reports, but cannot keep generating provider charges.
	if err := s.CheckWritable(ctx, workspaceID); err != nil {
		return providers.Metrics{}, err
	}
	ch, err := s.Q.GetChannelByID(ctx, channelID)
	if err != nil || ch.WorkspaceID != workspaceID {
		return providers.Metrics{}, errChannelGone
	}
	// A disconnected channel has no sign-in left to read with (the platform would only say "unregistered caller").
	switch ch.Status {
	case "needs_reconnect":
		return providers.Metrics{}, &providers.Failure{Kind: providers.ErrReauth, Msg: "the channel needs reconnecting"}
	case "connected":
	default:
		return providers.Metrics{}, errChannelGone
	}
	var creds providers.Credentials
	if err := s.unseal(ch.Credentials, &creds); err != nil {
		return providers.Metrics{}, err
	}
	if creds, err = s.refreshIfDue(ctx, ch, e, creds); err != nil {
		return providers.Metrics{}, err
	}
	pctx, cancel := context.WithTimeout(s.publishingCtx(ctx, workspaceID), 15*time.Second)
	defer cancel()
	return r.Metrics(pctx, creds, externalID)
}

// ReportTargetMetrics: your own automation sends the numbers of a post it published.
func (s *Service) ReportTargetMetrics(ctx context.Context, p Principal, postID, channelID string, m providers.Metrics) (TargetMetrics, error) {
	if err := p.require(ScopePostsPublish); err != nil {
		return TargetMetrics{}, err
	}
	if _, err := s.Q.GetPost(ctx, db.GetPostParams{WorkspaceID: p.WorkspaceID, ID: postID}); isNoRows(err) {
		return TargetMetrics{}, notFound("post")
	} else if err != nil {
		return TargetMetrics{}, err
	}
	t, err := s.Q.GetTarget(ctx, db.GetTargetParams{PostID: postID, ChannelID: channelID})
	if isNoRows(err) {
		return TargetMetrics{}, notFound("target")
	}
	if err != nil {
		return TargetMetrics{}, err
	}
	ch, err := s.Q.GetChannelByID(ctx, channelID)
	if err != nil || ch.WorkspaceID != p.WorkspaceID {
		return TargetMetrics{}, notFound("target")
	}
	if ch.Provider != "custom" {
		// 409 like a reported result for such a target: the request is fine, the target is not one that takes it.
		return TargetMetrics{}, conflict("only channels on your own automation report their numbers; Staqly reads the others from the platform")
	}
	for _, v := range []*int64{m.Impressions, m.Likes, m.Reposts, m.Replies, m.Quotes, m.Bookmarks, m.LinkClicks,
		m.Shares, m.WatchMinutes, m.AvgViewSeconds, m.SubscribersGained} {
		if v != nil && *v < 0 {
			return TargetMetrics{}, invalid("metrics", "numbers can't be negative")
		}
	}
	now := s.now()
	if err := s.Q.SetTargetMetrics(ctx, db.SetTargetMetricsParams{Metrics: mustJSON(m), MetricsAt: nullStr(now), PostID: postID, ChannelID: channelID}); err != nil {
		return TargetMetrics{}, err
	}
	return TargetMetrics{ChannelID: channelID, ChannelName: ch.Name, Provider: ch.Provider, Status: t.Status, ExternalURL: t.ExternalUrl,
		Available: true, Metrics: &m, UpdatedAt: &now}, nil
}

// errChannelGone: the post went out on a channel that is no longer connected to staqly.
var errChannelGone = errors.New("the channel is no longer connected")

// readFailure: what a person reads when a platform's numbers or comments can't be read right now. The platform's own
// words are technical (and logged); an app error (an expired subscription) already speaks to people.
func readFailure(platform string, err error) string {
	var appErr *Error
	switch {
	case errors.As(err, &appErr):
		return appErr.Message
	case errors.Is(err, errChannelGone):
		return "This channel is no longer connected to staqly, so the numbers aren't updated. Connect the account again under Channels to see new ones on your next posts."
	case errors.Is(err, providers.ErrReauth):
		return "staqly lost access to this " + platform + " account. Reconnect it under Channels and try again."
	case errors.Is(err, providers.ErrRejected):
		return platform + " didn't share them this time. If the post was deleted on " + platform + ", they won't change any more."
	default:
		return "We couldn't reach " + platform + " just now. Try again in a moment."
	}
}
