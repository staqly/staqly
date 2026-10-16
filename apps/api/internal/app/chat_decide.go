package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"slices"
	"strconv"
	"strings"

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

// itemRef names an item in a button payload or link, bound to the version
// the message showed ("itm_x.7"); version 0 = unbound.
func itemRef(id string, version int64) string {
	if version <= 0 {
		return id
	}
	return id + "." + strconv.FormatInt(version, 10)
}

func parseItemRef(ref string) (string, int64) {
	id, v, ok := strings.Cut(ref, ".")
	if !ok {
		return ref, 0
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return id, n
}

// refMatches reports whether a payload contains a reference to itemID
// ("|itm_x|" or a versioned "|itm_x.7|").
func refMatches(payload, itemID string) bool {
	return strings.Contains(payload, "|"+itemID+"|") || strings.Contains(payload, "|"+itemID+".")
}

var errItemChanged = &Error{Code: "item_changed", Status: 409, Message: "item changed since this message — open it in staqly"}

// decideBound runs approve/reject, refusing if a still-pending item changed
// after the message (or link) was sent: the reviewer decides exactly what
// they saw. Decided items fall through to RunAction's "already ..." conflict.
func (s *Service) decideBound(ctx context.Context, p Principal, itemID, action, feedback string, version int64) (Item, error) {
	// Links and chat buttons carry no API key, so the read-only guard of the
	// REST API and MCP does not see them.
	if err := s.CheckWritable(ctx, p.WorkspaceID); err != nil {
		return Item{}, err
	}
	in := ActionInput{Feedback: feedback}
	if version > 0 {
		if cur, err := s.Q.GetItem(ctx, db.GetItemParams{WorkspaceID: p.WorkspaceID, ID: itemID}); err == nil && cur.Status == "pending" {
			if cur.Version != version {
				return Item{}, errItemChanged
			}
			in.ExpectedVersion = &version
		}
	}
	return s.RunAction(ctx, p, itemID, action, in)
}

// chatDecide approves or rejects itemID in the channel's workspace and returns
// a short outcome line for the chat UI. version binds the decision to the
// item version shown in the message (0 = unbound).
func (s *Service) chatDecide(ctx context.Context, ch *notifyChannel, actorID, actorName, itemID, action, feedback string, version int64) (Item, string, error) {
	p := Principal{WorkspaceID: ch.row.WorkspaceID, Kind: PrincipalSystem, ID: actorID, Name: actorName,
		Scopes: []string{ScopeItemsReview}}
	it, err := s.decideBound(ctx, p, itemID, action, feedback, version)
	if err == nil {
		if action == "approve" {
			return it, "✅ Approved by " + actorDisplay(actorName), nil
		}
		return it, "❌ Rejected by " + actorDisplay(actorName), nil
	}
	var de *Error
	if asError(err, &de) {
		switch {
		case de.Code == "item_changed":
			return Item{}, "⚠️ " + de.Message, err
		case de.Code == "conflict":
			if cur, gerr := s.Q.GetItem(ctx, db.GetItemParams{WorkspaceID: ch.row.WorkspaceID, ID: itemID}); gerr == nil {
				return Item{}, "Already " + cur.Status, err
			}
		case de.Code == "not_found":
			return Item{}, "This item no longer exists", err
		}
		return Item{}, de.Message, err
	}
	s.logger(ctx).Error("chat decision failed", "workspace_id", ch.row.WorkspaceID, "channel_id", ch.row.ID, "item_id", itemID, "err", err)
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
