package providers

import (
	"errors"
	"strings"
	"testing"
)

// What a platform's refusal becomes decides what staqly does next: "reauth" marks the channel as needing a
// reconnect and fails the target, "rejected" fails the target at once, "gone" says the post is not there any
// more, and "retry" (no kind) lets the job try again with backoff. A refusal in the wrong class either retries a
// post that can never go out or gives up on one that would have.
func errorClass(err error) string {
	switch {
	case errors.Is(err, ErrReauth):
		return "reauth"
	case errors.Is(err, ErrGone): // a rejection too: asked first
		return "gone"
	case errors.Is(err, ErrRejected):
		return "rejected"
	}
	return "retry"
}

func TestPlatformRefusalsFallInTheRightClass(t *testing.T) {
	x := func(status int, body string) error { return xError(status, body) }
	tiktok := func(status int, body string) error { return tiktokError(status, []byte(body)) }
	reddit := func(status int, body string) error { return redditError(status, []byte(body)) }
	pinterest := func(status int, body string) error { return pinterestError(status, []byte(body)) }
	for _, c := range []struct {
		name   string
		mapper func(int, string) error
		status int
		body   string
		class  string
		says   string // part of the message a person reads
	}{
		{"x: sign-in refused", x, 401, `{"title":"Unauthorized"}`, "reauth", "reconnect"},
		{"x: refresh token used up", x, 400, `{"error":"invalid_grant"}`, "reauth", "reconnect"},
		{"x: wrong kind of token", x, 403, `{"title":"Unsupported Authentication"}`, "reauth", "reconnect"},
		{"x: rate limit", x, 429, `{"title":"Too Many Requests"}`, "retry", "rate limit"},
		{"x: down", x, 503, `Service Unavailable`, "retry", "unavailable"},
		{"x: duplicate", x, 403, `{"detail":"You are not allowed to create a Tweet with duplicate content."}`, "rejected", ""},
		{"x: monthly cap", x, 429 - 26, `{"type":"https://api.twitter.com/2/problems/usage-capped"}`, "rejected", "monthly X API cap"},
		{"x: suspended account", x, 403, `{"type":"user-suspended"}`, "rejected", "suspended"},
		{"x: locked account", x, 403, `{"detail":"Your account is temporarily locked."}`, "rejected", "locked"},
		{"x: bad media", x, 400, `{"detail":"Your media IDs are invalid."}`, "rejected", "media"},
		{"x: forbidden", x, 403, `{"detail":"You are not permitted to perform this action."}`, "rejected", "not permitted"},
		{"x: bad request", x, 400, `{"detail":"text is too long"}`, "rejected", "text is too long"},
		{"x: unknown answer", x, 418, `{"detail":"teapot"}`, "retry", "teapot"},

		{"tiktok: sign-in refused", tiktok, 401, `{"error":{"code":"access_token_invalid","message":"expired"}}`, "reauth", "reconnect"},
		{"tiktok: 401 without a code", tiktok, 401, ``, "reauth", "reconnect"},
		{"tiktok: posting not allowed", tiktok, 401, `{"error":{"code":"scope_not_authorized"}}`, "reauth", "allow"},
		{"tiktok: rate limit", tiktok, 429, `{"error":{"code":"rate_limit_exceeded"}}`, "retry", "rate limit"},
		{"tiktok: down", tiktok, 500, `{"error":{"code":"internal_error","message":"oops"}}`, "retry", "unavailable"},
		{"tiktok: unaudited app", tiktok, 403, `{"error":{"code":"unaudited_client_can_only_post_to_private_accounts"}}`, "rejected", "Only you"},
		{"tiktok: daily post limit", tiktok, 403, `{"error":{"code":"spam_risk_too_many_posts"}}`, "rejected", "daily post limit"},
		{"tiktok: banned from posting", tiktok, 403, `{"error":{"code":"spam_risk_user_banned_from_posting"}}`, "rejected", "right now"},
		{"tiktok: other spam flag", tiktok, 403, `{"error":{"code":"spam_risk_text","message":"too many hashtags"}}`, "rejected", "too many hashtags"},
		{"tiktok: bad parameters", tiktok, 400, `{"error":{"code":"invalid_params","message":"title too long"}}`, "rejected", "title too long"},
		{"tiktok: token endpoint error", tiktok, 400, `{"error":"invalid_request"}`, "rejected", "invalid_request"},

		{"reddit: sign-in refused", reddit, 401, `{"message":"Unauthorized"}`, "reauth", "reconnect"},
		{"reddit: rate limit", reddit, 429, `{"message":"Too Many Requests"}`, "retry", "rate limit"},
		{"reddit: down", reddit, 502, `<html>Bad gateway</html>`, "retry", "unavailable (502)"},
		{"reddit: private subreddit", reddit, 403, `{"reason":"private","message":"Forbidden"}`, "rejected", "private"},
		{"reddit: no such subreddit", reddit, 404, `{"message":"Not Found"}`, "rejected", "check the subreddit"},
		{"reddit: an HTML error page is not shown", reddit, 400, `<html><body>Bad</body></html>`, "rejected", "Bad Request"},
		{"reddit: other refusal", reddit, 400, `{"message":"SUBREDDIT_NOTALLOWED"}`, "rejected", "SUBREDDIT_NOTALLOWED"},

		{"pinterest: sign-in refused", pinterest, 401, `{"code":2,"message":"Authentication failed."}`, "reauth", "reconnect"},
		{"pinterest: rate limit", pinterest, 429, `{"message":"slow down"}`, "retry", "rate limit"},
		{"pinterest: down", pinterest, 500, `{"message":"oops"}`, "retry", "unavailable"},
		{"pinterest: their fault with a 4xx", pinterest, 400, `{"message":"Something went wrong on our end. Sorry about that."}`, "retry", "unavailable"},
		{"pinterest: app on trial access", pinterest, 403, `{"message":"Your application consumer type must be approved"}`, "rejected", "Trial access"},
		{"pinterest: board removed", pinterest, 404, `{"message":"Board not found."}`, "rejected", "board"},
		{"pinterest: may not save to the board", pinterest, 403, `{"message":"Not authorized"}`, "rejected", "Not authorized"},
		{"pinterest: the pin is gone", pinterest, 404, `{"message":"Pin not found."}`, "gone", "not there any more"},
		{"pinterest: other refusal", pinterest, 400, `{"error":"Invalid image"}`, "rejected", "Invalid image"},
	} {
		err := c.mapper(c.status, c.body)
		if got := errorClass(err); got != c.class {
			t.Errorf("%s (%d %s): %s (%v), want %s", c.name, c.status, c.body, got, err, c.class)
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: the message %q does not say %q", c.name, err.Error(), c.says)
		}
		if strings.Contains(err.Error(), "<html") {
			t.Errorf("%s: the message shows the platform's HTML: %q", c.name, err.Error())
		}
	}
}
