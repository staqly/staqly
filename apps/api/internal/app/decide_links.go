package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// One-tap decision links: random 256-bit tokens (only the SHA-256 is stored),
// single-use, expiring (NOTIFY_LINK_TTL, default 7 days). GET shows a
// confirmation page; only POST decides, so link-preview bots that fetch URLs
// can't approve anything.

func hashLinkToken(tok string) string {
	sum := sha256.Sum256([]byte("staqly-decide:" + tok))
	return hex.EncodeToString(sum[:])
}

func (s *Service) createDecisionLinks(ctx context.Context, ch *notifyChannel, itemID string, version int64) (approve, reject string, err error) {
	now := s.Now()
	exp := formatTime(now.Add(s.envDuration("NOTIFY_LINK_TTL", 7*24*time.Hour)))
	urls := [2]string{}
	err = s.tx(ctx, func(q *db.Queries) error {
		for i, action := range []string{"approve", "reject"} {
			tok := "dl_" + randomString(32)
			if err := q.InsertDecisionLink(ctx, db.InsertDecisionLinkParams{TokenHash: hashLinkToken(tok),
				WorkspaceID: ch.row.WorkspaceID, ItemID: itemID, ChannelID: nullStr(ch.row.ID), ChannelName: ch.row.Name,
				Action: action, ItemVersion: sql.NullInt64{Int64: version, Valid: version > 0}, ExpiresAt: exp, CreatedAt: formatTime(now)}); err != nil {
				return err
			}
			urls[i] = s.publicAPIBase() + "/v1/decide/" + tok
		}
		return nil
	})
	return urls[0], urls[1], err
}

// DecisionLinkView is what the confirmation page shows.
type DecisionLinkView struct {
	Action      string `json:"action"`
	ChannelName string `json:"channel_name"`
	ExpiresAt   string `json:"expires_at"`
	ItemID      string `json:"item_id"`
	Title       string `json:"title"`
	ItemType    string `json:"item_type"`
	Collection  string `json:"collection"`
	Preview     string `json:"preview"`
	Status      string `json:"status"`
	WebURL      string `json:"web_url"`
}

var errLinkGone = &Error{Code: "gone", Status: 410, Message: "This link has already been used or has expired."}

func (s *Service) activeLink(ctx context.Context, tok string) (db.DecisionLink, error) {
	if len(tok) < 20 || len(tok) > 100 {
		return db.DecisionLink{}, notFound("link")
	}
	l, err := s.Q.GetDecisionLink(ctx, hashLinkToken(tok))
	if isNoRows(err) {
		return l, notFound("link")
	}
	if err != nil {
		return l, err
	}
	if l.UsedAt.Valid || l.ExpiresAt <= s.now() {
		return l, errLinkGone
	}
	// The channel that sent the link was deleted or disabled: the link dies
	// with it (deleting the channel also deletes its links).
	if !l.ChannelID.Valid {
		return l, errLinkGone
	}
	if ch, err := s.Q.GetNotifyChannel(ctx, db.GetNotifyChannelParams{WorkspaceID: l.WorkspaceID, ID: l.ChannelID.String}); err != nil || ch.Enabled != 1 {
		return l, errLinkGone
	}
	return l, nil
}

// LookupDecisionLink validates a token without consuming it.
func (s *Service) LookupDecisionLink(ctx context.Context, tok string) (DecisionLinkView, error) {
	l, err := s.activeLink(ctx, tok)
	if err != nil {
		return DecisionLinkView{}, err
	}
	it, err := s.Q.GetItem(ctx, db.GetItemParams{WorkspaceID: l.WorkspaceID, ID: l.ItemID})
	if err != nil {
		return DecisionLinkView{}, notFound("item")
	}
	if l.ItemVersion.Valid && it.Status == "pending" && it.Version != l.ItemVersion.Int64 {
		return DecisionLinkView{}, errItemChanged
	}
	v := DecisionLinkView{Action: l.Action, ChannelName: l.ChannelName, ExpiresAt: l.ExpiresAt, ItemID: it.ID, Title: it.Title,
		ItemType: it.Type, Status: it.Status, Preview: itemPreview(s.toItem(ctx, it)), WebURL: s.webURL() + "/items/" + it.ID}
	if c, err := s.Q.GetCollection(ctx, db.GetCollectionParams{WorkspaceID: l.WorkspaceID, ID: it.CollectionID}); err == nil {
		v.Collection = c.Name
	}
	return v, nil
}

// UseDecisionLink consumes the token and runs the action through RunAction
// as "Link: <channel>". A decision that loses to another one (409) still
// consumes the link; other failures release it.
func (s *Service) UseDecisionLink(ctx context.Context, tok, feedback string) (Item, string, error) {
	l, err := s.activeLink(ctx, tok)
	if err != nil {
		return Item{}, "", err
	}
	n, err := s.W.UseDecisionLink(ctx, db.UseDecisionLinkParams{Now: nullStr(s.now()), TokenHash: l.TokenHash})
	if err != nil {
		return Item{}, "", err
	}
	if n == 0 {
		return Item{}, "", errLinkGone
	}
	p := Principal{WorkspaceID: l.WorkspaceID, Kind: PrincipalSystem, ID: "link:" + l.ChannelID.String,
		Name: "Link: " + l.ChannelName, Scopes: []string{ScopeItemsReview}}
	if l.Action != "reject" {
		feedback = ""
	}
	it, err := s.decideBound(ctx, p, l.ItemID, l.Action, feedback, l.ItemVersion.Int64)
	// Stale (item changed) and lost races stay consumed; other failures release.
	var de *Error
	if err != nil && !isConflict(err) && !(asError(err, &de) && de.Code == "item_changed") {
		_ = s.W.ReleaseDecisionLink(ctx, l.TokenHash)
	}
	return it, l.Action, err
}

// LookupItemStatus returns the current status of a link's item, even after
// the link was used (for "already decided" messages).
func (s *Service) LookupItemStatus(ctx context.Context, tok string) (string, error) {
	l, err := s.Q.GetDecisionLink(ctx, hashLinkToken(tok))
	if err != nil {
		return "", notFound("link")
	}
	it, err := s.Q.GetItem(ctx, db.GetItemParams{WorkspaceID: l.WorkspaceID, ID: l.ItemID})
	if err != nil {
		return "", notFound("item")
	}
	return it.Status, nil
}
