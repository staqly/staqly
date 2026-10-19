package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Threads: a post's follow-up parts ("comments"), published as replies under it, one part per job, each
// delay_seconds after the one before (the first after the post). Only platforms that can reply
// (providers.Replier) publish them; on the others the replies are skipped, and your own automation gets the
// thread in its request instead.

const (
	maxThreadParts = 25
	maxReplyDelay  = 24 * 60 * 60
)

// ThreadPart is one follow-up part as clients send it.
type ThreadPart struct {
	Content      string   `json:"content"`
	Media        []string `json:"media"`
	DelaySeconds int64    `json:"delay_seconds"`
}

// ThreadPartOut is a part as the API returns it, with its media resolved.
type ThreadPartOut struct {
	ThreadPart
	MediaItems []Media `json:"media_items"`
}

// Reply is how one thread part went on one channel.
type Reply struct {
	Index       int     `json:"index"`
	Status      string  `json:"status"` // pending, publishing, published, failed, skipped
	ExternalID  string  `json:"external_id"`
	ExternalURL string  `json:"external_url"`
	Error       string  `json:"error"`
	PublishedAt *string `json:"published_at"`
}

func parseThread(raw string) []ThreadPart {
	out := []ThreadPart{}
	_ = json.Unmarshal([]byte(raw), &out)
	for i := range out {
		if out[i].Media == nil {
			out[i].Media = []string{}
		}
	}
	return out
}

func parseReplies(raw string) []Reply {
	out := []Reply{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// repliesFor lists one reply per thread part: what is stored, or pending.
func repliesFor(parts int, raw string) []Reply {
	stored := parseReplies(raw)
	out := make([]Reply, parts)
	for i := range out {
		out[i] = Reply{Index: i, Status: "pending"}
		if i < len(stored) {
			out[i] = stored[i]
			out[i].Index = i
		}
	}
	return out
}

func threadField(i int, field string) string { return fmt.Sprintf("thread.%d.%s", i, field) }

// checkThread validates the parts themselves (count, text, media, delays); nil means no thread.
func (s *Service) checkThread(ctx context.Context, q *db.Queries, workspaceID string, parts []ThreadPart) ([]ThreadPart, error) {
	if parts == nil {
		return []ThreadPart{}, nil
	}
	if len(parts) > maxThreadParts {
		return nil, invalid("thread", fmt.Sprintf("at most %d parts", maxThreadParts))
	}
	out := make([]ThreadPart, len(parts))
	for i, part := range parts {
		if part.Media == nil {
			part.Media = []string{}
		}
		if utf8.RuneCountInString(part.Content) > maxPostContent {
			return nil, invalid(threadField(i, "content"), fmt.Sprintf("at most %d characters", maxPostContent))
		}
		if strings.TrimSpace(part.Content) == "" && len(part.Media) == 0 {
			return nil, invalid(threadField(i, "content"), "write something or add media")
		}
		if part.DelaySeconds < 0 || part.DelaySeconds > maxReplyDelay {
			return nil, invalid(threadField(i, "delay_seconds"), "must be 0 to 86400 seconds (a day)")
		}
		if err := s.checkMedia(ctx, q, workspaceID, part.Media); err != nil {
			var ae *Error
			if errors.As(err, &ae) && ae.Fields["media"] != "" {
				return nil, invalid(threadField(i, "media"), ae.Fields["media"])
			}
			return nil, err
		}
		out[i] = part
	}
	return out, nil
}

// checkThreadLimits holds every part to each target channel's limits (characters, files), like the post.
func checkThreadLimits(parts []ThreadPart, chans map[string]db.Channel) error {
	for _, ch := range chans {
		e, ok := providers.Get(ch.Provider)
		if !ok {
			continue
		}
		l := e.Limits()
		for i, part := range parts {
			if n := utf8.RuneCountInString(part.Content); l.MaxChars > 0 && n > l.MaxChars {
				return invalid(threadField(i, "content"), fmt.Sprintf("at most %d characters for %s (%d)", l.MaxChars, ch.Name, n))
			}
			if len(part.Media) > l.MaxMedia {
				if l.MaxMedia == 0 {
					return invalid(threadField(i, "media"), ch.Name+" takes no media")
				}
				return invalid(threadField(i, "media"), fmt.Sprintf("at most %d files for %s", l.MaxMedia, ch.Name))
			}
		}
	}
	return nil
}

// targetThread is the thread one target publishes: the post's comments, then the channel's last comment
// (the "thread_finisher" setting) when it has one.
func targetThread(raw, settings string) []ThreadPart {
	parts := parseThread(raw)
	set := providers.Settings{}
	_ = json.Unmarshal([]byte(settings), &set)
	if f := strings.TrimSpace(set["thread_finisher"]); f != "" {
		parts = append(parts, ThreadPart{Content: f, Media: []string{}})
	}
	return parts
}

// threadOut resolves a stored thread for the API.
func (s *Service) threadOut(ctx context.Context, q *db.Queries, workspaceID, raw string) []ThreadPartOut {
	parts := parseThread(raw)
	out := make([]ThreadPartOut, len(parts))
	for i, part := range parts {
		out[i] = ThreadPartOut{ThreadPart: part, MediaItems: []Media{}}
		for _, id := range part.Media {
			if m, err := q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: workspaceID, ID: id}); err == nil {
				out[i].MediaItems = append(out[i].MediaItems, s.toMedia(m))
			}
		}
	}
	return out
}

// threadContent is the thread as providers receive it (media read from storage).
func (s *Service) threadContent(ctx context.Context, q *db.Queries, workspaceID, raw string) ([]providers.ThreadPart, error) {
	parts := parseThread(raw)
	out := make([]providers.ThreadPart, len(parts))
	for i, part := range parts {
		files, err := s.mediaFiles(ctx, q, workspaceID, part.Media)
		if err != nil {
			return nil, err
		}
		out[i] = providers.ThreadPart{Text: part.Content, Media: files, DelaySeconds: part.DelaySeconds}
	}
	return out, nil
}

// startThread runs inside the transaction that records a target as published: it queues the first reply
// (platforms that reply) or marks every reply skipped (the others, including your own automation).
func (s *Service) startThread(ctx context.Context, q *db.Queries, row db.Post, channelID string, result providers.Result, now string) error {
	t, err := q.GetTarget(ctx, db.GetTargetParams{PostID: row.ID, ChannelID: channelID})
	if err != nil {
		return err
	}
	parts := targetThread(row.Thread, t.Settings)
	if len(parts) == 0 {
		return nil
	}
	replies := repliesFor(len(parts), "[]")
	canReply := false
	if ch, err := q.GetChannelByID(ctx, channelID); err == nil {
		if e, ok := providers.Get(ch.Provider); ok {
			_, canReply = e.Provider.(providers.Replier)
		}
	}
	if !canReply || result.ExternalID == "" {
		for i := range replies {
			replies[i].Status = "skipped"
		}
		return q.SetTargetReplies(ctx, db.SetTargetRepliesParams{Replies: mustJSON(replies), UpdatedAt: now, PostID: row.ID, ChannelID: channelID})
	}
	if err := q.SetTargetReplies(ctx, db.SetTargetRepliesParams{Replies: mustJSON(replies), UpdatedAt: now, PostID: row.ID, ChannelID: channelID}); err != nil {
		return err
	}
	return s.enqueueReply(ctx, q, row.WorkspaceID, row.ID, channelID, 0, result.ExternalID, parts[0].DelaySeconds)
}

func (s *Service) enqueueReply(ctx context.Context, q *db.Queries, workspaceID, postID, channelID string, index int, parentID string, delay int64) error {
	return s.enqueue(ctx, q, workspaceID, "publish_reply", fmt.Sprintf("publish_reply:%s:%s:%d", postID, channelID, index),
		map[string]any{"post_id": postID, "channel_id": channelID, "index": index, "parent_id": parentID},
		s.Now().Add(time.Duration(delay)*time.Second), 0)
}

// publishReply is the publish_reply job: one thread part under the part (or post) before it.
func (s *Service) publishReply(ctx context.Context, postID, channelID string, index int, parentID string) error {
	row, err := s.Q.GetPostByID(ctx, postID)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	target, err := s.Q.GetTarget(ctx, db.GetTargetParams{PostID: postID, ChannelID: channelID})
	if isNoRows(err) || (err == nil && target.Status != "published") {
		return nil // the target went away, or was never published
	}
	if err != nil {
		return err
	}
	parts := targetThread(row.Thread, target.Settings)
	replies := repliesFor(len(parts), target.Replies)
	if index >= len(parts) || replies[index].Status == "published" || replies[index].Status == "skipped" {
		return nil
	}
	ch, err := s.Q.GetChannelByID(ctx, channelID)
	if err != nil {
		return s.replyEnded(ctx, postID, channelID, index, providers.Result{}, "channel was removed")
	}
	e, ok := providers.Get(ch.Provider)
	replier, can := e.Provider.(providers.Replier)
	if !ok || !can {
		return s.replyEnded(ctx, postID, channelID, index, providers.Result{}, "this platform does not take replies")
	}
	if ch.Status != "connected" {
		return s.replyEnded(ctx, postID, channelID, index, providers.Result{}, "channel is "+strings.ReplaceAll(ch.Status, "_", " "))
	}
	files, err := s.mediaFiles(ctx, s.Q, row.WorkspaceID, parts[index].Media)
	if err != nil {
		return err
	}
	settings := providers.Settings{}
	_ = json.Unmarshal([]byte(target.Settings), &settings)
	var creds providers.Credentials
	if err := s.unseal(ch.Credentials, &creds); err != nil {
		return s.replyEnded(ctx, postID, channelID, index, providers.Result{}, "stored credentials cannot be read: "+err.Error())
	}
	if creds, err = s.refreshIfDue(ctx, ch, e, creds); err != nil {
		if errors.Is(err, providers.ErrReauth) {
			return s.replyEnded(ctx, postID, channelID, index, providers.Result{}, err.Error())
		}
		return err
	}
	text, _ := providers.ApplyLinks(channelLinks(ch), parts[index].Content)
	if ch.Provider == ProviderX {
		if err := s.checkX(ctx, s.Q, row.WorkspaceID, xUnits(text)); err != nil {
			return s.replyEnded(ctx, postID, channelID, index, providers.Result{}, errorMessage(err))
		}
	}
	content := providers.PostContent{PostID: postID, ChannelID: channelID, Text: text, Media: files}
	result, err := replier.Reply(providers.WithHTTPClient(ctx, s.safeClient()), creds, parentID, content, settings)
	// As for the post: what the platform answered is recorded even if the budget ran out meanwhile.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobWriteTimeout)
	defer cancel()
	switch {
	case err == nil:
		return s.replyEnded(wctx, postID, channelID, index, result, "")
	case errors.Is(err, providers.ErrReauth):
		_ = s.channelNeedsReconnect(wctx, ch, err)
		return s.replyEnded(wctx, postID, channelID, index, providers.Result{}, err.Error())
	case errors.Is(err, providers.ErrRejected):
		return s.replyEnded(wctx, postID, channelID, index, providers.Result{}, err.Error())
	}
	return err // the job retries; after the last attempt replyFailed ends the thread
}

// replyEnded records a reply's outcome. Published: the next part is queued under it. Failed: the thread stops
// there and the parts after it are skipped; the post itself stays published.
func (s *Service) replyEnded(ctx context.Context, postID, channelID string, index int, result providers.Result, errMsg string) error {
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
		parts := targetThread(row.Thread, t.Settings)
		if index >= len(parts) {
			return nil
		}
		replies := repliesFor(len(parts), t.Replies)
		now := s.now()
		typ, data := "post.reply_published", map[string]any{"channel_id": channelID, "index": index, "url": result.URL, "external_id": result.ExternalID}
		if errMsg == "" {
			replies[index] = Reply{Index: index, Status: "published", ExternalID: result.ExternalID, ExternalURL: result.URL, PublishedAt: &now}
			if ch, err := q.GetChannelByID(ctx, channelID); err == nil && ch.Provider == ProviderX {
				text, _ := providers.ApplyLinks(channelLinks(ch), parts[index].Content)
				ref := fmt.Sprintf("reply:%s:%s:%d", postID, channelID, index)
				if err := s.meterX(ctx, q, row.WorkspaceID, ref, xUnits(text)); err != nil {
					return err
				}
			}
		} else {
			typ, data = "post.reply_failed", map[string]any{"channel_id": channelID, "index": index, "error": errMsg}
			replies[index] = Reply{Index: index, Status: "failed", Error: truncateRunes(errMsg, 500)}
			for i := index + 1; i < len(replies); i++ {
				replies[i] = Reply{Index: i, Status: "skipped"}
			}
		}
		if err := q.SetTargetReplies(ctx, db.SetTargetRepliesParams{Replies: mustJSON(replies), UpdatedAt: now, PostID: postID, ChannelID: channelID}); err != nil {
			return err
		}
		if _, err := s.emitRef(ctx, q, sys, typ, Ref{Collection: row.CollectionID, Post: postID, Channel: channelID},
			s.postEventData(ctx, q, row.WorkspaceID, postID, data)); err != nil {
			return err
		}
		if errMsg == "" && index+1 < len(parts) {
			return s.enqueueReply(ctx, q, row.WorkspaceID, postID, channelID, index+1, result.ExternalID, parts[index+1].DelaySeconds)
		}
		return nil
	})
}
