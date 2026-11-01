package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// errUnpublished: a post taken down from its channels is not published, scheduled or retried again.
var errUnpublished = &Error{Code: "unpublished", Status: 409, Message: "this post was unpublished; duplicate it to post it again"}

// UnpublishPost takes a published post down from its channels (all published ones, or channelIDs). Channels whose
// platform can't delete through an API are marked "manual" at once; the others get a remove_target job.
func (s *Service) UnpublishPost(ctx context.Context, p Principal, postID string, channelIDs []string) (Post, error) {
	if err := p.require(ScopePostsPublish); err != nil {
		return Post{}, err
	}
	if channelIDs != nil && len(channelIDs) == 0 { // nil means every channel; an empty list must not
		return Post{}, invalid("channel_ids", "pick at least one channel")
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		row, err := q.GetPost(ctx, db.GetPostParams{WorkspaceID: p.WorkspaceID, ID: postID})
		if isNoRows(err) {
			return notFound("post")
		}
		if err != nil {
			return err
		}
		targets, err := q.ListTargets(ctx, postID)
		if err != nil {
			return err
		}
		var eligible []db.ListTargetsRow
		for _, t := range targets {
			if t.Status == "published" && (t.Removal == "" || t.Removal == "failed") {
				eligible = append(eligible, t)
			}
		}
		if len(eligible) == 0 {
			return &Error{Code: "not_published", Status: 409, Message: "this post has nothing published to take down"}
		}
		for _, id := range channelIDs {
			if !slices.ContainsFunc(targets, func(t db.ListTargetsRow) bool { return t.ChannelID == id }) {
				return invalid("channel_ids", id+" is not a channel of this post")
			}
		}
		var chosen []db.ListTargetsRow
		for _, t := range eligible {
			if channelIDs == nil || slices.Contains(channelIDs, t.ChannelID) {
				chosen = append(chosen, t)
			}
		}
		if len(chosen) == 0 {
			return conflict("these channels have nothing to take down (removed already, or being removed)")
		}
		now := s.now()
		by := p.Kind + ":" + p.ID
		if p.UserID != "" {
			by = "user:" + p.UserID
		}
		if !row.UnpublishedAt.Valid {
			if err := q.SetPostUnpublished(ctx, db.SetPostUnpublishedParams{UnpublishedAt: nullStr(now), UnpublishedBy: by, UpdatedAt: now,
				WorkspaceID: p.WorkspaceID, ID: postID}); err != nil {
				return err
			}
		}
		channels := []string{}
		for _, t := range chosen {
			channels = append(channels, t.ChannelID)
			set := db.SetTargetRemovalParams{Removal: "removing", UpdatedAt: now, PostID: postID, ChannelID: t.ChannelID}
			if e, ok := providers.Get(t.Provider); !ok || t.Provider == "custom" || !isRemover(e) {
				set.Removal, set.RemovalError = "manual", manualMessage(t.Provider)
			}
			if err := q.SetTargetRemoval(ctx, set); err != nil {
				return err
			}
			// A thread part still waiting would post under a post that is going away.
			for i := range targetThread(row.Thread, t.Settings) {
				if err := s.cancelJobs(ctx, q, fmt.Sprintf("publish_reply:%s:%s:%d", postID, t.ChannelID, i)); err != nil {
					return err
				}
			}
			if set.Removal == "removing" {
				if err := s.enqueue(ctx, q, p.WorkspaceID, "remove_target", "remove_target:"+postID+":"+t.ChannelID,
					map[string]any{"post_id": postID, "channel_id": t.ChannelID}, s.Now(), 3); err != nil {
					return err
				}
			}
		}
		_, err = s.emit(ctx, q, p, "post.unpublished", row.CollectionID, postID, s.postEventData(ctx, q, p.WorkspaceID, postID, map[string]any{"channels": channels}))
		return err
	})
	if err != nil {
		return Post{}, err
	}
	return s.getPost(ctx, p, postID)
}

func manualMessage(provider string) string {
	name := provider
	if e, ok := providers.Get(provider); ok {
		name = e.Name()
	}
	return "Delete it on " + name + " yourself: staqly can't take it down through its API."
}

func isRemover(e providers.Entry) bool {
	_, ok := e.Provider.(providers.Remover)
	return ok
}

// removeTarget is the remove_target job: it deletes the thread parts (last first), then the post, on one channel.
// A re-run skips what is settled and the parts already marked removed. The platform is called outside any tx.
func (s *Service) removeTarget(ctx context.Context, postID, channelID string) error {
	row, err := s.Q.GetPostByID(ctx, postID)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	target, err := s.Q.GetTarget(ctx, db.GetTargetParams{PostID: postID, ChannelID: channelID})
	if isNoRows(err) || (err == nil && (target.Removal != "removing" || target.Status != "published")) {
		return nil // settled meanwhile, or taken off
	}
	if err != nil {
		return err
	}
	ch, err := s.Q.GetChannelByID(ctx, channelID)
	if err != nil && !isNoRows(err) {
		return err
	}
	if err != nil || ch.WorkspaceID != row.WorkspaceID {
		return s.settleRemoval(ctx, postID, channelID, "manual", "The channel was removed from staqly: delete the post on the platform yourself.")
	}
	e, ok := providers.Get(ch.Provider)
	if !ok || !isRemover(e) {
		return s.settleRemoval(ctx, postID, channelID, "manual", manualMessage(ch.Provider))
	}
	if ch.Status == "disabled" || ch.Status == "needs_reconnect" {
		return s.settleRemoval(ctx, postID, channelID, "failed", "The channel is "+map[string]string{"disabled": "turned off", "needs_reconnect": "waiting to be reconnected"}[ch.Status]+"; fix that and try again.")
	}
	var creds providers.Credentials
	if err := s.unseal(ch.Credentials, &creds); err != nil {
		return s.settleRemoval(ctx, postID, channelID, "failed", "stored credentials cannot be read: "+err.Error())
	}
	if creds, err = s.refreshIfDue(ctx, ch, e, creds); err != nil {
		if errors.Is(err, providers.ErrReauth) {
			return s.settleRemoval(ctx, postID, channelID, "failed", err.Error())
		}
		return err
	}
	remover := e.Provider.(providers.Remover)
	pctx := s.publishingCtx(ctx, row.WorkspaceID)
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobWriteTimeout)
	defer cancel()

	replies := repliesFor(len(targetThread(row.Thread, target.Settings)), target.Replies)
	fail := func(err error) error {
		switch {
		case errors.Is(err, providers.ErrManual):
			return s.settleRemoval(wctx, postID, channelID, "manual", err.Error())
		case errors.Is(err, providers.ErrReauth):
			_ = s.channelNeedsReconnect(wctx, ch, err)
			return s.settleRemoval(wctx, postID, channelID, "failed", err.Error())
		case errors.Is(err, providers.ErrRejected):
			return s.settleRemoval(wctx, postID, channelID, "failed", err.Error())
		}
		return err // the job retries with backoff; the last failure settles the target (jobFailed)
	}
	for i := len(replies) - 1; i >= 0; i-- {
		if replies[i].Status != "published" || replies[i].ExternalID == "" {
			continue
		}
		if err := remover.Remove(pctx, creds, replies[i].ExternalID); err != nil {
			return fail(err)
		}
		replies[i].Status = "removed"
		if err := s.W.SetTargetReplies(wctx, db.SetTargetRepliesParams{Replies: mustJSON(replies), UpdatedAt: s.now(), PostID: postID, ChannelID: channelID}); err != nil {
			return err
		}
	}
	if target.ExternalID != "" {
		if err := remover.Remove(pctx, creds, target.ExternalID); err != nil {
			return fail(err)
		}
	}
	return s.settleRemoval(wctx, postID, channelID, "removed", "")
}

// settleRemoval records how taking a target down ended (removed, manual or failed), once: a target that is not
// "removing" any more was settled by another run. Emits post.target_removed or post.target_remove_failed.
func (s *Service) settleRemoval(ctx context.Context, postID, channelID, removal, errMsg string) error {
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
		if isNoRows(err) || (err == nil && t.Removal != "removing") {
			return nil
		}
		if err != nil {
			return err
		}
		now := s.now()
		removedAt := sql.NullString{}
		typ, extra := "post.target_removed", map[string]any{"channel_id": channelID}
		if removal == "removed" {
			removedAt = nullStr(now)
		} else {
			typ, extra["error"] = "post.target_remove_failed", errMsg
			extra["removal"] = removal
		}
		if err := q.SetTargetRemoval(ctx, db.SetTargetRemovalParams{Removal: removal, RemovalError: truncateRunes(errMsg, 500), RemovedAt: removedAt,
			UpdatedAt: now, PostID: postID, ChannelID: channelID}); err != nil {
			return err
		}
		_, err = s.emitRef(ctx, q, sys, typ, Ref{Collection: row.CollectionID, Post: postID, Channel: channelID}, s.postEventData(ctx, q, row.WorkspaceID, postID, extra))
		return err
	})
}
