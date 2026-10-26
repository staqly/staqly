package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Reposts: a target's "reposters" setting (a channels field) names other connected channels of the same
// platform that repost the post once it is out. Each repost is its own job, so one account failing does not
// hold back the others; a repost that fails is logged and dropped, the post stays published.

// checkChannelSettings validates the channels fields of a target: every id is another channel of this
// workspace on the same platform.
func (s *Service) checkChannelSettings(ctx context.Context, q *db.Queries, workspaceID string, ch db.Channel, fields []providers.Field, settings map[string]string) error {
	for _, f := range fields {
		if f.Kind != "channels" {
			continue
		}
		ids := providers.ListValues(settings[f.Key])
		if len(ids) == 0 {
			continue
		}
		key := "targets." + ch.ID + ".settings." + f.Key
		rows, err := q.ChannelsByIDs(ctx, db.ChannelsByIDsParams{WorkspaceID: workspaceID, Ids: mustJSON(ids)})
		if err != nil {
			return err
		}
		found := map[string]db.Channel{}
		for _, r := range rows {
			found[r.ID] = r
		}
		for _, id := range ids {
			other, ok := found[id]
			switch {
			case id == ch.ID:
				return invalid(key, "a channel cannot repost its own post")
			case !ok:
				return invalid(key, "unknown channel "+id)
			case other.Provider != ch.Provider:
				return invalid(key, other.Name+" is on another platform")
			}
		}
	}
	return nil
}

// enqueueReposts runs inside the transaction that records a target as published.
func (s *Service) enqueueReposts(ctx context.Context, q *db.Queries, workspaceID, postID, channelID, settingsRaw string) error {
	settings := providers.Settings{}
	_ = json.Unmarshal([]byte(settingsRaw), &settings)
	for _, by := range providers.ListValues(settings["reposters"]) {
		if err := s.enqueue(ctx, q, workspaceID, "repost", "repost:"+postID+":"+channelID+":"+by,
			map[string]any{"post_id": postID, "channel_id": channelID, "repost_by": by}, s.Now(), 0); err != nil {
			return err
		}
	}
	return nil
}

// runRepost is the repost job: the channel repostBy reposts what channelID published for postID.
func (s *Service) runRepost(ctx context.Context, postID, channelID, repostBy string) error {
	target, err := s.Q.GetTarget(ctx, db.GetTargetParams{PostID: postID, ChannelID: channelID})
	if isNoRows(err) || (err == nil && (target.Status != "published" || target.ExternalID == "")) {
		return nil // the post went away or never made it out
	}
	if err != nil {
		return err
	}
	ch, err := s.Q.GetChannelByID(ctx, repostBy)
	if isNoRows(err) {
		return nil // the reposting channel was removed
	}
	if err != nil {
		return err
	}
	e, ok := providers.Get(ch.Provider)
	reposter, can := e.Provider.(providers.Reposter)
	if !ok || !can || ch.Status != "connected" {
		s.logger(ctx).Info("repost skipped", "post", postID, "channel", repostBy, "status", ch.Status)
		return nil
	}
	var creds providers.Credentials
	if err := s.unseal(ch.Credentials, &creds); err != nil {
		return nil
	}
	if creds, err = s.refreshIfDue(ctx, ch, e, creds); err != nil {
		if errors.Is(err, providers.ErrReauth) {
			_ = s.channelNeedsReconnect(ctx, ch, err)
			return nil
		}
		return err
	}
	err = reposter.Repost(s.providerCtx(ctx), creds, target.ExternalID)
	switch {
	case errors.Is(err, providers.ErrReauth):
		_ = s.channelNeedsReconnect(ctx, ch, err)
		return nil
	case errors.Is(err, providers.ErrRejected):
		s.logger(ctx).Info("repost refused", "post", postID, "channel", repostBy, "err", err)
		return nil
	}
	return err // retried by the job queue
}
