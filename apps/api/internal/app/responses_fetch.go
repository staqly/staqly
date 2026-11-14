package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

const (
	responsesWindow      = 7 * 24 * time.Hour // the worker reads targets published this recently
	responsesEvery       = 30 * time.Minute
	checkEvery           = 6 * time.Hour    // a recent target's numbers are read in the background this often
	responsesAnalyzeWait = 30 * time.Second // debounce: reads inside it join the queued analysis (one model call)
)

// ownResponseIDs: the ids that are the account's own posts (the target and its thread parts), so they never count
// as responses. Facebook and Instagram thread parts are "comment:<post>:<comment>"; the bare comment id is added too.
func ownResponseIDs(target db.PostTarget) map[string]bool {
	own := map[string]bool{}
	add := func(id string) {
		if id == "" {
			return
		}
		own[id] = true
		if rest, ok := strings.CutPrefix(id, "comment:"); ok {
			own[rest[strings.LastIndex(rest, ":")+1:]] = true
		}
	}
	add(target.ExternalID)
	for _, r := range parseReplies(target.Replies) {
		add(r.ExternalID)
	}
	return own
}

// readResponses reads the responses under one published target (also while it is being taken down: the last read), stores them (the sentiment of a known one stays)
// and queues the analysis when something new arrived. The provider is called outside any transaction.
func (s *Service) readResponses(ctx context.Context, postID, channelID string) error {
	return s.readResponsesThen(ctx, postID, channelID, responsesAnalyzeWait)
}

// readResponsesThen is readResponses with the analysis of new responses queued `wait` from now: Refresh asks for 0,
// so the person who pressed it sees the AI's reading as soon as the model answers.
func (s *Service) readResponsesThen(ctx context.Context, postID, channelID string, wait time.Duration) error {
	post, err := s.Q.GetPostByID(ctx, postID)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	target, err := s.Q.GetTarget(ctx, db.GetTargetParams{PostID: postID, ChannelID: channelID})
	if isNoRows(err) || (err == nil && (target.Status != "published" || (target.Removal != "" && target.Removal != "removing") || target.ExternalID == "")) {
		return nil
	}
	if err != nil {
		return err
	}
	ch, err := s.Q.GetChannelByID(ctx, channelID)
	if isNoRows(err) || (err == nil && (ch.WorkspaceID != post.WorkspaceID || ch.Status != "connected")) {
		return nil
	}
	if err != nil {
		return err
	}
	e, ok := providers.Get(ch.Provider)
	reader, canRead := e.Provider.(providers.ResponseReader)
	if !ok || !canRead {
		return nil
	}
	if err := s.CheckWritable(ctx, post.WorkspaceID); err != nil {
		return nil // an expired subscription stops the reads, not the job queue
	}
	creds, err := s.channelCredentials(ctx, ch)
	if err != nil {
		return nil
	}
	if creds, err = s.refreshIfDue(ctx, ch, e, creds); err != nil {
		if errors.Is(err, providers.ErrReauth) {
			return nil // refreshIfDue marked the channel needs_reconnect
		}
		return err
	}
	var since time.Time
	if newest, err := s.Q.NewestResponseTime(ctx, db.NewestResponseTimeParams{WorkspaceID: post.WorkspaceID, PostID: postID, ChannelID: channelID}); err != nil {
		return err
	} else if t, perr := time.Parse(timeFormat, newest); perr == nil {
		since = t.Add(-time.Hour)
	}
	pctx, cancel := context.WithTimeout(s.publishingCtx(ctx, post.WorkspaceID), 30*time.Second)
	got, err := reader.Responses(pctx, creds, target.ExternalID, since)
	cancel()
	wctx, wcancel := context.WithTimeout(context.WithoutCancel(ctx), jobWriteTimeout)
	defer wcancel()
	switch {
	case errors.Is(err, providers.ErrReauth):
		_ = s.channelNeedsReconnect(wctx, ch, err)
		return nil
	case errors.Is(err, providers.ErrRejected):
		s.logger(ctx).Info("responses: not read", "post_id", postID, "channel_id", channelID, "provider", ch.Provider, "err", err)
		return s.noteResponsesFailed(wctx, post.WorkspaceID, postID, channelID, readFailure(e.Name(), err))
	case err != nil:
		return err
	}

	known, err := s.Q.ListResponses(ctx, db.ListResponsesParams{WorkspaceID: post.WorkspaceID, PostID: postID})
	if err != nil {
		return err
	}
	have := map[string]db.PostResponse{}
	for _, r := range known {
		if r.ChannelID == channelID {
			have[r.ID] = r
		}
	}
	own, now, fresh := ownResponseIDs(target), s.now(), 0
	if ins, err := s.Q.ListResponseInsights(ctx, db.ListResponseInsightsParams{WorkspaceID: post.WorkspaceID, PostID: postID}); err != nil {
		return err
	} else {
		for _, i := range ins {
			if i.Status == "waiting_credits" { // credits are back (or not): one more try per read, no new comment needed
				fresh++
			}
		}
	}
	err = s.tx(wctx, func(q *db.Queries) error {
		changed := 0 // new comments, or ones whose text or likes moved: the comments on screen are out of date
		for _, r := range got {
			if r.Own || r.ID == "" || own[r.ID] {
				continue
			}
			old, seen := have[r.ID]
			if !seen {
				fresh++
			}
			if !seen || old.Text != r.Text || old.Likes.Valid != (r.Likes != nil) || old.Likes.Int64 != derefInt(r.Likes) {
				changed++
			}
			if err := q.UpsertResponse(wctx, db.UpsertResponseParams{WorkspaceID: post.WorkspaceID, PostID: postID, ChannelID: channelID, ID: r.ID,
				ParentID: r.ParentID, Author: r.Author, AuthorHandle: r.AuthorHandle, Text: r.Text, Url: r.URL, Likes: sql.NullInt64{Int64: derefInt(r.Likes), Valid: r.Likes != nil},
				CreatedAt: formatTime(r.CreatedAt), FetchedAt: now}); err != nil {
				return err
			}
		}
		if changed > 0 {
			if _, err := s.emitRef(wctx, q, responsesActor(post.WorkspaceID), "post.responses_updated",
				Ref{Collection: post.CollectionID, Post: postID, Channel: channelID}, map[string]any{"channel_id": channelID, "changed": changed}); err != nil {
				return err
			}
		}
		if fresh == 0 {
			return nil
		}
		return s.debounce(wctx, q, post.WorkspaceID, "analyze_responses", "analyze_responses:"+postID, map[string]any{"post_id": postID}, s.Now().Add(wait), 3)
	})
	return err
}

// noteResponsesFailed keeps why a channel's responses can't be read (the platform refused) in the insight row,
// leaving the counts and summary of an earlier analysis as they are.
func (s *Service) noteResponsesFailed(ctx context.Context, workspaceID, postID, channelID, msg string) error {
	row := db.UpsertResponseInsightParams{WorkspaceID: workspaceID, PostID: postID, ChannelID: channelID, Themes: "[]", Questions: "[]"}
	rows, err := s.Q.ListResponseInsights(ctx, db.ListResponseInsightsParams{WorkspaceID: workspaceID, PostID: postID})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.ChannelID == channelID {
			row = db.UpsertResponseInsightParams{WorkspaceID: workspaceID, PostID: postID, ChannelID: channelID, Positive: r.Positive, Neutral: r.Neutral,
				Negative: r.Negative, Summary: r.Summary, Themes: r.Themes, Questions: r.Questions, ResponsesSeen: r.ResponsesSeen}
		}
	}
	row.Status, row.Error, row.AnalyzedAt = "failed", truncateRunes(msg, 500), s.now()
	return s.W.UpsertResponseInsight(ctx, row)
}

func (s *Service) queueRead(ctx context.Context, workspaceID, postID, channelID string) error {
	return s.enqueue(ctx, s.W, workspaceID, "read_responses", "read_responses:"+postID+":"+channelID,
		map[string]any{"post_id": postID, "channel_id": channelID}, s.Now(), 3)
}

// queueRecentResponseReads: one worker tick, for every target published in the last 7 days a read of its comments
// and, when its numbers are older than checkEvery, a check_target (its numbers, and whether it was deleted).
func (s *Service) queueRecentResponseReads(ctx context.Context) error {
	rows, err := s.Q.ListResponseTargets(ctx, nullStr(formatTime(s.Now().Add(-responsesWindow))))
	if err != nil {
		return err
	}
	stale := formatTime(s.Now().Add(-checkEvery))
	for _, t := range rows {
		e, ok := providers.Get(t.Provider)
		if !ok {
			continue
		}
		if _, can := e.Provider.(providers.ResponseReader); can {
			if err := s.queueRead(ctx, t.WorkspaceID, t.PostID, t.ChannelID); err != nil {
				return err
			}
		}
		if r, _ := numbersReader(e); r != nil && (!t.MetricsAt.Valid || t.MetricsAt.String < stale) {
			if err := s.enqueue(ctx, s.W, t.WorkspaceID, "check_target", "check_target:"+t.PostID+":"+t.ChannelID,
				map[string]any{"post_id": t.PostID, "channel_id": t.ChannelID}, s.Now(), 3); err != nil {
				return err
			}
		}
	}
	return nil
}

func init() {
	RegisterWorker(BackgroundWorker{Name: "responses", Run: func(ctx context.Context, s *Service) {
		wait := 2 * time.Minute
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = responsesEvery
			if err := s.queueRecentResponseReads(ctx); err != nil && ctx.Err() == nil {
				s.logger(ctx).Warn("queueing response reads failed", "err", err)
			}
		}
	}})
}

func derefInt(n *int64) int64 {
	if n == nil {
		return 0
	}
	return *n
}
