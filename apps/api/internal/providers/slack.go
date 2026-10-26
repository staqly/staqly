package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Slack over the Web API: the person installs the server's Slack app into a workspace (OAuth v2); the workspace is
// the staqly channel and each post names the Slack channel it goes to (the channel setting, picked from Options).
// Messages go out with chat.postMessage, files through files.getUploadURLExternal + files.completeUploadExternal, a
// post's thread as replies in the message's Slack thread. Bot tokens last until the app is removed, unless the app
// turned token rotation on: then a refresh token renews them every 12 hours. The same sign-in also gives a user token
// for the person who connected, so a post can go out as them instead of the app (the post_as setting). Channels
// connected before v0.6 hold one Slack channel (channel_id), used when a post names none.
type Slack struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	slackAPI       = "https://slack.com/api"
	slackAuthorize = "https://slack.com/oauth/v2/authorize"
	// slackPoll is how long to wait between checks for the message a file upload made; slackPolls how many.
	slackPoll  = time.Second
	slackPolls = 10
)

// slackScopes are bot scopes: post (chat:write), join public channels to post files there (channels:join), upload
// files and find the message they made (files:write, files:read), list the channels to pick from (channels:read,
// groups:read).
// team:read gives the workspace's icon; reactions:read and users:read the reactions under a post and who reacted.
const slackScopes = "chat:write,channels:join,channels:read,groups:read,files:write,files:read,team:read,reactions:read,users:read"

// slackUserScopes let a post go out as the person who connected: post, upload files and find their message.
// users.profile:read shows the person's photo next to "As myself".
const slackUserScopes = "chat:write,files:write,files:read,users.profile:read"

func init() {
	Register(Slack{NewBase("slack", "Slack", AuthOAuth2,
		Limits{MaxChars: 40000, MaxMedia: 10, MediaKinds: []string{"image", "video"}}).WithSettings(slackSettings...)}, StatusAvailable)
}

// slackSettings: per-post settings.
var slackSettings = []Field{
	{Key: "link_preview", Label: "Show link previews", Kind: "boolean", Default: "true",
		Help: "Slack unfurls links in the message into previews."},
	// remote, one value: the list carries the person's Slack photo; Options is what a client without it shows.
	{Key: "post_as", Label: "Post as", Kind: "remote", Default: "app", MaxItems: 1, Options: slackPostAs,
		Help: "As yourself, the message shows the name and photo of whoever connected Slack; they must be in the channel."},
	{Key: "channel", Label: "Slack channels", Kind: "remote", Required: true, Prefer: []string{"#random", "#general"},
		MaxItems: 10, MaxLength: 400, Help: "Public channels, and private ones after /invite @YourApp in them."},
}

var slackPostAs = []Option{{Value: "app", Label: "As the app", Icon: "staqly"}, {Value: "me", Label: "As myself"}}

// Slack calls back over https only.
func (Slack) Needs() Needs { return Needs{HTTPS: true} }

func (Slack) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{
		"client_id":    {app.ClientID},
		"scope":        {slackScopes},
		"user_scope":   {slackUserScopes},
		"redirect_uri": {redirect},
		"state":        {state},
	}
	return slackAuthorize + "?" + q.Encode(), nil
}

type slackToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	BotUserID    string `json:"bot_user_id"`
	AuthedUser   struct {
		ID           string `json:"id"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	} `json:"authed_user"`
	Team struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"team"`
}

func (t slackToken) into(c Credentials, now time.Time) Credentials {
	out := Credentials{}
	for k, v := range c {
		out[k] = v
	}
	out["access_token"] = t.AccessToken
	if t.RefreshToken != "" {
		out["refresh_token"] = t.RefreshToken
	}
	if t.ExpiresIn > 0 {
		out["expires_at"] = now.Add(time.Duration(t.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	if t.BotUserID != "" {
		out["bot_user_id"] = t.BotUserID
	}
	if t.Team.ID != "" {
		out["team_id"], out["team_name"] = t.Team.ID, t.Team.Name
	}
	if u := t.AuthedUser; u.AccessToken != "" {
		out["user_id"], out["user_token"], out["user_refresh_token"] = u.ID, u.AccessToken, u.RefreshToken
	}
	return out
}

func (Slack) token(ctx context.Context, app App, form url.Values) (slackToken, error) {
	var t slackToken
	form.Set("client_id", app.ClientID)
	form.Set("client_secret", app.ClientSecret)
	req, err := checkForm(ctx, slackAPI+"/oauth.v2.access", form)
	if err != nil {
		return t, err
	}
	if err := slackDo(ctx, req, &t); err != nil {
		return t, err
	}
	if t.AccessToken == "" {
		return t, errors.New("Slack returned no bot token")
	}
	return t, nil
}

func (s Slack) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	t, err := s.token(ctx, app, url.Values{"code": {code}, "redirect_uri": {redirect}})
	if err != nil {
		return nil, Profile{}, err
	}
	creds := t.into(Credentials{}, time.Now())
	who, err := s.authTest(ctx, creds["access_token"])
	if err != nil {
		return nil, Profile{}, err
	}
	creds["team_url"] = who.URL
	if who.Team != "" {
		creds["team_name"] = who.Team
	}
	// The workspace's address stays unique (two workspaces may share a name) and reads well: acme.slack.com.
	handle := strings.TrimSuffix(strings.TrimPrefix(who.URL, "https://"), "/")
	if handle == "" {
		handle = creds["team_id"]
	}
	return creds, Profile{Name: creds["team_name"], Handle: handle, ProfileURL: who.URL, AvatarURL: s.teamIcon(ctx, creds["access_token"])}, nil
}

// teamIcon is the workspace's own icon; "" for Slack's stand-in (the dashboard then draws the name's first letter,
// as Slack does) or when the sign-in can't read it (connected before team:read).
func (Slack) teamIcon(ctx context.Context, token string) string {
	var res struct {
		Team struct {
			Icon struct {
				Image132 string `json:"image_132"`
				Default  bool   `json:"image_default"`
			} `json:"icon"`
		} `json:"team"`
	}
	req, err := slackRequest(ctx, token, "team.info", nil)
	if err != nil || slackDo(ctx, req, &res) != nil || res.Team.Icon.Default {
		return ""
	}
	if u, err := url.Parse(res.Team.Icon.Image132); err == nil && u.Scheme == "https" {
		return u.String()
	}
	return ""
}

type slackWho struct {
	URL    string `json:"url"` // https://acme.slack.com/
	Team   string `json:"team"`
	TeamID string `json:"team_id"`
	UserID string `json:"user_id"`
}

func (Slack) authTest(ctx context.Context, token string) (slackWho, error) {
	var who slackWho
	req, err := slackRequest(ctx, token, "auth.test", nil)
	if err != nil {
		return who, err
	}
	return who, slackDo(ctx, req, &who)
}

// Refresh renews a rotating token; a token without rotation does not expire, so it is only checked.
func (s Slack) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	if c["refresh_token"] == "" {
		if _, err := s.authTest(ctx, c["access_token"]); err != nil {
			return nil, err
		}
		return c, nil
	}
	t, err := s.token(ctx, app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c["refresh_token"]}})
	if err != nil {
		return nil, err
	}
	out := t.into(c, time.Now())
	if c["user_refresh_token"] != "" {
		// Rotation covers the user token too. The bot's old refresh token is spent by now, so a failure here drops
		// only posting as the person (it asks to reconnect) instead of losing the bot token as well.
		u, err := s.token(ctx, app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c["user_refresh_token"]}})
		if err != nil {
			delete(out, "user_token")
			delete(out, "user_refresh_token")
		} else {
			out["user_token"], out["user_refresh_token"] = u.AccessToken, u.RefreshToken
		}
	}
	return out, nil
}

type slackChannel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsPrivate bool   `json:"is_private"`
	IsMember  bool   `json:"is_member"`
	IsArchive bool   `json:"is_archived"`
}

// Slack lists the workspace's channels for the channel setting.
var _ OptionLister = Slack{}

func (s Slack) Options(ctx context.Context, c Credentials, key string) ([]Option, error) {
	if key == "post_as" {
		return s.postAs(ctx, c), nil
	}
	if key != "channel" {
		return nil, nil
	}
	chans, err := s.channels(ctx, c["access_token"])
	if err != nil {
		return nil, err
	}
	out := make([]Option, 0, len(chans))
	for _, ch := range chans {
		out = append(out, Option{Value: ch.ID, Label: "#" + ch.Name})
	}
	return out, nil
}

// postAs is "As the app" and "As myself" with the person's Slack photo when the sign-in may read it (connected
// before users.profile:read, or posting as the person not set up: no photo).
func (Slack) postAs(ctx context.Context, c Credentials) []Option {
	out := append([]Option(nil), slackPostAs...)
	if c["user_token"] == "" {
		return out
	}
	var res struct {
		Profile struct {
			Image48 string `json:"image_48"`
		} `json:"profile"`
	}
	if req, err := slackRequest(ctx, c["user_token"], "users.profile.get", nil); err == nil && slackDo(ctx, req, &res) == nil {
		if u, err := url.Parse(res.Profile.Image48); err == nil && u.Scheme == "https" {
			out[1].Icon = u.String()
		}
	}
	return out
}

// channels lists the channels the app may post to: every public one and the private ones it was invited to.
func (Slack) channels(ctx context.Context, token string) ([]slackChannel, error) {
	var out []slackChannel
	cursor := ""
	for page := 0; page < 20; page++ {
		q := url.Values{"types": {"public_channel,private_channel"}, "exclude_archived": {"true"}, "limit": {"200"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var res struct {
			Channels []slackChannel `json:"channels"`
			Meta     struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		req, err := slackRequest(ctx, token, "conversations.list?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		if err := slackDo(ctx, req, &res); err != nil {
			return nil, err
		}
		for _, ch := range res.Channels {
			if !ch.IsArchive && (!ch.IsPrivate || ch.IsMember) {
				out = append(out, ch)
			}
		}
		if cursor = res.Meta.NextCursor; cursor == "" {
			break
		}
	}
	return out, nil
}

func slackChannelURL(teamURL, channel string) string {
	if teamURL == "" {
		return ""
	}
	return strings.TrimSuffix(teamURL, "/") + "/archives/" + channel
}

// Slack publishes a post's thread as replies in the message's thread.
var _ Replier = Slack{}

func (s Slack) Publish(ctx context.Context, c Credentials, p PostContent, set Settings) (Result, error) {
	token, asMe, err := slackAuthFor(c, set)
	if err != nil {
		return Result{}, err
	}
	chans, err := s.targets(ctx, c, set)
	if err != nil {
		return Result{}, err
	}
	parts := make([]slackPart, 0, len(chans))
	for _, ch := range chans {
		ts, err := s.send(ctx, c, token, ch, asMe, p, set, "")
		if err != nil {
			return Result{}, slackPartly(parts, err)
		}
		parts = append(parts, slackPart{channel: ch, ts: ts})
	}
	return slackResult(c, parts), nil
}

// Reply posts a thread part under the post in every channel it went to. A post's ExternalID lists
// "<channel>:<thread ts>" per channel (a reply's "<channel>:<thread ts>/<reply ts>"); posts from before several
// channels hold the bare "<thread ts>" of their one channel.
func (s Slack) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, set Settings) (Result, error) {
	token, asMe, err := slackAuthFor(c, set)
	if err != nil {
		return Result{}, err
	}
	var roots []slackPart
	if !strings.Contains(parentExternalID, ":") {
		chans, err := s.targets(ctx, c, set)
		if err != nil {
			return Result{}, err
		}
		root, _, _ := strings.Cut(parentExternalID, "/")
		roots = append(roots, slackPart{channel: chans[0], ts: root})
	} else {
		for _, e := range strings.Split(parentExternalID, ",") {
			ch, rest, _ := strings.Cut(e, ":")
			if root, _, _ := strings.Cut(rest, "/"); root != "" {
				roots = append(roots, slackPart{channel: ch, ts: root})
			}
		}
	}
	if len(roots) == 0 || roots[0].ts == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Slack: nothing to reply to (the post has no id)"}
	}
	parts := make([]slackPart, 0, len(roots))
	for _, r := range roots {
		ts, err := s.send(ctx, c, token, r.channel, asMe, p, set, r.ts)
		if err != nil {
			return Result{}, slackPartly(parts, err)
		}
		parts = append(parts, slackPart{channel: r.channel, thread: r.ts, ts: ts})
	}
	return slackResult(c, parts), nil
}

// slackPart is one channel's message of a post: its ts ("" when Slack did not say), and the thread it replies in.
type slackPart struct{ channel, thread, ts string }

// slackResult: the post's id in every channel, linked to its first message.
func slackResult(c Credentials, parts []slackPart) Result {
	var ids []string
	res := Result{URL: slackChannelURL(c["team_url"], parts[0].channel)}
	for _, p := range parts {
		if p.ts == "" {
			continue // published, but Slack has not said where: no replies there
		}
		id := p.ts
		if p.thread != "" {
			id = p.thread + "/" + p.ts
		}
		ids = append(ids, p.channel+":"+id)
		if len(ids) == 1 {
			res.URL = slackMessageURL(c["team_url"], p.channel, p.ts, p.thread)
		}
	}
	res.ExternalID = strings.Join(ids, ",")
	return res
}

// slackPartly: once some channels have the post, trying again would post it there twice, so the failure is final
// and names where it went.
func slackPartly(done []slackPart, err error) error {
	if len(done) == 0 {
		return err
	}
	var where []string
	for _, p := range done {
		where = append(where, p.channel)
	}
	return &Failure{Kind: ErrRejected, Msg: "Slack: published in " + strings.Join(where, ", ") + " but not in the next channel (" + err.Error() + "); post the rest separately"}
}

// slackAuthFor is the token a post goes out with: the app's, or the person's (post_as me).
func slackAuthFor(c Credentials, set Settings) (token string, asMe bool, err error) {
	if c["access_token"] == "" {
		return "", false, &Failure{Kind: ErrReauth, Msg: "Slack: no sign-in stored; reconnect the workspace"}
	}
	if set["post_as"] != "me" {
		return c["access_token"], false, nil
	}
	if c["user_token"] == "" {
		return "", true, &Failure{Kind: ErrRejected, Msg: "Slack: this channel can't post as a person yet; reconnect it from the channel's menu, or post as the app"}
	}
	return c["user_token"], true, nil
}

// targets are the channel ids a post goes to: the ones it names (#names are looked up), else the one Slack channel
// a channel connected before workspaces holds. A post saved before the setting was required names none.
func (s Slack) targets(ctx context.Context, c Credentials, set Settings) ([]string, error) {
	picked := ListValues(set["channel"])
	if len(picked) == 0 && c["channel_id"] != "" {
		return []string{c["channel_id"]}, nil
	}
	if len(picked) == 0 {
		return nil, &Failure{Kind: ErrRejected, Msg: "Slack: pick the Slack channels the post goes to in its Slack settings"}
	}
	var byName map[string]string
	out := make([]string, 0, len(picked))
	for _, v := range picked {
		if !strings.HasPrefix(v, "#") {
			out = append(out, v)
			continue
		}
		if byName == nil {
			chans, err := s.channels(ctx, c["access_token"])
			if err != nil {
				return nil, err
			}
			byName = map[string]string{}
			for _, ch := range chans {
				byName["#"+ch.Name] = ch.ID
			}
		}
		id, ok := byName[v]
		if !ok {
			return nil, &Failure{Kind: ErrRejected, Msg: "Slack: the app sees no channel " + v + " in " + c["team_name"] + "; pick another, or /invite the app to a private one"}
		}
		out = append(out, id)
	}
	return out, nil
}

// slackEscape escapes what Slack's message format reserves.
var slackEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// send posts one message in one channel (in threadTS's thread when set) and answers its ts, "" when Slack did not
// say where the files went.
func (s Slack) send(ctx context.Context, c Credentials, token, channel string, asMe bool, p PostContent, set Settings, threadTS string) (_ string, err error) {
	if channel != c["channel_id"] || asMe {
		// Only the connected channel posting as the app says anything about the connection: elsewhere, a channel
		// Slack refuses is about this post.
		defer func() { err = slackNotHere(err, asMe) }()
		if !asMe && threadTS == "" {
			// The app joins a public channel so it can post and share files there (a no-op once it is in). A private
			// channel it is not in fails below with Slack's own answer.
			if req, err := slackRequest(ctx, token, "conversations.join", map[string]any{"channel": channel}); err == nil {
				_ = slackDo(ctx, req, nil)
			}
		}
	}
	text := slackEscape.Replace(p.Text)
	if len(p.Media) == 0 {
		body := map[string]any{"channel": channel, "text": text}
		if threadTS != "" {
			body["thread_ts"] = threadTS
		}
		if set["link_preview"] == "false" {
			body["unfurl_links"], body["unfurl_media"] = false, false
		}
		req, err := slackRequest(ctx, token, "chat.postMessage", body)
		if err != nil {
			return "", err
		}
		var out struct {
			TS string `json:"ts"`
		}
		if err := slackDo(ctx, req, &out); err != nil {
			return "", slackLost(err)
		}
		return out.TS, nil
	}
	// Upload every file first (safe to retry: nothing is shared yet), then share them in one message.
	files := make([]map[string]string, 0, len(p.Media))
	for _, m := range p.Media {
		id, err := s.upload(ctx, token, m)
		if err != nil {
			return "", err
		}
		files = append(files, map[string]string{"id": id, "title": m.Name})
	}
	body := map[string]any{"files": files, "channel_id": channel}
	if text != "" {
		body["initial_comment"] = text
	}
	if threadTS != "" {
		body["thread_ts"] = threadTS
	}
	req, err := slackRequest(ctx, token, "files.completeUploadExternal", body)
	if err != nil {
		return "", err
	}
	if err := slackDo(ctx, req, nil); err != nil {
		return "", slackLost(err)
	}
	// The answer names the files, not the message: find the message the share made.
	return s.sharedTS(ctx, token, files[0]["id"], channel), nil
}

// Slack reports reactions (who and which) and the replies in the thread of every message a post made.
var _ MetricsReader = Slack{}

func (s Slack) Metrics(ctx context.Context, c Credentials, externalID string) (Metrics, error) {
	token := c["access_token"]
	var likes, replies int64
	byEmoji := map[string]*Reaction{}
	var order []string
	seen := map[string]map[string]bool{} // emoji -> user ids already counted
	var userIDs []string
	for _, part := range slackRoots(c, externalID) {
		var res struct {
			Message struct {
				ReplyCount int64 `json:"reply_count"`
				Reactions  []struct {
					Name  string   `json:"name"`
					Count int64    `json:"count"`
					Users []string `json:"users"`
				} `json:"reactions"`
			} `json:"message"`
		}
		q := url.Values{"channel": {part.channel}, "timestamp": {part.ts}, "full": {"true"}}
		req, err := slackRequest(ctx, token, "reactions.get?"+q.Encode(), nil)
		if err != nil {
			return Metrics{}, err
		}
		if err := slackDo(ctx, req, &res); err != nil {
			return Metrics{}, err
		}
		replies += res.Message.ReplyCount
		for _, r := range res.Message.Reactions {
			likes += r.Count
			if byEmoji[r.Name] == nil {
				byEmoji[r.Name], seen[r.Name] = &Reaction{Emoji: r.Name}, map[string]bool{}
				order = append(order, r.Name)
			}
			byEmoji[r.Name].Count += r.Count
			for _, u := range r.Users {
				if !seen[r.Name][u] {
					seen[r.Name][u] = true
					userIDs = append(userIDs, u)
				}
			}
		}
	}
	names := s.userNames(ctx, token, userIDs)
	out := Metrics{Likes: &likes, Replies: &replies}
	for _, e := range order {
		r := byEmoji[e]
		for u := range seen[e] {
			if n := names[u]; n != "" && len(r.People) < 20 {
				r.People = append(r.People, n)
			}
		}
		slices.Sort(r.People)
		out.Reactions = append(out.Reactions, *r)
	}
	return out, nil
}

// slackRoots are the messages a post made: "<channel>:<ts>" per channel, or the bare ts of a post from before
// several channels, in the channel it was connected to.
func slackRoots(c Credentials, externalID string) []slackPart {
	var out []slackPart
	if !strings.Contains(externalID, ":") {
		if root, _, _ := strings.Cut(externalID, "/"); root != "" && c["channel_id"] != "" {
			out = append(out, slackPart{channel: c["channel_id"], ts: root})
		}
		return out
	}
	for _, e := range strings.Split(externalID, ",") {
		ch, rest, _ := strings.Cut(e, ":")
		if root, _, _ := strings.Cut(rest, "/"); ch != "" && root != "" {
			out = append(out, slackPart{channel: ch, ts: root})
		}
	}
	return out
}

// userNames looks up the people who reacted (users:read; a workspace connected before it gives no names, only
// counts). ponytail: one users.info per person, up to 50 a read; users.list when posts get big audiences.
func (Slack) userNames(ctx context.Context, token string, ids []string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		if _, done := out[id]; done || len(out) >= 50 {
			continue
		}
		var res struct {
			User struct {
				Name    string `json:"name"`
				Profile struct {
					DisplayName string `json:"display_name"`
					RealName    string `json:"real_name"`
				} `json:"profile"`
			} `json:"user"`
		}
		req, err := slackRequest(ctx, token, "users.info?"+url.Values{"user": {id}}.Encode(), nil)
		if err != nil || slackDo(ctx, req, &res) != nil {
			out[id] = ""
			continue
		}
		p := res.User.Profile
		out[id] = p.DisplayName
		for _, n := range []string{p.RealName, res.User.Name} {
			if out[id] == "" {
				out[id] = n
			}
		}
	}
	return out
}

const slackGone = "Slack: the app can no longer post in this channel"

// slackNotHere turns a channel Slack refuses into a failed post when the post went to another channel or as the
// person: the connected channel itself is fine.
func slackNotHere(err error, asMe bool) error {
	var f *Failure
	if !errors.As(err, &f) || !strings.HasPrefix(f.Msg, slackGone) {
		return err
	}
	if asMe {
		return &Failure{Kind: ErrRejected, Msg: "Slack: the person who connected Slack can't post in that channel (they are not in it, or it is archived); post as the app or pick another channel"}
	}
	return &Failure{Kind: ErrRejected, Msg: "Slack: the app can't post in that channel (invite it with /invite, or unarchive the channel), or pick another one"}
}

// slackLost: Slack has no idempotency key, so posting again after an answer that never came could post twice.
func slackLost(err error) error {
	var f *Failure
	if errors.As(err, &f) {
		return err
	}
	return &Failure{Kind: ErrRejected, Msg: "Slack may have published the post, but its answer was lost (" + err.Error() + "); check the channel before posting it again"}
}

func slackMessageURL(teamURL, channel, ts, threadTS string) string {
	base := slackChannelURL(teamURL, channel)
	if base == "" {
		return ""
	}
	u := base + "/p" + strings.Replace(ts, ".", "", 1)
	if threadTS != "" {
		u += "?thread_ts=" + threadTS + "&cid=" + channel
	}
	return u
}

// upload sends one file's bytes to the address Slack gives for it and returns the file's id.
func (Slack) upload(ctx context.Context, token string, m MediaFile) (string, error) {
	f, err := m.Reader(ctx)
	if err != nil {
		return "", fmt.Errorf("Slack: cannot read %s: %w", m.Name, err)
	}
	defer f.Close()
	var data io.Reader = f
	size := m.Size
	if size <= 0 { // Slack needs the exact length up front
		b, err := io.ReadAll(io.LimitReader(f, maxMediaRead+1))
		if err == nil && int64(len(b)) > maxMediaRead {
			err = errors.New("too large to send")
		}
		if err != nil {
			return "", fmt.Errorf("Slack: cannot read %s: %w", m.Name, err)
		}
		data, size = bytes.NewReader(b), int64(len(b))
	}
	var slot struct {
		UploadURL string `json:"upload_url"`
		FileID    string `json:"file_id"`
	}
	form := url.Values{"filename": {m.Name}, "length": {strconv.FormatInt(size, 10)}}
	req, err := checkForm(ctx, slackAPI+"/files.getUploadURLExternal", form)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if err := slackDo(ctx, req, &slot); err != nil {
		return "", err
	}
	if slot.UploadURL == "" || slot.FileID == "" {
		return "", errors.New("Slack gave no address to upload " + m.Name + " to")
	}
	up, err := http.NewRequestWithContext(ctx, http.MethodPost, slot.UploadURL, data)
	if err != nil {
		return "", err
	}
	up.ContentLength = size
	up.Header.Set("Content-Type", "application/octet-stream")
	resp, err := httpClient(ctx).Do(up)
	if err != nil {
		return "", fmt.Errorf("Slack could not be reached (%s)", up.URL.Host)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return "", &Failure{Msg: fmt.Sprintf("Slack could not take %s (%d); staqly tries again in a few minutes", m.Name, resp.StatusCode)}
		}
		return "", &Failure{Kind: ErrRejected, Msg: fmt.Sprintf("Slack refused %s (%d)", m.Name, resp.StatusCode)}
	}
	return slot.FileID, nil
}

// sharedTS waits until Slack reports the message that shared the file in the channel ("" when it does not say).
func (Slack) sharedTS(ctx context.Context, token, fileID, channel string) string {
	for i := 0; i < slackPolls; i++ {
		var info struct {
			File struct {
				Shares map[string]map[string][]struct {
					TS string `json:"ts"`
				} `json:"shares"` // public|private -> channel -> shares
			} `json:"file"`
		}
		req, err := slackRequest(ctx, token, "files.info?"+url.Values{"file": {fileID}}.Encode(), nil)
		if err != nil {
			return ""
		}
		if slackDo(ctx, req, &info) == nil {
			for _, kind := range []string{"public", "private"} {
				if s := info.File.Shares[kind][channel]; len(s) > 0 && s[0].TS != "" {
					return s[0].TS
				}
			}
		}
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(slackPoll):
		}
	}
	return ""
}

// slackRequest builds a Web API call: a GET for a method with its query, a JSON POST when body is set, a bare
// POST otherwise.
func slackRequest(ctx context.Context, token, method string, body map[string]any) (*http.Request, error) {
	verb, rd := http.MethodGet, io.Reader(nil)
	if body != nil {
		b, _ := json.Marshal(body)
		verb, rd = http.MethodPost, bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, verb, slackAPI+"/"+method, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	return req, nil
}

// slackDo sends a request and decodes the reply into out. Slack answers 200 with {"ok":false,"error":"..."} for
// most failures, 429 with Retry-After for rate limits.
func slackDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("Slack could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusTooManyRequests {
		return &Failure{Msg: "Slack rate limit reached; staqly tries again in a few minutes"}
	}
	if resp.StatusCode >= 500 {
		return &Failure{Msg: fmt.Sprintf("Slack is unavailable (%d); staqly tries again in a few minutes", resp.StatusCode)}
	}
	var env struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &env) != nil {
		return fmt.Errorf("Slack answered %d without a readable reply", resp.StatusCode)
	}
	if !env.OK {
		return slackError(env.Error)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// slackError maps Slack's error codes to what staqly does next: a sign-in problem needs reconnecting, a refused
// message is final, the rest is retried.
func slackError(code string) error {
	rejected := func(msg string) error { return &Failure{Kind: ErrRejected, Msg: "Slack: " + msg} }
	switch code {
	case "invalid_auth", "not_authed", "token_revoked", "token_expired", "account_inactive", "invalid_refresh_token",
		"app_uninstalled", "team_access_not_granted":
		return &Failure{Kind: ErrReauth, Msg: "Slack no longer accepts the app's sign-in for this workspace (" + code + "); reconnect the channel"}
	case "missing_scope", "no_permission", "not_allowed_token_type":
		return &Failure{Kind: ErrReauth, Msg: "Slack: the app is missing a permission (" + code + "); reconnect the channel to grant it"}
	case "channel_not_found", "not_in_channel", "is_archived":
		return &Failure{Kind: ErrReauth, Msg: slackGone + " (" + code + "); invite it back or unarchive the channel, then reconnect"}
	case "ratelimited", "rate_limited":
		return &Failure{Msg: "Slack rate limit reached; staqly tries again in a few minutes"}
	case "internal_error", "fatal_error", "service_unavailable", "request_timeout":
		return &Failure{Msg: "Slack is unavailable (" + code + "); staqly tries again in a few minutes"}
	case "invalid_code", "code_already_used":
		return rejected("the sign-in expired or was used already; connect again")
	case "bad_redirect_uri":
		// For whoever runs the server: the app's OAuth & Permissions page needs <dashboard>/channels/callback/slack.
		return rejected("the app does not list this server's return address; connect again once it does")
	case "invalid_client_id", "bad_client_secret":
		// For whoever runs the server: PROVIDER_SLACK_CLIENT_ID and _SECRET are on the app's Basic Information page.
		return rejected("this server's Slack app keys are not accepted (" + code + ")")
	case "msg_too_long":
		return rejected("the message is too long")
	case "no_text":
		return rejected("the message is empty")
	case "restricted_action", "restricted_action_read_only_channel", "restricted_action_thread_only_channel",
		"restricted_action_non_threadable_channel", "ekm_access_denied", "posting_to_general_channel_denied":
		return rejected("the workspace does not let the app post here (" + code + ")")
	case "file_uploads_disabled", "file_upload_size_restricted", "file_uploads_except_images_disabled":
		return rejected("the workspace does not take this file (" + code + ")")
	case "thread_reply_not_allowed", "cannot_reply_to_message":
		return rejected("Slack does not take a reply under this message (" + code + ")")
	}
	return rejected("Slack refused the request (" + code + ")")
}

// Slack checks the app's keys before the sign-in code: a made-up code with good keys is "invalid_code".
func (Slack) CheckApp(ctx context.Context, app App, redirect string) (bool, error) {
	req, err := checkForm(ctx, slackAPI+"/oauth.v2.access", url.Values{"client_id": {app.ClientID},
		"client_secret": {app.ClientSecret}, "code": {checkCode}, "redirect_uri": {redirect}})
	if err != nil {
		return false, err
	}
	status, body, err := probe(ctx, req, "Slack")
	switch {
	case err != nil:
		return false, err
	case strings.Contains(body, "invalid_client_id"), strings.Contains(body, "bad_client_secret"):
		return false, errors.New("Slack did not accept the app keys: check the Client ID and Client Secret on the app's Basic Information page")
	case strings.Contains(body, "bad_redirect_uri"):
		return false, errors.New("Slack took the app keys but not the return address: add " + redirect + " under OAuth & Permissions > Redirect URLs")
	case strings.Contains(body, "invalid_code"), strings.Contains(body, "code_already_used"):
		return true, nil
	}
	return false, unexpected("Slack", status, body)
}
