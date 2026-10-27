package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// fakeDiscordBot answers like Discord's API for the bot "bot1" in the server 800.
type fakeDiscordBot struct {
	mu        sync.Mutex
	posts     []discordPost // every message posted
	failIn    map[string]string
	rateLimit bool
	srv       *httptest.Server
	n         int
}

type discordPost struct {
	channel string
	payload map[string]any
	files   int
}

func (f *fakeDiscordBot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	write := func(code int, body string) {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}
	if r.URL.Path == "/oauth2/token" {
		_ = r.ParseForm()
		switch {
		case r.Form.Get("client_secret") != "secret":
			write(401, `{"error":"invalid_client"}`)
		case r.Form.Get("code") != "good" || r.Form.Get("grant_type") != "authorization_code":
			write(400, `{"error":"invalid_grant","error_description":"Invalid \"code\" in request."}`)
		default:
			write(200, `{"access_token":"user-token","token_type":"Bearer","scope":"bot","guild":{"id":"800","name":"Jane Doe Club"}}`)
		}
		return
	}
	if r.Header.Get("Authorization") != "Bot bot-token" {
		write(401, `{"message":"401: Unauthorized","code":0}`)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/guilds/800":
		write(200, `{"id":"800","name":"Jane Doe Club","icon":"ic0n","roles":[{"id":"800","permissions":"3072"},{"id":"r1","permissions":"0"}]}`)
	case r.Method == http.MethodGet && r.URL.Path == "/guilds/800/channels":
		write(200, `[
			{"id":"c2","type":4,"name":"Later","position":1},
			{"id":"c1","type":4,"name":"Talk","position":0},
			{"id":"906","type":0,"name":"late","position":0,"parent_id":"c2"},
			{"id":"902","type":5,"name":"news","position":1,"parent_id":"c1"},
			{"id":"901","type":0,"name":"general","position":0,"parent_id":"c1"},
			{"id":"903","type":0,"name":"secret","position":2,"parent_id":"c1","permission_overwrites":[{"id":"800","type":0,"allow":"0","deny":"1024"}]},
			{"id":"904","type":2,"name":"voice","position":3,"parent_id":"c1"},
			{"id":"905","type":0,"name":"bots","position":4,"parent_id":"c1","permission_overwrites":[{"id":"800","type":0,"allow":"0","deny":"1024"},{"id":"r1","type":0,"allow":"1024","deny":"0"}]}
		]`)
	case r.Method == http.MethodGet && r.URL.Path == "/users/@me":
		write(200, `{"id":"bot1"}`)
	case r.Method == http.MethodGet && r.URL.Path == "/guilds/800/members/bot1":
		write(200, `{"roles":["r1"]}`)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/channels/") && strings.HasSuffix(r.URL.Path, "/messages"):
		channel := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/channels/"), "/messages")
		if f.rateLimit {
			w.Header().Set("Retry-After", "2")
			write(429, `{"message":"You are being rate limited.","retry_after":2,"global":false}`)
			return
		}
		if body, ok := f.failIn[channel]; ok {
			code := 403
			if strings.Contains(body, "10003") {
				code = 404
			}
			write(code, body)
			return
		}
		_ = r.ParseMultipartForm(1 << 20)
		p := discordPost{channel: channel}
		_ = json.Unmarshal([]byte(r.FormValue("payload_json")), &p.payload)
		if r.MultipartForm != nil {
			p.files = len(r.MultipartForm.File)
		}
		f.posts = append(f.posts, p)
		f.n++
		write(200, fmt.Sprintf(`{"id":"m%d","channel_id":"%s"}`, f.n, channel))
	default:
		write(404, `{"message":"Unknown Guild","code":10004}`)
	}
}

func useFakeDiscordBot(t *testing.T) (*fakeDiscordBot, context.Context) {
	t.Helper()
	f := &fakeDiscordBot{failIn: map[string]string{}}
	f.srv = httptest.NewServer(f)
	old := discordAPI
	discordAPI = f.srv.URL
	t.Cleanup(func() { f.srv.Close(); discordAPI = old })
	return f, discordBotCtx("bot-token")
}

func discordBotCtx(token string) context.Context {
	return WithGetenv(context.Background(), func(k string) string {
		if k == discordBotEnv {
			return token
		}
		return ""
	})
}

func TestDiscordAuthURL(t *testing.T) {
	u, err := Discord{}.AuthURL(App{ClientID: "cid"}, "https://app.example/channels/callback/discord", "st", nil)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.Parse(u)
	v := q.Query()
	if !strings.HasPrefix(u, "https://discord.com/oauth2/authorize?") || v.Get("client_id") != "cid" || v.Get("scope") != "bot" ||
		v.Get("permissions") != "309237763072" || v.Get("response_type") != "code" || v.Get("state") != "st" ||
		v.Get("redirect_uri") != "https://app.example/channels/callback/discord" {
		t.Fatal(u)
	}
	d, _ := Get("discord")
	if d.Auth() != AuthOAuth2 || !d.Provider.(Needer).Needs().BotToken {
		t.Fatal("discord is a bot install")
	}
}

func TestDiscordExchange(t *testing.T) {
	_, ctx := useFakeDiscordBot(t)
	app := App{ClientID: "cid", ClientSecret: "secret"}
	creds, profile, err := Discord{}.Exchange(ctx, app, "https://app.example/cb", "good", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds["guild_id"] != "800" {
		t.Fatalf("creds %v", creds)
	}
	if profile.Name != "Jane Doe Club" || profile.Handle != "800" || profile.ProfileURL != "https://discord.com/channels/800" ||
		profile.AvatarURL != "https://cdn.discordapp.com/icons/800/ic0n.png" {
		t.Fatalf("profile %+v", profile)
	}
	if _, _, err := (Discord{}).Exchange(ctx, app, "https://app.example/cb", "used", nil); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("bad code: %v", err)
	}
	if _, _, err := (Discord{}).Exchange(ctx, App{ClientID: "cid", ClientSecret: "nope"}, "https://app.example/cb", "good", nil); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "app keys") {
		t.Fatalf("bad keys: %v", err)
	}
	// A bot token of another application: the bot is not where the person added the app.
	if _, _, err := (Discord{}).Exchange(discordBotCtx("other"), app, "https://app.example/cb", "good", nil); !errors.Is(err, ErrReauth) {
		t.Fatalf("wrong bot token: %v", err)
	}
	if _, _, err := (Discord{}).Exchange(discordBotCtx(""), app, "https://app.example/cb", "good", nil); err == nil || !strings.Contains(err.Error(), discordBotEnv) {
		t.Fatalf("no bot token: %v", err)
	}

	if _, err := (Discord{}).Refresh(ctx, app, creds); err != nil {
		t.Fatal(err)
	}
	if _, err := (Discord{}).Refresh(ctx, app, Credentials{"guild_id": "999"}); !errors.Is(err, ErrReauth) {
		t.Fatalf("bot removed: %v", err)
	}

	ok, err := Discord{}.CheckApp(ctx, app, "https://app.example/cb")
	if !ok || err != nil {
		t.Fatalf("check: %v %v", ok, err)
	}
	if ok, err := (Discord{}).CheckApp(ctx, App{ClientID: "cid", ClientSecret: "nope"}, "https://app.example/cb"); ok || err == nil {
		t.Fatal("check with bad keys")
	}
}

func TestDiscordOptions(t *testing.T) {
	_, ctx := useFakeDiscordBot(t)
	opts, err := Discord{}.Options(ctx, Credentials{"guild_id": "800"}, "channel")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range opts {
		got = append(got, o.Value+o.Label)
	}
	// By category, then position; no voice channel, none the bot can't see (#secret), one its role may see (#bots).
	if strings.Join(got, " ") != "901#general 902#news 905#bots 906#late" {
		t.Fatal(got)
	}
}

func TestDiscordPublish(t *testing.T) {
	f, ctx := useFakeDiscordBot(t)
	d := Discord{}
	c := Credentials{"guild_id": "800"}

	if _, err := d.Publish(ctx, c, PostContent{Text: "Hi"}, Settings{}); !errors.Is(err, ErrRejected) {
		t.Fatalf("no channel: %v", err)
	}
	media := []MediaFile{chatMedia("a.jpg", "image/jpeg", "a"), chatMedia("b.mp4", "video/mp4", "b")}
	res, err := d.Publish(ctx, c, PostContent{Text: "Hello", Media: media}, Settings{"channel": "901, #news"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "901:m1,902:m2" || res.URL != "https://discord.com/channels/800/901/m1" {
		t.Fatalf("%+v", res)
	}
	if len(f.posts) != 2 || f.posts[1].channel != "902" || f.posts[0].payload["content"] != "Hello" || f.posts[0].files != 2 {
		t.Fatalf("posts %+v", f.posts)
	}
	if att, _ := f.posts[0].payload["attachments"].([]any); len(att) != 2 {
		t.Fatalf("attachments %v", f.posts[0].payload)
	}

	reply, err := d.Reply(ctx, c, res.ExternalID, PostContent{Text: "More"}, Settings{"channel": "901,#news"})
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := f.posts[3].payload["message_reference"].(map[string]any)
	if reply.ExternalID != "901:m3,902:m4" || f.posts[3].channel != "902" || ref["message_id"] != "m2" || ref["fail_if_not_exists"] != false {
		t.Fatalf("reply %+v %v", reply, f.posts[3].payload)
	}
	if _, err := d.Reply(ctx, c, "", PostContent{Text: "More"}, Settings{}); !errors.Is(err, ErrRejected) {
		t.Fatalf("nothing to reply to: %v", err)
	}
	reg, _ := Get("discord")
	if err := reg.Validate(PostContent{Text: "a"}, Settings{}); err == nil {
		t.Fatal("a post needs its Discord channels")
	}
	if err := reg.Validate(PostContent{Text: strings.Repeat("a", 2001)}, Settings{"channel": "901"}); err == nil {
		t.Fatal("2001 characters")
	}
}

func TestDiscordFailures(t *testing.T) {
	f, ctx := useFakeDiscordBot(t)
	d := Discord{}
	c := Credentials{"guild_id": "800"}
	post := PostContent{Text: "Hello"}

	// A channel closed to the bot fails the post, not the connection.
	f.failIn["903"] = `{"message":"Missing Access","code":50001}`
	if _, err := d.Publish(ctx, c, post, Settings{"channel": "903"}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "can't post in that channel") {
		t.Fatalf("missing access: %v", err)
	}
	// After a channel that took it, the failure is final and says where it went.
	_, err := d.Publish(ctx, c, post, Settings{"channel": "901,903"})
	if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "published in 901") {
		t.Fatalf("partial: %v", err)
	}
	f.failIn["907"] = `{"message":"Unknown Channel","code":10003}`
	if _, err := d.Publish(ctx, c, post, Settings{"channel": "907"}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "gone") {
		t.Fatalf("unknown channel: %v", err)
	}
	if _, err := d.Publish(ctx, c, post, Settings{"channel": "#nope"}); !errors.Is(err, ErrRejected) {
		t.Fatalf("unknown name: %v", err)
	}
	if _, err := d.Publish(discordBotCtx("revoked"), c, post, Settings{"channel": "901"}); !errors.Is(err, ErrReauth) {
		t.Fatalf("bad bot token: %v", err)
	}
	f.rateLimit = true
	_, err = d.Publish(ctx, c, post, Settings{"channel": "901"})
	if err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) || RetryAt(err).Before(time.Now().Add(time.Second)) {
		t.Fatalf("rate limit: %v", err)
	}
	f.rateLimit = false
	if err := discordBotError(502, "", []byte("bad gateway"), false); errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
		t.Fatalf("outage: %v", err)
	}
	if err := discordBotError(413, "", []byte(`{"message":"Request entity too large","code":40005}`), false); !errors.Is(err, ErrRejected) {
		t.Fatalf("too large: %v", err)
	}
	if err := discordBotError(400, "", []byte(`{"message":"Invalid Form Body","code":50035,"errors":{"content":{}}}`), false); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "Invalid Form Body") {
		t.Fatalf("bad request: %v", err)
	}

	// The answer never came back: the message may be out.
	f.srv.Close()
	if _, err := d.Publish(ctx, c, post, Settings{"channel": "901"}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") {
		t.Fatalf("lost answer: %v", err)
	}
}

// Permissions come as strings (API v10) or, in older answers, numbers: both read the same.
func TestDiscordPermsDecode(t *testing.T) {
	var g discordGuild
	err := json.Unmarshal([]byte(`{"id":"1","roles":[{"id":"1","permissions":"309237763072"},{"id":"2","permissions":2048}]}`), &g)
	if err != nil || g.Roles[0].Permissions != 309237763072 || g.Roles[1].Permissions != 2048 {
		t.Fatalf("%+v %v", g, err)
	}
	// The largest permission set Discord can send (every bit) fits; a value that is not a number is an error.
	if err := json.Unmarshal([]byte(`{"roles":[{"id":"1","permissions":"18446744073709551615"}]}`), &g); err != nil || g.Roles[0].Permissions != 1<<64-1 {
		t.Fatalf("all bits: %+v %v", g, err)
	}
	if err := json.Unmarshal([]byte(`{"roles":[{"id":"1","permissions":"admin"}]}`), &g); err == nil {
		t.Fatal("a permission set that is not a number was read")
	}
}

// A channel named twice in the settings (by id and by #name, or the same id again) gets the post once.
func TestDiscordPostsOnceToAChannelNamedTwice(t *testing.T) {
	f, ctx := useFakeDiscordBot(t)
	res, err := Discord{}.Publish(ctx, Credentials{"guild_id": "800"}, PostContent{Text: "Hello"}, Settings{"channel": "901, #general,901,902"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.posts) != 2 || f.posts[0].channel != "901" || f.posts[1].channel != "902" || res.ExternalID != "901:m1,902:m2" {
		t.Fatalf("posts %+v, result %+v", f.posts, res)
	}
}

// Was a bug: a long answer was cut inside a character, and PostgreSQL refuses a stored error that is not UTF-8.
func TestDiscordErrorDetailStaysUTF8(t *testing.T) {
	body := strings.Repeat("a", 199) + strings.Repeat("ż", 50) // ż is two bytes: byte 200 is inside one
	for name, err := range map[string]error{
		"bot":     discordBotError(418, "", []byte(body), false),
		"webhook": discordError(418, []byte(body)),
		"errors":  discordError(400, []byte(`{"message":"Invalid Form Body","errors":"`+body+`"}`)),
	} {
		if !utf8.ValidString(err.Error()) || !strings.HasSuffix(err.Error(), "...") {
			t.Errorf("%s: %q", name, err)
		}
	}
}

// A rate limit with the wait only in the Retry-After header (no retry_after in the body) still waits that long.
func TestDiscordRateLimitReadsRetryAfterHeader(t *testing.T) {
	err := discordBotError(429, "30", []byte(`{"message":"You are being rate limited."}`), false)
	if errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) || RetryAt(err).Before(time.Now().Add(25*time.Second)) {
		t.Fatalf("rate limit: %v (retry at %v)", err, RetryAt(err))
	}
	// An unreadable header is no particular wait, not a crash or a time in the past.
	if err := discordBotError(429, "soon", nil, false); !RetryAt(err).IsZero() {
		t.Fatalf("unreadable Retry-After: %v", RetryAt(err))
	}
}

// The token endpoint's answers: an outage or a rate limit is tried again, anything else it refuses is final.
func TestDiscordOAuthErrors(t *testing.T) {
	for _, c := range []struct {
		status    int
		body      string
		temporary bool
		says      string
	}{
		{503, `upstream`, true, "unavailable"},
		{429, `{"retry_after":2}`, true, "rate limit"},
		{400, `{"error":"invalid_grant","error_description":"Invalid \"redirect_uri\" in request."}`, false, "return address"},
		{400, `{"error":"unsupported_grant_type"}`, false, "unsupported_grant_type"},
		{400, `not json`, false, "not json"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
		err := discordOAuthDo(context.Background(), req, &struct{}{})
		srv.Close()
		if err == nil || errors.Is(err, ErrRejected) == c.temporary || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%d %s: %v", c.status, c.body, err)
		}
	}
}
