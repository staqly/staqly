package providers

import (
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

// Discord through the server's own bot: the person adds the server's Discord application to a Discord server
// (OAuth2 bot install); that server (guild) is the staqly channel and each post names the Discord channels it goes
// to (the channel setting, picked from Options), like a Slack workspace. Every call goes out as the bot
// (Authorization: Bot <PROVIDER_DISCORD_BOT_TOKEN>). The token is read at use time through getenv, not kept per
// channel: a channel stores only its guild's id, and a new token for the same bot keeps every channel working. A
// post is one message per channel (files as attachments); its thread replies to the message before it in each
// channel. Channels connected before the bot are webhooks (discord_webhook.go).
type Discord struct{ Base }

// discordAuthorize is Discord's consent page, a variable so tests point it at httptest.
var discordAuthorize = "https://discord.com/oauth2/authorize"

// discordBotEnv holds the bot user's token (Developer Portal > the application > Bot > Reset Token).
const discordBotEnv = "PROVIDER_DISCORD_BOT_TOKEN"

// discordPermissions the install asks for: View Channel, Send Messages, Embed Links, Attach Files, Read Message
// History, Create Public Threads, Send Messages in Threads.
const discordPermissions = "309237763072"

// Permission bits the channel list checks.
const (
	discordAdministrator uint64 = 1 << 3
	discordViewChannel   uint64 = 1 << 10
	discordSendMessages  uint64 = 1 << 11
)

func init() {
	Register(Discord{NewBase("discord", "Discord", AuthOAuth2,
		Limits{MaxChars: 2000, MaxMedia: 10, MediaKinds: []string{"image", "video"}}).WithSettings(discordSettings...)}, StatusAvailable)
}

// discordSettings: per-post settings.
var discordSettings = []Field{
	{Key: "channel", Label: "Discord channels", Kind: "remote", Required: true, Prefer: []string{"#general"},
		MaxItems: 10, MaxLength: 400, Help: "Text and announcement channels the bot may post in. For a private one, give the bot's role access to it first."},
}

// The bot posts, so the app is the server's alone and Discord waits for its bot token as well as its keys.
func (Discord) Needs() Needs { return Needs{BotToken: true} }

func (Discord) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{
		"client_id":     {app.ClientID},
		"scope":         {"bot"},
		"permissions":   {discordPermissions},
		"response_type": {"code"},
		"redirect_uri":  {redirect},
		"state":         {state},
	}
	return discordAuthorize + "?" + q.Encode(), nil
}

type discordGuild struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Icon  string `json:"icon"`
	Roles []struct {
		ID          string `json:"id"`
		Permissions string `json:"permissions"`
	} `json:"roles"`
}

// Exchange trades the code for the guild the bot was added to. The person's own token (scope bot) can do nothing
// staqly needs, so it is not kept: the guild's id is the whole connection.
func (d Discord) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	req, err := checkForm(ctx, discordAPI+"/oauth2/token", url.Values{"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {redirect}, "client_id": {app.ClientID}, "client_secret": {app.ClientSecret}})
	if err != nil {
		return nil, Profile{}, err
	}
	var t struct {
		Guild struct {
			ID string `json:"id"`
		} `json:"guild"`
	}
	if err := discordOAuthDo(ctx, req, &t); err != nil {
		return nil, Profile{}, err
	}
	if t.Guild.ID == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Discord did not say which server the bot joined; connect again and pick a server"}
	}
	// The bot reads the guild itself: this also checks that the bot token belongs to the app the person added.
	g, err := d.guild(ctx, t.Guild.ID)
	if err != nil {
		return nil, Profile{}, err
	}
	return Credentials{"guild_id": g.ID}, discordProfile(g), nil
}

// discordProfile: the server's name, its id as the handle (two servers may share a name), its icon.
func discordProfile(g discordGuild) Profile {
	p := Profile{Name: g.Name, Handle: g.ID, ProfileURL: "https://discord.com/channels/" + g.ID}
	if p.Name == "" {
		p.Name = "Discord server"
	}
	if g.Icon != "" {
		p.AvatarURL = "https://cdn.discordapp.com/icons/" + g.ID + "/" + g.Icon + ".png"
	}
	return p
}

// Refresh: the bot's token does not expire, so this checks that the bot is still in the server.
func (d Discord) Refresh(ctx context.Context, _ App, c Credentials) (Credentials, error) {
	if c["guild_id"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Discord: no server stored; connect the channel again"}
	}
	if _, err := d.guild(ctx, c["guild_id"]); err != nil {
		return nil, err
	}
	return c, nil
}

func (Discord) guild(ctx context.Context, id string) (discordGuild, error) {
	var g discordGuild
	return g, discordBotCall(ctx, http.MethodGet, "/guilds/"+url.PathEscape(id), nil, "", true, &g)
}

type discordChannel struct {
	ID         string `json:"id"`
	Type       int    `json:"type"` // 0 text, 4 category, 5 announcement
	Name       string `json:"name"`
	Position   int    `json:"position"`
	ParentID   string `json:"parent_id"`
	Overwrites []struct {
		ID    string `json:"id"`
		Type  int    `json:"type"` // 0 role, 1 member
		Allow string `json:"allow"`
		Deny  string `json:"deny"`
	} `json:"permission_overwrites"`
}

// Discord lists the server's channels for the channel setting.
var _ OptionLister = Discord{}

func (d Discord) Options(ctx context.Context, c Credentials, key string) ([]Option, error) {
	if key != "channel" {
		return nil, nil
	}
	chans, err := d.channels(ctx, c["guild_id"])
	if err != nil {
		return nil, err
	}
	chans = d.postable(ctx, c["guild_id"], chans)
	out := make([]Option, 0, len(chans))
	for _, ch := range chans {
		out = append(out, Option{Value: ch.ID, Label: "#" + ch.Name})
	}
	return out, nil
}

// channels are the server's text and announcement channels in the order Discord shows them (by category).
func (Discord) channels(ctx context.Context, guildID string) ([]discordChannel, error) {
	if guildID == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Discord: no server stored; connect the channel again"}
	}
	var all []discordChannel
	if err := discordBotCall(ctx, http.MethodGet, "/guilds/"+url.PathEscape(guildID)+"/channels", nil, "", true, &all); err != nil {
		return nil, err
	}
	category := map[string]int{}
	for _, ch := range all {
		if ch.Type == 4 {
			category[ch.ID] = ch.Position + 1
		}
	}
	out := []discordChannel{}
	for _, ch := range all {
		if ch.Type == 0 || ch.Type == 5 {
			out = append(out, ch)
		}
	}
	slices.SortStableFunc(out, func(a, b discordChannel) int {
		if ca, cb := category[a.ParentID], category[b.ParentID]; ca != cb {
			return ca - cb
		}
		return a.Position - b.Position
	})
	return out, nil
}

// postable keeps the channels the bot may see and send messages in: its roles' permissions with each channel's
// overwrites applied, as Discord computes them. When the bot's roles can't be read, every channel is kept and a
// channel the bot can't use fails at publish instead.
func (d Discord) postable(ctx context.Context, guildID string, chans []discordChannel) []discordChannel {
	var me struct {
		ID string `json:"id"`
	}
	var member struct {
		Roles []string `json:"roles"`
	}
	if discordBotCall(ctx, http.MethodGet, "/users/@me", nil, "", false, &me) != nil || me.ID == "" ||
		discordBotCall(ctx, http.MethodGet, "/guilds/"+url.PathEscape(guildID)+"/members/"+url.PathEscape(me.ID), nil, "", false, &member) != nil {
		return chans
	}
	g, err := d.guild(ctx, guildID)
	if err != nil {
		return chans
	}
	bits := func(s string) uint64 { v, _ := strconv.ParseUint(s, 10, 64); return v }
	roles := map[string]uint64{}
	for _, r := range g.Roles {
		roles[r.ID] = bits(r.Permissions)
	}
	base := roles[guildID] // @everyone has the guild's id
	for _, r := range member.Roles {
		base |= roles[r]
	}
	if base&discordAdministrator != 0 {
		return chans
	}
	mine := map[string]bool{}
	for _, r := range member.Roles {
		mine[r] = true
	}
	out := []discordChannel{}
	for _, ch := range chans {
		p := base
		var allow, deny uint64
		for _, o := range ch.Overwrites {
			if o.Type == 0 && o.ID == guildID {
				p = p&^bits(o.Deny) | bits(o.Allow)
			}
		}
		for _, o := range ch.Overwrites {
			if o.Type == 0 && mine[o.ID] {
				allow, deny = allow|bits(o.Allow), deny|bits(o.Deny)
			}
		}
		p = p&^deny | allow
		for _, o := range ch.Overwrites {
			if o.Type == 1 && o.ID == me.ID {
				p = p&^bits(o.Deny) | bits(o.Allow)
			}
		}
		if p&discordViewChannel != 0 && p&discordSendMessages != 0 {
			out = append(out, ch)
		}
	}
	return out
}

// Discord publishes a post's thread as replies to the post's message in every channel it went to.
var _ Replier = Discord{}

func (d Discord) Publish(ctx context.Context, c Credentials, p PostContent, set Settings) (Result, error) {
	chans, err := d.targets(ctx, c, set)
	if err != nil {
		return Result{}, err
	}
	parts := make([]discordPart, 0, len(chans))
	for _, ch := range chans {
		id, err := d.send(ctx, ch, p, "")
		if err != nil {
			return Result{}, discordPartly(parts, err)
		}
		parts = append(parts, discordPart{channel: ch, id: id})
	}
	return discordResult(c, parts), nil
}

// Reply answers each message of the part before: its ExternalID lists "<channel>:<message id>" per channel.
func (d Discord) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	var roots []discordPart
	for _, e := range strings.Split(parentExternalID, ",") {
		if ch, id, ok := strings.Cut(e, ":"); ok && ch != "" && id != "" {
			roots = append(roots, discordPart{channel: ch, id: id})
		}
	}
	if len(roots) == 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Discord: nothing to reply to (the post has no id)"}
	}
	parts := make([]discordPart, 0, len(roots))
	for _, r := range roots {
		id, err := d.send(ctx, r.channel, p, r.id)
		if err != nil {
			return Result{}, discordPartly(parts, err)
		}
		parts = append(parts, discordPart{channel: r.channel, id: id})
	}
	return discordResult(c, parts), nil
}

// discordPart is one channel's message of a post.
type discordPart struct{ channel, id string }

// discordResult: the post's message in every channel, linked to the first.
func discordResult(c Credentials, parts []discordPart) Result {
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		ids = append(ids, p.channel+":"+p.id)
	}
	return Result{ExternalID: strings.Join(ids, ","),
		URL: "https://discord.com/channels/" + c["guild_id"] + "/" + parts[0].channel + "/" + parts[0].id}
}

// discordPartly: once some channels have the post, trying again would post it there twice, so the failure is final
// and names where it went.
func discordPartly(done []discordPart, err error) error {
	if len(done) == 0 {
		return err
	}
	where := make([]string, 0, len(done))
	for _, p := range done {
		where = append(where, p.channel)
	}
	return &Failure{Kind: ErrRejected, Msg: "Discord: published in " + strings.Join(where, ", ") + " but not in the next channel (" + err.Error() + "); post the rest separately"}
}

// targets are the channel ids a post goes to: the ones it names, #names looked up in the server.
func (d Discord) targets(ctx context.Context, c Credentials, set Settings) ([]string, error) {
	picked := ListValues(set["channel"])
	if len(picked) == 0 {
		return nil, &Failure{Kind: ErrRejected, Msg: "Discord: pick the Discord channels the post goes to in its Discord settings"}
	}
	var byName map[string]string
	out := make([]string, 0, len(picked))
	for _, v := range picked {
		if !strings.HasPrefix(v, "#") {
			out = append(out, v)
			continue
		}
		if byName == nil {
			chans, err := d.channels(ctx, c["guild_id"])
			if err != nil {
				return nil, err
			}
			byName = map[string]string{}
			for _, ch := range chans {
				if _, taken := byName["#"+ch.Name]; !taken {
					byName["#"+ch.Name] = ch.ID
				}
			}
		}
		id, ok := byName[v]
		if !ok {
			return nil, &Failure{Kind: ErrRejected, Msg: "Discord: the server has no text channel " + v + "; pick another"}
		}
		out = append(out, id)
	}
	return out, nil
}

// send posts one message in one channel (a reply to replyTo when set) and answers its id.
func (Discord) send(ctx context.Context, channel string, p PostContent, replyTo string) (string, error) {
	payload := map[string]any{"content": p.Text}
	if replyTo != "" {
		// The comment still goes out when its parent was deleted in the meantime.
		payload["message_reference"] = map[string]any{"message_id": replyTo, "fail_if_not_exists": false}
	}
	body, ctype, err := discordMultipart(ctx, payload, p.Media)
	if err != nil {
		return "", err
	}
	var out discordMessage
	if err := discordBotCall(ctx, http.MethodPost, "/channels/"+url.PathEscape(channel)+"/messages", body, ctype, false, &out); err != nil {
		return "", discordLost(err)
	}
	if out.ID == "" {
		return "", &Failure{Kind: ErrRejected, Msg: "Discord published the post but did not say its id; check the channel"}
	}
	return out.ID, nil
}

// discordBotCall calls the API as the bot and decodes a 2xx reply into out. guildLevel: the call is about the
// server itself, so a refusal means the bot is no longer in it (reconnect), not that one channel is closed to it.
func discordBotCall(ctx context.Context, method, path string, body io.Reader, ctype string, guildLevel bool, out any) error {
	token := strings.TrimSpace(getenv(ctx, discordBotEnv))
	if token == "" {
		return &Failure{Kind: ErrRejected, Msg: "Discord: this staqly server has no Discord bot token (" + discordBotEnv + "); ask whoever runs it to set it"}
	}
	req, err := http.NewRequestWithContext(ctx, method, discordAPI+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+token)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("Discord could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			return json.Unmarshal(raw, out)
		}
		return nil
	}
	return discordBotError(resp.StatusCode, resp.Header.Get("Retry-After"), raw, guildLevel)
}

// discordBotError maps Discord's replies to what staqly does next: a bot that is gone from the server needs
// reconnecting, a channel closed to it or a refused message fails the post, rate limits and outages are retried.
func discordBotError(status int, retryAfter string, raw []byte, guildLevel bool) error {
	var e struct {
		Message    string          `json:"message"`
		Code       int             `json:"code"`
		RetryAfter float64         `json:"retry_after"`
		Errors     json.RawMessage `json:"errors"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := e.Message
	if detail == "" {
		detail = truncateDetail(string(raw))
	}
	rejected := func(msg string) error { return &Failure{Kind: ErrRejected, Msg: "Discord: " + msg} }
	switch {
	case status == http.StatusTooManyRequests:
		f := &Failure{Msg: "Discord rate limit reached; staqly tries again in a few minutes"}
		wait := e.RetryAfter
		if wait <= 0 {
			wait, _ = strconv.ParseFloat(retryAfter, 64)
		}
		if wait > 0 {
			f.RetryAt = time.Now().Add(time.Duration(wait * float64(time.Second)))
		}
		return f
	case status >= 500:
		return &Failure{Msg: fmt.Sprintf("Discord is unavailable (%d); staqly tries again in a few minutes", status)}
	case status == http.StatusUnauthorized:
		// For whoever runs the server: PROVIDER_DISCORD_BOT_TOKEN is the token on the application's Bot page.
		return &Failure{Kind: ErrReauth, Msg: "Discord does not accept this staqly server's bot token; whoever runs it sets " + discordBotEnv + " to the bot's current token"}
	case e.Code == 10004, guildLevel && (status == http.StatusForbidden || status == http.StatusNotFound):
		return &Failure{Kind: ErrReauth, Msg: "Discord: the bot is no longer in this server (removed, or the server is gone); connect the server again"}
	case e.Code == 10003:
		return rejected("that channel is gone (deleted); pick another one")
	case e.Code == 10008:
		return rejected("the message to reply to is gone")
	case status == http.StatusRequestEntityTooLarge, e.Code == 40005:
		return rejected("a file is larger than this server lets the bot upload (10 MB unless the server is boosted)")
	case status == http.StatusForbidden:
		// 50001 Missing Access, 50013 Missing Permissions: this channel only, the connection is fine.
		return rejected("the bot can't post in that channel (" + detail + "); give its role access to the channel, or pick another one")
	case status == http.StatusBadRequest:
		if len(e.Errors) > 0 {
			return rejected(detail + ": " + truncateDetail(string(e.Errors)))
		}
		return rejected(detail)
	}
	return &Failure{Msg: fmt.Sprintf("Discord answered %d: %s", status, detail)}
}

func truncateDetail(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

// discordOAuthDo posts to the token endpoint, whose errors are OAuth's ({"error","error_description"}).
func discordOAuthDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("Discord could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return json.Unmarshal(raw, out)
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return discordBotError(resp.StatusCode, resp.Header.Get("Retry-After"), raw, false)
	}
	var e struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.Unmarshal(raw, &e)
	switch e.Error {
	case "invalid_client":
		// For whoever runs the server: PROVIDER_DISCORD_CLIENT_ID and _SECRET are on the application's OAuth2 page.
		return &Failure{Kind: ErrRejected, Msg: "Discord: this server's Discord app keys are not accepted (invalid_client)"}
	case "invalid_grant":
		if strings.Contains(e.Description, "redirect_uri") {
			return &Failure{Kind: ErrRejected, Msg: "Discord: the app does not list this server's return address; connect again once it does"}
		}
		return &Failure{Kind: ErrRejected, Msg: "Discord: the sign-in expired or was used already; connect again"}
	}
	return &Failure{Kind: ErrRejected, Msg: "Discord refused the sign-in (" + truncateDetail(firstNonBlank(e.Error, string(raw))) + ")"}
}

func firstNonBlank(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Discord checks the app's keys before the code: a made-up code with good keys is "invalid_grant".
func (Discord) CheckApp(ctx context.Context, app App, redirect string) (bool, error) {
	req, err := checkForm(ctx, discordAPI+"/oauth2/token", url.Values{"grant_type": {"authorization_code"}, "code": {checkCode},
		"redirect_uri": {redirect}, "client_id": {app.ClientID}, "client_secret": {app.ClientSecret}})
	if err != nil {
		return false, err
	}
	status, body, err := probe(ctx, req, "Discord")
	switch {
	case err != nil:
		return false, err
	case strings.Contains(body, "invalid_client"):
		return false, errors.New("Discord did not accept the app keys: check the Client ID and Client Secret on the application's OAuth2 page")
	case strings.Contains(body, "invalid_grant"):
		return true, nil
	}
	return false, unexpected("Discord", status, body)
}
