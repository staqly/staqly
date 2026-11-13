package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Kick's webhooks: once a channel connects, staqly subscribes it to providers.KickEvents (sync_events, again daily
// as Kick drops a subscription whose webhook kept failing for a day). Kick sends every event to the webhook URL set
// on the app, the API's /v1/kick/webhook, signed with Kick's key. A chat reply to a post's message becomes a comment
// under the post; follows and subscriptions count in the analytics' Audience (channel_activity); the stream going
// live publishes the posts waiting for it (publish_on.go).
//
// An event for a channel staqly no longer has answers 410: Kick drops that subscription after a day of those.

// kickEventMaxAge: an older call is refused; a replay inside it is caught by its id (inbound_events, kept longer).
const (
	kickEventMaxAge     = 3 * 24 * time.Hour
	inboundEventsKeep   = 7 * 24 * time.Hour
	eventSubscribeEvery = 24 * time.Hour
)

// KickWebhookEnabled: the server has Kick's app; without it the endpoint answers 404.
func (s *Service) KickWebhookEnabled() bool {
	return s.Env("PROVIDER_KICK_CLIENT_ID", "") != ""
}

// KickCall is one webhook call's headers.
type KickCall struct {
	MessageID, Timestamp, Signature, Type string
}

var errKickChannelGone = &Error{Code: "channel_gone", Status: 410, Message: "staqly has no channel for this Kick account"}

// HandleKickWebhook checks the signature and acts on the event. It returns how many posts or channels it touched.
func (s *Service) HandleKickWebhook(ctx context.Context, call KickCall, body []byte) (int, error) {
	if !providers.KickSigned(ctx, call.MessageID, call.Timestamp, body, call.Signature) {
		return 0, forbidden("the signature doesn't match Kick's key")
	}
	at, err := time.Parse(time.RFC3339, call.Timestamp)
	if err != nil || s.Now().Sub(at) > kickEventMaxAge {
		return 0, forbidden("the event is too old")
	}
	var ev struct {
		Broadcaster providers.KickUser   `json:"broadcaster"`
		Giftees     []providers.KickUser `json:"giftees"`
	}
	var channels []db.Channel
	if json.Unmarshal(body, &ev) != nil || ev.Broadcaster.ChannelSlug == "" {
		return 0, nil
	}
	channels, err = s.Q.ChannelsByProfileURL(ctx, db.ChannelsByProfileURLParams{Provider: "kick",
		ProfileUrl: "https://kick.com/" + strings.ToLower(ev.Broadcaster.ChannelSlug)})
	if err != nil {
		return 0, err
	}
	if len(channels) == 0 {
		return 0, errKickChannelGone
	}
	n := 0
	err = s.tx(ctx, func(q *db.Queries) error {
		fresh, err := q.MarkInboundEvent(ctx, db.MarkInboundEventParams{Provider: "kick", ID: call.MessageID, ReceivedAt: s.now()})
		if err != nil || fresh == 0 {
			return err
		}
		switch call.Type {
		case "chat.message.sent":
			var m providers.KickChat
			if json.Unmarshal(body, &m) != nil {
				return nil
			}
			n, err = s.kickReply(ctx, q, channels, m)
			return err
		case "livestream.status.updated":
			var live struct {
				IsLive bool `json:"is_live"`
			}
			if json.Unmarshal(body, &live) != nil || !live.IsLive {
				return nil
			}
			for _, ch := range channels {
				started, err := s.channelWentLive(ctx, q, ch.ID)
				if err != nil {
					return err
				}
				n += started
			}
			return nil
		}
		if kind, count := kickActivity(call.Type, len(ev.Giftees)); kind != "" {
			for _, ch := range channels {
				if err := q.AddChannelActivity(ctx, db.AddChannelActivityParams{WorkspaceID: ch.WorkspaceID, ChannelID: ch.ID,
					Kind: kind, Count: count, At: formatTime(at)}); err != nil {
					return err
				}
				n++
			}
		}
		return nil
	})
	return n, err
}

// kickActivity is what a Kick event adds to the channel's audience numbers: the kind and how many.
func kickActivity(event string, giftees int) (string, int64) {
	switch event {
	case "channel.followed":
		return "follow", 1
	case "channel.subscription.new":
		return "subscription", 1
	case "channel.subscription.renewal":
		return "renewal", 1
	case "channel.subscription.gifts":
		return "gift", int64(max(giftees, 1))
	}
	return "", 0
}

// kickReply stores a chat reply to a post's message as a comment under the post. The channel's own messages (the
// post's thread goes out as replies to it) are not comments.
func (s *Service) kickReply(ctx context.Context, q *db.Queries, channels []db.Channel, m providers.KickChat) (int, error) {
	if m.RepliesTo == nil || m.RepliesTo.MessageID == "" || m.MessageID == "" || m.Sender.UserID == m.Broadcaster.UserID {
		return 0, nil
	}
	created := m.CreatedAt
	if created.IsZero() {
		created = s.Now()
	}
	n := 0
	for _, ch := range channels {
		targets, err := q.TargetsByExternalID(ctx, db.TargetsByExternalIDParams{ChannelID: ch.ID, ExternalID: m.RepliesTo.MessageID})
		if err != nil {
			return n, err
		}
		for _, t := range targets {
			if ownResponseIDs(db.PostTarget{Replies: t.Replies})[m.MessageID] {
				continue
			}
			handle := m.Sender.ChannelSlug
			if handle == "" {
				handle = strings.ToLower(m.Sender.Username)
			}
			if err := q.UpsertResponse(ctx, db.UpsertResponseParams{WorkspaceID: t.WorkspaceID, PostID: t.PostID, ChannelID: t.ChannelID,
				ID: m.MessageID, Author: m.Sender.Username, AuthorHandle: "@" + handle, Text: providers.KickText(m.Content),
				Url: ch.ProfileUrl, Likes: sql.NullInt64{}, CreatedAt: formatTime(created), FetchedAt: s.now()}); err != nil {
				return n, err
			}
			if err := s.debounce(ctx, q, t.WorkspaceID, "analyze_responses", "analyze_responses:"+t.PostID,
				map[string]any{"post_id": t.PostID}, s.Now().Add(responsesAnalyzeWait), 3); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

// queueEventSync subscribes a channel that just connected (or reconnected) to its platform's events.
func (s *Service) queueEventSync(ctx context.Context, row db.Channel) {
	e, ok := providers.Get(row.Provider)
	if !ok {
		return
	}
	if _, subscribes := e.Provider.(providers.EventSubscriber); subscribes {
		_ = s.enqueue(ctx, s.W, row.WorkspaceID, "sync_events", "sync_events:"+row.ID, map[string]any{"channel_id": row.ID}, s.Now(), 3)
	}
}

// syncEvents is the sync_events job: the channel's subscriptions to its platform's events.
func (s *Service) syncEvents(ctx context.Context, channelID string) error {
	ch, err := s.Q.GetChannelByID(ctx, channelID)
	if isNoRows(err) || (err == nil && (ch.Status != "connected" || ch.DeletedAt.Valid)) {
		return nil
	}
	if err != nil {
		return err
	}
	e, ok := providers.Get(ch.Provider)
	sub, subscribes := e.Provider.(providers.EventSubscriber)
	if !ok || !subscribes {
		return nil
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
	pctx, cancel := context.WithTimeout(s.publishingCtx(ctx, ch.WorkspaceID), 30*time.Second)
	hookURL := ""
	if base := strings.TrimRight(s.Cfg.PublicURL, "/"); base != "" {
		hookURL = base + "/v1/" + strings.TrimSuffix(ch.Provider, "_key") + "/webhook/" + ch.ID // Whop's webhook is made per channel
	}
	updated, err := sub.SubscribeEvents(pctx, creds, hookURL)
	cancel()
	if err == nil && updated != nil {
		// The platform made a webhook whose secret it shows once: kept sealed with the channel.
		sealed, serr := s.seal(updated)
		if serr != nil {
			return serr
		}
		wctx, wcancel := context.WithTimeout(context.WithoutCancel(ctx), jobWriteTimeout)
		defer wcancel()
		return s.W.SetChannelCredentials(wctx, db.SetChannelCredentialsParams{Credentials: sealed, UpdatedAt: s.now(), ID: ch.ID})
	}
	switch {
	case errors.Is(err, providers.ErrReauth):
		wctx, wcancel := context.WithTimeout(context.WithoutCancel(ctx), jobWriteTimeout)
		defer wcancel()
		return s.channelNeedsReconnect(wctx, ch, err)
	case errors.Is(err, providers.ErrRejected):
		// Publishing still works; the daily run tries again, and a reconnect grants what was missing.
		s.logger(ctx).Info("events: not subscribed", "channel_id", ch.ID, "provider", ch.Provider, "err", err)
		return nil
	}
	return err
}

// queueEventSyncs subscribes every connected channel of the platforms with events again, and forgets handled
// webhook calls older than a week.
func (s *Service) queueEventSyncs(ctx context.Context) error {
	if err := s.W.PruneInboundEvents(ctx, formatTime(s.Now().Add(-inboundEventsKeep))); err != nil {
		return err
	}
	for _, e := range providers.All() {
		if _, subscribes := e.Provider.(providers.EventSubscriber); !subscribes {
			continue
		}
		rows, err := s.Q.ConnectedChannelsOfProvider(ctx, e.ID())
		if err != nil {
			return err
		}
		for _, r := range rows {
			if err := s.enqueue(ctx, s.W, r.WorkspaceID, "sync_events", "sync_events:"+r.ID, map[string]any{"channel_id": r.ID}, s.Now(), 3); err != nil {
				return err
			}
		}
	}
	return nil
}

func init() {
	RegisterWorker(BackgroundWorker{Name: "events", Run: func(ctx context.Context, s *Service) {
		wait := 5 * time.Minute
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = eventSubscribeEvery
			if err := s.queueEventSyncs(ctx); err != nil && ctx.Err() == nil {
				s.logger(ctx).Warn("queueing event subscriptions failed", "err", err)
			}
		}
	}})
}
