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
	responsesAnalyzeWait = 2 * time.Minute // debounce: more reads inside it move the analysis out again
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

// readResponses reads the responses under one published target, stores them (the sentiment of a known one stays)
// and queues the analysis when something new arrived. The provider is called outside any transaction.
func (s *Service) readResponses(ctx context.Context, postID, channelID string) error {
	post, err := s.Q.GetPostByID(ctx, postID)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	target, err := s.Q.GetTarget(ctx, db.GetTargetParams{PostID: postID, ChannelID: channelID})
	if isNoRows(err) || (err == nil && (target.Status != "published" || target.Removal != "" || target.ExternalID == "")) {
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
	var creds providers.Credentials
	if err := s.unseal(ch.Credentials, &creds); err != nil {
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
	have := map[string]bool{}
	for _, r := range known {
		if r.ChannelID == channelID {
			have[r.ID] = true
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
		for _, r := range got {
			if r.Own || r.ID == "" || own[r.ID] {
				continue
			}
			if !have[r.ID] {
				fresh++
			}
			if err := q.UpsertResponse(wctx, db.UpsertResponseParams{WorkspaceID: post.WorkspaceID, PostID: postID, ChannelID: channelID, ID: r.ID,
				ParentID: r.ParentID, Author: r.Author, AuthorHandle: r.AuthorHandle, Text: r.Text, Url: r.URL, Likes: sql.NullInt64{Int64: derefInt(r.Likes), Valid: r.Likes != nil},
				CreatedAt: formatTime(r.CreatedAt), FetchedAt: now}); err != nil {
				return err
			}
		}
		if fresh == 0 {
			return nil
		}
		return s.enqueue(wctx, q, post.WorkspaceID, "analyze_responses", "analyze_responses:"+postID, map[string]any{"post_id": postID}, s.Now().Add(responsesAnalyzeWait), 3)
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

// queueResponseReads queues a read for every published, still online target of the post (the refresh button).
func (s *Service) queueResponseReads(ctx context.Context, workspaceID, postID string) error {
	rows, err := s.Q.ListTargets(ctx, postID)
	if err != nil {
		return err
	}
	for _, t := range rows {
		if t.Status == "published" && t.Removal == "" && t.ChannelStatus == "connected" {
			if err := s.queueRead(ctx, workspaceID, postID, t.ChannelID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) queueRead(ctx context.Context, workspaceID, postID, channelID string) error {
	return s.enqueue(ctx, s.W, workspaceID, "read_responses", "read_responses:"+postID+":"+channelID,
		map[string]any{"post_id": postID, "channel_id": channelID}, s.Now(), 3)
}

// queueRecentResponseReads: one worker tick, a read for every target published in the last 7 days.
func (s *Service) queueRecentResponseReads(ctx context.Context) error {
	rows, err := s.Q.ListResponseTargets(ctx, nullStr(formatTime(s.Now().Add(-responsesWindow))))
	if err != nil {
		return err
	}
	for _, t := range rows {
		if e, ok := providers.Get(t.Provider); !ok {
			continue
		} else if _, can := e.Provider.(providers.ResponseReader); !can {
			continue
		}
		if err := s.queueRead(ctx, t.WorkspaceID, t.PostID, t.ChannelID); err != nil {
			return err
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
