package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Meta's account callbacks (Instagram, Facebook, Threads): Meta POSTs a signed_request when someone removes the app
// from their account (deauthorize) and when they ask for their data to be deleted. Both name the person by their id,
// which staqly keeps only inside sealed credentials, so the channels are unsealed and compared.

// metaSigned request: "<signature>.<payload>", both base64url; the signature is HMAC-SHA256 of the payload text
// with one of the apps' secrets.
func (s *Service) metaSignedRequest(sr string) (string, error) {
	sig, payload, ok := strings.Cut(strings.TrimSpace(sr), ".")
	if !ok {
		return "", forbidden("not a signed request")
	}
	want, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(sig, "="))
	if err != nil {
		return "", forbidden("not a signed request")
	}
	signed := false
	for _, secret := range s.metaAppSecrets() {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(payload))
		signed = signed || hmac.Equal(want, mac.Sum(nil))
	}
	if !signed {
		return "", forbidden("the signature doesn't match any Meta app secret")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(payload, "="))
	if err != nil {
		return "", invalid("signed_request", "unreadable payload")
	}
	var p struct {
		UserID json.RawMessage `json:"user_id"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return "", invalid("signed_request", "unreadable payload")
	}
	id := strings.Trim(string(p.UserID), `"`)
	if id == "" || id == "null" {
		return "", invalid("signed_request", "no user_id")
	}
	return id, nil
}

// metaChannelsOf: the channels whose sign-in is this person's (Instagram's account or app-scoped id, the Facebook
// person who connected a Page, the Threads profile).
func (s *Service) metaChannelsOf(ctx context.Context, userID string) ([]db.Channel, error) {
	rows, err := s.Q.MetaChannels(ctx)
	if err != nil {
		return nil, err
	}
	var out []db.Channel
	for _, r := range rows {
		var c providers.Credentials
		if r.Credentials == "" || s.unseal(r.Credentials, &c) != nil {
			continue
		}
		if c["user_id"] == userID || c["app_user_id"] == userID || c["authorized_by"] == userID {
			out = append(out, r)
		}
	}
	return out, nil
}

var errMetaDeauthorized = errors.New("the account removed staqly's access on the platform; connect it again to publish")

// MetaDeauthorize: someone removed the app from their account: their channels need reconnecting (the tokens are dead).
func (s *Service) MetaDeauthorize(ctx context.Context, signedRequest string) (int, error) {
	id, err := s.metaSignedRequest(signedRequest)
	if err != nil {
		return 0, err
	}
	rows, err := s.metaChannelsOf(ctx, id)
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		if err := s.channelNeedsReconnect(ctx, r, errMetaDeauthorized); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

// MetaDataDeletion: someone asked Meta to have their data deleted: their channels lose the sign-in, picture and
// profile link, and the comments read under their posts; the posts (the workspace's own writing) stay. It returns the
// status page and the confirmation code Meta shows the person.
func (s *Service) MetaDataDeletion(ctx context.Context, signedRequest string) (string, string, error) {
	id, err := s.metaSignedRequest(signedRequest)
	if err != nil {
		return "", "", err
	}
	rows, err := s.metaChannelsOf(ctx, id)
	if err != nil {
		return "", "", err
	}
	code := strings.TrimPrefix(newID("del"), "del_")
	err = s.tx(ctx, func(q *db.Queries) error {
		for _, r := range rows {
			if err := q.WipeChannelAccount(ctx, db.WipeChannelAccountParams{LastError: "The account asked the platform to delete its data; connect it again to publish",
				UpdatedAt: s.now(), ID: r.ID}); err != nil {
				return err
			}
			if _, err := q.DeleteChannelResponses(ctx, r.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	if err := s.AdminAudit(ctx, Admin{Email: "meta"}, "meta_data_deletion", "", code, map[string]any{"channels": len(rows)}); err != nil {
		return "", "", err
	}
	return s.webURL() + "/api/v1/meta/data-deletion/" + code, code, nil
}

// MetaDataDeletionStatus: what the status page says for a confirmation code.
func (s *Service) MetaDataDeletionStatus(ctx context.Context, code string) (string, error) {
	n, err := s.Q.AdminAuditExists(ctx, db.AdminAuditExistsParams{Action: "meta_data_deletion", Target: code})
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", notFound("deletion request")
	}
	return fmt.Sprintf("Data deletion request %s: done. Staqly deleted the sign-in, profile picture and comments it had "+
		"from your account; connected channels need to be connected again to publish.", code), nil
}
