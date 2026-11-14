package app

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Whop's webhooks: each connected Whop channel has its own webhook on Whop (sync_events makes it and keeps its signing
// secret sealed with the channel), pointed at the API's /v1/whop/webhook/<channel id>. Whop signs every call with that
// secret (Standard Webhooks). Someone joining the business (member.created) and a membership starting
// (membership.activated) count in the analytics' Audience as members and memberships. Whop sends nothing for forum
// posts, so their numbers are read from the API.

// whopEventMaxAge: Standard Webhooks' window; a replay inside it is caught by its id (inbound_events).
const whopEventMaxAge = 5 * time.Minute

// WhopCall is one webhook call's headers.
type WhopCall struct {
	ID, Timestamp, Signature string
}

// HandleWhopWebhook checks the call against the channel's secret and counts the event. It returns how many channels
// it touched. A channel staqly no longer has answers 410, so Whop stops sending after its retries.
func (s *Service) HandleWhopWebhook(ctx context.Context, channelID string, call WhopCall, body []byte) (int, error) {
	ch, err := s.Q.GetChannelByID(ctx, channelID)
	if isNoRows(err) || (err == nil && ((ch.Provider != "whop" && ch.Provider != "whop_key") || ch.DeletedAt.Valid)) {
		return 0, &Error{Code: "channel_gone", Status: 410, Message: "staqly has no Whop channel here"}
	}
	if err != nil {
		return 0, err
	}
	var creds providers.Credentials
	if err := s.unseal(ch.Credentials, &creds); err != nil {
		return 0, forbidden("the channel's webhook secret can't be read")
	}
	if !providers.WhopSigned(creds["webhook_secret"], call.ID, call.Timestamp, body, call.Signature) {
		return 0, forbidden("the signature doesn't match this channel's Whop webhook")
	}
	sec, err := strconv.ParseInt(call.Timestamp, 10, 64)
	at := time.Unix(sec, 0)
	if err != nil || s.Now().Sub(at).Abs() > whopEventMaxAge {
		return 0, forbidden("the event's time is too far from now")
	}
	var ev struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(body, &ev) != nil {
		return 0, nil
	}
	kind := map[string]string{"member.created": "follow", "membership.activated": "subscription"}[ev.Type]
	n := 0
	err = s.tx(ctx, func(q *db.Queries) error {
		fresh, err := q.MarkInboundEvent(ctx, db.MarkInboundEventParams{Provider: "whop", ID: call.ID, ReceivedAt: s.now()})
		if err != nil || fresh == 0 || kind == "" {
			return err
		}
		n = 1
		return q.AddChannelActivity(ctx, db.AddChannelActivityParams{WorkspaceID: ch.WorkspaceID, ChannelID: ch.ID, Kind: kind, Count: 1, At: formatTime(at)})
	})
	return n, err
}
