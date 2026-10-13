package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"slices"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Shared helpers for deciding from chat (Telegram, Slack, Discord). Callers
// verify the platform's request authenticity first; these helpers verify the
// button payload signature, the approver allowlist, and then decide through
// RunAction (so rules, version guards, events and webhooks behave as usual).

// callbackSig binds a button payload to the channel (8-byte HMAC, base64url).
func callbackSig(key, payload string) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:8])
}

func verifyCallbackSig(key, payload, sig string) bool {
	return key != "" && subtle.ConstantTimeCompare([]byte(callbackSig(key, payload)), []byte(sig)) == 1
}

func approverAllowed(list []string, id string) bool {
	return id != "" && slices.Contains(list, id)
}

// chatDecide approves or rejects itemID in the channel's workspace and returns
// a short outcome line for the chat UI.
func (s *Service) chatDecide(ctx context.Context, ch *notifyChannel, actorID, actorName, itemID, action, feedback string) (Item, string, error) {
	p := Principal{WorkspaceID: ch.row.WorkspaceID, Kind: PrincipalSystem, ID: actorID, Name: actorName,
		Scopes: []string{ScopeItemsReview}}
	it, err := s.RunAction(ctx, p, itemID, action, ActionInput{Feedback: feedback})
	if err == nil {
		if action == "approve" {
			return it, "✅ Approved by " + actorDisplay(actorName), nil
		}
		return it, "❌ Rejected by " + actorDisplay(actorName), nil
	}
	var de *Error
	if asError(err, &de) {
		switch {
		case de.Code == "conflict":
			if cur, gerr := s.Q.GetItem(ctx, db.GetItemParams{WorkspaceID: ch.row.WorkspaceID, ID: itemID}); gerr == nil {
				return Item{}, "Already " + cur.Status, err
			}
		case de.Code == "not_found":
			return Item{}, "This item no longer exists", err
		}
		return Item{}, de.Message, err
	}
	s.Log.Error("chat decision failed", "channel_id", ch.row.ID, "err", err)
	return Item{}, "Something went wrong; try the dashboard", err
}

func actorDisplay(name string) string {
	for _, pfx := range []string{"Telegram: ", "Slack: ", "Discord: "} {
		if len(name) > len(pfx) && name[:len(pfx)] == pfx {
			return name[len(pfx):]
		}
	}
	return name
}

// channelByID loads an enabled channel of the given type for a public
// integration endpoint (the caller must still verify the request).
func (s *Service) channelByID(ctx context.Context, id, typ string) (*notifyChannel, error) {
	if id == "" || len(id) > 64 {
		return nil, notFound("channel")
	}
	r, err := s.Q.GetNotifyChannelByID(ctx, id)
	if err != nil || r.Type != typ || r.Enabled != 1 {
		return nil, notFound("channel")
	}
	return s.loadChannel(r)
}
