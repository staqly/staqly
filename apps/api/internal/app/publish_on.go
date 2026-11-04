package app

import (
	"context"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// A scheduled post can wait for an event instead of a time (publish_on): today "live", the next start of a
// channel's stream on a platform that reports it (providers.LiveNotifier). It goes out once, through the same
// publish_post job a time would queue, when the platform's receiver calls channelWentLive. Stored on the post as
// "live:<channel id>"; a scheduled post has publish_at or publish_on, never both.

// PublishOn is what a post waits for.
type PublishOn struct {
	Event     string `json:"event"`      // live
	ChannelID string `json:"channel_id"` // the channel whose stream start publishes the post
}

const publishOnLive = "live:"

func publishOnOf(stored string) *PublishOn {
	if id, ok := strings.CutPrefix(stored, publishOnLive); ok && id != "" {
		return &PublishOn{Event: "live", ChannelID: id}
	}
	return nil
}

// checkPublishOn validates what a post should wait for and returns its stored form.
func (s *Service) checkPublishOn(ctx context.Context, q *db.Queries, workspaceID string, on PublishOn) (string, error) {
	if on.Event != "live" {
		return "", invalid("publish_on", `event must be "live"`)
	}
	if on.ChannelID == "" {
		return "", invalid("publish_on", "channel_id is required")
	}
	ch, err := q.GetChannel(ctx, db.GetChannelParams{WorkspaceID: workspaceID, ID: on.ChannelID})
	if isNoRows(err) || (err == nil && ch.DeletedAt.Valid) {
		return "", invalid("publish_on", "no such channel in this workspace")
	}
	if err != nil {
		return "", err
	}
	e, ok := providers.Get(ch.Provider)
	if _, live := e.Provider.(providers.LiveNotifier); !ok || !live {
		return "", invalid("publish_on", e.Name()+" doesn't tell staqly when a stream starts")
	}
	return publishOnLive + ch.ID, nil
}

// waitFor records that a post now waits for its event (post.scheduled with publish_on); no job runs until it comes.
func (s *Service) waitFor(ctx context.Context, q *db.Queries, p Principal, workspaceID, postID, stored string) error {
	if err := s.cancelJobs(ctx, q, "publish_post:"+postID); err != nil {
		return err
	}
	_, err := s.emit(ctx, q, p, "post.scheduled", "", postID, s.postEventData(ctx, q, workspaceID, postID, map[string]any{"publish_on": publishOnOf(stored)}))
	return err
}

// channelWentLive publishes the posts waiting for this channel's stream: each gets its publish_post job now, which
// checks the post again inside its own write, so an event that comes twice publishes once. Called inside the
// receiver's transaction, after the event's id was recorded. It returns how many posts it started.
func (s *Service) channelWentLive(ctx context.Context, q *db.Queries, channelID string) (int, error) {
	waiting, err := q.PostsWaitingOn(ctx, publishOnLive+channelID)
	if err != nil {
		return 0, err
	}
	for _, w := range waiting {
		if err := s.enqueue(ctx, q, w.WorkspaceID, "publish_post", "publish_post:"+w.ID, map[string]any{"post_id": w.ID}, s.Now(), 0); err != nil {
			return 0, err
		}
	}
	return len(waiting), nil
}

// unwaitChannel takes the posts waiting for a removed channel's stream back to draft (post.unscheduled).
func (s *Service) unwaitChannel(ctx context.Context, q *db.Queries, p Principal, channelID string) error {
	waiting, err := q.PostsWaitingOn(ctx, publishOnLive+channelID)
	if err != nil {
		return err
	}
	for _, w := range waiting {
		if err := q.SetPostStatus(ctx, db.SetPostStatusParams{Status: "draft", PublishedAt: nullStr(""), UpdatedAt: s.now(), ID: w.ID}); err != nil {
			return err
		}
		if _, err := s.emit(ctx, q, p, "post.unscheduled", w.CollectionID, w.ID, s.postEventData(ctx, q, w.WorkspaceID, w.ID, nil)); err != nil {
			return err
		}
	}
	return nil
}
