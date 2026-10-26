package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Telegram over the Bot API: a bot made with @BotFather posts into a channel, a group or a chat it was added to.
// The person pastes the bot's token and names the chat; Connect checks that the bot may post there. Files are
// uploaded as bytes (sendPhoto, sendVideo, sendAnimation, sendMediaGroup), so no public link is needed. Thread
// parts are replies to the message before them. The token is part of every request URL: errors never quote it.
type Telegram struct{ Base }

// telegramAPI is the Bot API host, a variable so tests point it at httptest.
var telegramAPI = "https://api.telegram.org"

const (
	telegramCaptionMax = 1024     // a caption (the text of a post with media)
	telegramPhotoMax   = 10 << 20 // a picture uploaded through the Bot API
	telegramFileMax    = 50 << 20 // any other file uploaded through the Bot API
)

func init() {
	Register(Telegram{NewBase("telegram", "Telegram", AuthToken,
		Limits{MaxChars: 4096, MaxMedia: 10, MediaKinds: []string{"image", "video"}}).WithSettings(telegramSettings...)}, StatusAvailable)
}

func (Telegram) CredentialFields() []Field {
	return []Field{
		{Key: "bot_token", Label: "Bot token", Kind: "secret", Required: true, Placeholder: "123456789:AAH...",
			Help: "Message @BotFather in Telegram, send /newbot (or /token for an existing bot) and paste the token it gives."},
		{Key: "chat", Label: "Channel or group", Kind: "text", Required: true, Placeholder: "@janedoe_news",
			Help: "A public channel's @username, or the numeric id of a private channel or group (like -1001234567890). Add the bot as an administrator that may post messages first."},
	}
}

// telegramSettings: per-post settings.
var telegramSettings = []Field{
	{Key: "silent", Label: "Send silently", Kind: "boolean", Default: "false",
		Help: "Subscribers get the message without a sound."},
	{Key: "link_preview", Label: "Show a link preview", Kind: "boolean", Default: "true",
		Help: "Telegram shows a preview of the first link in a text-only post."},
}

func (t Telegram) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(t.Limits(), p, s); err != nil {
		return err
	}
	if err := telegramCheckMedia(p.Text, p.Media); err != nil {
		return err
	}
	return CheckFields(t.SettingsFields(), s)
}

// telegramCheckMedia: a caption is shorter than a message, a GIF goes alone, and the Bot API caps uploads.
// A long text with media is refused rather than split, so a post never goes out in two halves.
func telegramCheckMedia(text string, media []MediaFile) error {
	if len(media) == 0 {
		return nil
	}
	if n := utf8.RuneCountInString(text); n > telegramCaptionMax {
		return invalid("content", fmt.Sprintf("with media, Telegram takes at most %d characters (%d); shorten the text or post the media in a comment", telegramCaptionMax, n))
	}
	for _, m := range media {
		switch {
		case m.MimeType == "image/gif" && len(media) > 1:
			return invalid("media", "Telegram sends a GIF on its own; post "+m.Name+" alone")
		case strings.HasPrefix(m.MimeType, "image/") && m.MimeType != "image/gif" && m.Size > telegramPhotoMax:
			return invalid("media", m.Name+" is over 10 MB, the most Telegram takes for a picture")
		case m.Size > telegramFileMax:
			return invalid("media", m.Name+" is over 50 MB, the most a Telegram bot may upload")
		}
	}
	return nil
}

// telegramChat is what getChat says about the chat.
type telegramChat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"` // private, group, supergroup, channel
	Title    string `json:"title"`
	Username string `json:"username"`
	First    string `json:"first_name"`
	Last     string `json:"last_name"`
}

func (t Telegram) Connect(ctx context.Context, fields map[string]string) (Credentials, Profile, error) {
	token := strings.TrimSpace(fields["bot_token"])
	chat := strings.TrimSpace(fields["chat"])
	shared := fields["bot"] == "shared" // connected through the server's bot (/connect CODE in the chat)
	if shared {
		if token = telegramShared.token; token == "" {
			return nil, Profile{}, &Failure{Kind: ErrReauth, Msg: "Telegram: this server has no bot of its own any more; connect the chat with your own bot"}
		}
	}
	if token == "" {
		return nil, Profile{}, invalid("bot_token", "paste the bot token from @BotFather")
	}
	if chat == "" {
		return nil, Profile{}, invalid("chat", "name the channel or group, like @janedoe_news or -1001234567890")
	}
	chat = telegramChatRef(chat)
	var bot struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	if err := telegramCall(ctx, token, "getMe", map[string]any{}, &bot); err != nil {
		if errors.Is(err, ErrReauth) {
			return nil, Profile{}, invalid("bot_token", "Telegram does not accept this bot token; copy it again from @BotFather")
		}
		return nil, Profile{}, err
	}
	var c telegramChat
	if err := telegramCall(ctx, token, "getChat", map[string]any{"chat_id": chat}, &c); err != nil {
		if errors.Is(err, ErrReauth) || errors.Is(err, ErrRejected) {
			return nil, Profile{}, invalid("chat", "the bot cannot see this chat: check the name, and add @"+bot.Username+" to it ("+err.Error()+")")
		}
		return nil, Profile{}, err
	}
	var member struct {
		Status          string `json:"status"`
		CanPostMessages *bool  `json:"can_post_messages"`
		CanSendMessages *bool  `json:"can_send_messages"`
	}
	if err := telegramCall(ctx, token, "getChatMember", map[string]any{"chat_id": c.ID, "user_id": bot.ID}, &member); err != nil {
		if errors.Is(err, ErrReauth) || errors.Is(err, ErrRejected) {
			return nil, Profile{}, invalid("chat", "add @"+bot.Username+" to the chat first ("+err.Error()+")")
		}
		return nil, Profile{}, err
	}
	switch {
	case member.Status == "left", member.Status == "kicked":
		return nil, Profile{}, invalid("chat", "@"+bot.Username+" is not in this chat; add it first")
	case c.Type == "channel" && member.Status != "administrator" && member.Status != "creator":
		return nil, Profile{}, invalid("chat", "make @"+bot.Username+" an administrator of the channel, allowed to post messages")
	case c.Type == "channel" && member.CanPostMessages != nil && !*member.CanPostMessages:
		return nil, Profile{}, invalid("chat", "allow @"+bot.Username+" to post messages in the channel's administrator settings")
	case member.Status == "restricted" && member.CanSendMessages != nil && !*member.CanSendMessages:
		return nil, Profile{}, invalid("chat", "@"+bot.Username+" may not send messages in this group; lift the restriction")
	}

	id := strconv.FormatInt(c.ID, 10)
	creds := Credentials{"bot_token": token, "chat": chat, "chat_id": id, "chat_type": c.Type, "chat_username": c.Username}
	if shared { // the server's token is read when it is used, never stored with the channel
		delete(creds, "bot_token")
		creds["bot"] = "shared"
	}
	name := c.Title
	if name == "" {
		name = strings.TrimSpace(c.First + " " + c.Last)
	}
	profile := Profile{Name: name, Handle: id}
	if c.Username != "" {
		profile.Handle = "@" + c.Username
		profile.ProfileURL = "https://t.me/" + c.Username
	}
	return creds, profile, nil
}

// The server's own bot (PROVIDER_TELEGRAM_BOT_TOKEN), set by the app at start. A chat connected through it (the
// person adds the bot and sends /connect CODE there) holds "bot": "shared" instead of a token, so a new token for
// the same bot keeps every such channel working.
var telegramShared struct{ token, username, name string }

// SetTelegramBot records the server's bot: its token and its @username (without the @).
func SetTelegramBot(token, username string) {
	telegramShared.token, telegramShared.username = token, username
}

// SetTelegramBotName records the bot's display name ("Staqly"), shown beside its @username.
func SetTelegramBotName(name string) { telegramShared.name = name }

// TelegramBotName is the server's bot's display name, "" when unknown.
func TelegramBotName() string { return telegramShared.name }

// TelegramBot is the server's bot, "" when it has none.
func TelegramBot() (token, username string) { return telegramShared.token, telegramShared.username }

func telegramToken(c Credentials) string {
	if c["bot"] == "shared" {
		return telegramShared.token
	}
	return c["bot_token"]
}

// TelegramMe asks Telegram whose token this is: the bot's @username (without the @) and its display name.
func TelegramMe(ctx context.Context, token string) (username, name string, err error) {
	var bot struct {
		Username  string `json:"username"`
		FirstName string `json:"first_name"`
	}
	if err := telegramCall(ctx, token, "getMe", map[string]any{}, &bot); err != nil {
		return "", "", err
	}
	return bot.Username, bot.FirstName, nil
}

// TelegramCommand is a /connect CODE someone sent in a chat the server's bot is in.
type TelegramCommand struct {
	ChatID    int64
	MessageID int64
	Code      string
}

// /connect CODE, or /connect@thebot CODE as Telegram writes it in groups with several bots.
var telegramConnect = regexp.MustCompile(`(?i)^/connect(?:@\w+)?\s+([a-z0-9]{4,12})\s*$`)

// TelegramCommands waits up to `wait` seconds for the bot's new messages (getUpdates, long polling) and returns
// the /connect commands among them, with the offset that confirms them all.
func TelegramCommands(ctx context.Context, token string, offset int64, wait int) ([]TelegramCommand, int64, error) {
	type msg struct {
		MessageID int64  `json:"message_id"`
		Text      string `json:"text"`
		Chat      struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	}
	var updates []struct {
		UpdateID    int64 `json:"update_id"`
		Message     *msg  `json:"message"`
		ChannelPost *msg  `json:"channel_post"`
	}
	err := telegramCall(ctx, token, "getUpdates", map[string]any{"offset": offset, "timeout": wait,
		"allowed_updates": []string{"message", "channel_post"}}, &updates)
	if err != nil {
		return nil, offset, err
	}
	var out []TelegramCommand
	for _, u := range updates {
		offset = max(offset, u.UpdateID+1)
		m := u.Message
		if m == nil {
			m = u.ChannelPost
		}
		if m == nil {
			continue
		}
		if g := telegramConnect.FindStringSubmatch(strings.TrimSpace(m.Text)); g != nil {
			out = append(out, TelegramCommand{ChatID: m.Chat.ID, MessageID: m.MessageID, Code: strings.ToUpper(g[1])})
		}
	}
	return out, offset, nil
}

// TelegramDelete removes a message (the /connect command, once it worked); an error when the bot may not.
func TelegramDelete(ctx context.Context, token string, chatID, messageID int64) error {
	return telegramCall(ctx, token, "deleteMessage", map[string]any{"chat_id": chatID, "message_id": messageID}, nil)
}

// SetTelegramAPI points the Bot API somewhere else: a local Bot API server, or a test's.
func SetTelegramAPI(base string) { telegramAPI = base }

// TelegramSay sends a short note into a chat (why a /connect did not work).
func TelegramSay(ctx context.Context, token string, chatID int64, text string) {
	_ = telegramCall(ctx, token, "sendMessage", map[string]any{"chat_id": chatID, "text": text}, nil)
}

// telegramChatRef accepts @name, a bare name, a t.me link or a numeric id.
func telegramChatRef(v string) string {
	v = strings.TrimSuffix(strings.TrimSpace(v), "/")
	for _, prefix := range []string{"https://t.me/", "http://t.me/", "t.me/"} {
		if strings.HasPrefix(v, prefix) {
			v = strings.TrimPrefix(v, prefix)
		}
	}
	if _, err := strconv.ParseInt(v, 10, 64); err == nil {
		return v
	}
	return "@" + strings.TrimPrefix(v, "@")
}

// Telegram publishes a post's thread as replies.
var _ Replier = Telegram{}

func (t Telegram) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	return t.send(ctx, c, p, s, "")
}

// Reply sends a thread part as a reply to the message parentExternalID (the post, or the part before).
func (t Telegram) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, s Settings) (Result, error) {
	if parentExternalID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Telegram: nothing to reply to (the post has no id)"}
	}
	return t.send(ctx, c, p, s, parentExternalID)
}

type telegramMessage struct {
	MessageID int64 `json:"message_id"`
}

func (t Telegram) send(ctx context.Context, c Credentials, p PostContent, s Settings, replyTo string) (Result, error) {
	token, chat := telegramToken(c), c["chat_id"]
	if token == "" || chat == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Telegram: no bot token stored; reconnect the channel"}
	}
	if err := telegramCheckMedia(p.Text, p.Media); err != nil {
		var v *ValidationError
		errors.As(err, &v)
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Telegram: " + v.Msg}
	}
	params := map[string]any{"chat_id": chat}
	if s["silent"] == "true" {
		params["disable_notification"] = true
	}
	if replyTo != "" {
		id, err := strconv.ParseInt(replyTo, 10, 64)
		if err != nil {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Telegram: nothing to reply to (the post's id is not a message id)"}
		}
		// A reply still goes out if the message before it was deleted.
		params["reply_parameters"] = map[string]any{"message_id": id, "allow_sending_without_reply": true}
	}

	var id int64
	var err error
	switch len(p.Media) {
	case 0:
		params["text"] = p.Text
		if s["link_preview"] == "false" {
			params["link_preview_options"] = map[string]any{"is_disabled": true}
		}
		var m telegramMessage
		err = telegramCall(ctx, token, "sendMessage", params, &m)
		id = m.MessageID
	case 1:
		media := p.Media[0]
		method, field := "sendPhoto", "photo"
		switch {
		case strings.HasPrefix(media.MimeType, "video/"):
			method, field = "sendVideo", "video"
			params["supports_streaming"] = true
		case media.MimeType == "image/gif":
			method, field = "sendAnimation", "animation"
		}
		if p.Text != "" {
			params["caption"] = p.Text
		}
		params[field] = "attach://file0"
		var m telegramMessage
		err = telegramUpload(ctx, token, method, params, p.Media, &m)
		id = m.MessageID
	default:
		group := make([]map[string]any, len(p.Media))
		for i, m := range p.Media {
			kind := "photo"
			if strings.HasPrefix(m.MimeType, "video/") {
				kind = "video"
			}
			group[i] = map[string]any{"type": kind, "media": "attach://file" + strconv.Itoa(i)}
		}
		if p.Text != "" {
			group[0]["caption"] = p.Text // an album shows the first file's caption
		}
		params["media"] = group
		var ms []telegramMessage
		err = telegramUpload(ctx, token, "sendMediaGroup", params, p.Media, &ms)
		if len(ms) > 0 {
			id = ms[0].MessageID
		}
	}
	if err != nil {
		// The Bot API has no idempotency key: sending again after an answer that never came could post twice.
		var f *Failure
		if !errors.As(err, &f) && telegramSent(err) {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Telegram may have published the post, but its answer was lost (" + err.Error() + "); check the channel before posting it again"}
		}
		return Result{}, err
	}
	if id == 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Telegram published the post but did not say its id; check the channel"}
	}
	msg := strconv.FormatInt(id, 10)
	return Result{ExternalID: msg, URL: telegramLink(c, msg)}, nil
}

// telegramLink is the message's t.me link: by @username for a public chat, by id for a private channel or
// supergroup (members only), none for a private chat or a basic group.
func telegramLink(c Credentials, msg string) string {
	if u := c["chat_username"]; u != "" {
		return "https://t.me/" + u + "/" + msg
	}
	if id := c["chat_id"]; strings.HasPrefix(id, "-100") {
		return "https://t.me/c/" + strings.TrimPrefix(id, "-100") + "/" + msg
	}
	return ""
}

// errTelegramUnsent marks a failure before the request left (reading a file, building the body): safe to retry.
type errTelegramUnsent struct{ error }

// telegramSent: the request may have reached Telegram.
func telegramSent(err error) bool {
	var u errTelegramUnsent
	return !errors.As(err, &u)
}

// telegramCall sends a JSON request to a Bot API method and decodes "result" into out.
func telegramCall(ctx context.Context, token, method string, params map[string]any, out any) error {
	b, err := json.Marshal(params)
	if err != nil {
		return errTelegramUnsent{err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, telegramAPI+"/bot"+token+"/"+method, bytes.NewReader(b))
	if err != nil {
		return errTelegramUnsent{errors.New("Telegram: the bot token is not valid")}
	}
	req.Header.Set("Content-Type", "application/json")
	return telegramDo(ctx, req, out)
}

// telegramUpload sends a multipart request: the parameters as fields (objects as JSON), file i as "file<i>".
func telegramUpload(ctx context.Context, token, method string, params map[string]any, files []MediaFile, out any) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range params {
		s, ok := v.(string)
		if !ok {
			b, err := json.Marshal(v)
			if err != nil {
				return errTelegramUnsent{err}
			}
			s = string(b)
		}
		_ = w.WriteField(k, s)
	}
	for i, m := range files {
		f, err := m.Reader(ctx)
		if err != nil {
			return errTelegramUnsent{fmt.Errorf("Telegram: cannot read %s: %w", m.Name, err)}
		}
		part, err := w.CreateFormFile("file"+strconv.Itoa(i), m.Name)
		if err == nil {
			_, err = io.Copy(part, f)
		}
		_ = f.Close()
		if err != nil {
			return errTelegramUnsent{fmt.Errorf("Telegram: cannot read %s: %w", m.Name, err)}
		}
	}
	if err := w.Close(); err != nil {
		return errTelegramUnsent{err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, telegramAPI+"/bot"+token+"/"+method, &body)
	if err != nil {
		return errTelegramUnsent{errors.New("Telegram: the bot token is not valid")}
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	return telegramDo(ctx, req, out)
}

// telegramDo sends a request and decodes the answer's "result" into out; Telegram's errors become messages a
// person can act on. The URL holds the bot token, so a transport error names the host only.
func telegramDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("Telegram could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var env struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if json.Unmarshal(raw, &env) != nil {
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return &Failure{Msg: fmt.Sprintf("Telegram is unavailable (%d); staqly tries again in a few minutes", resp.StatusCode)}
		}
		return fmt.Errorf("Telegram answered %d without a readable reply", resp.StatusCode)
	}
	if env.OK {
		if out != nil && len(env.Result) > 0 {
			return json.Unmarshal(env.Result, out)
		}
		return nil
	}
	status := env.ErrorCode
	if status == 0 {
		status = resp.StatusCode
	}
	return telegramError(status, env.Description, env.Parameters.RetryAfter)
}

// telegramError maps the Bot API's errors to what staqly does next: a bot that lost its token or its place in the
// chat needs reconnecting, a refused message is final, flood limits and outages are retried.
func telegramError(status int, desc string, retryAfter int) error {
	lower := strings.ToLower(desc)
	has := func(s string) bool { return strings.Contains(lower, s) }
	rejected := func(msg string) error { return &Failure{Kind: ErrRejected, Msg: "Telegram: " + msg} }
	reauth := func(msg string) error { return &Failure{Kind: ErrReauth, Msg: "Telegram: " + msg} }
	switch {
	case status == http.StatusNotFound && !has("message"):
		// A malformed token is answered 404 for the whole URL.
		return reauth("the bot token is not valid; reconnect the channel with the token from @BotFather")
	case status == http.StatusUnauthorized:
		return reauth("the bot token no longer works (revoked in @BotFather?); reconnect the channel with the new one")
	case status == http.StatusTooManyRequests:
		if retryAfter > 0 {
			return &Failure{Msg: fmt.Sprintf("Telegram rate limit reached; staqly tries again in %d s", retryAfter),
				RetryAt: time.Now().Add(time.Duration(retryAfter) * time.Second)}
		}
		return &Failure{Msg: "Telegram rate limit reached; staqly tries again in a few minutes"}
	case status >= 500:
		return &Failure{Msg: fmt.Sprintf("Telegram is unavailable (%d); staqly tries again in a few minutes", status)}
	case has("bot was kicked"), has("bot is not a member"), has("bot was blocked"), has("chat not found"),
		has("not enough rights"), has("need administrator rights"), has("have no rights"), has("chat_write_forbidden"):
		return reauth("the bot may no longer post in this chat (" + desc + "); add it back with posting rights, then reconnect the channel")
	case has("group chat was upgraded"):
		return reauth("the group became a supergroup with a new id; reconnect the channel")
	case has("message is too long"), has("caption is too long"):
		return rejected("the text is too long (" + desc + ")")
	case has("too big"), has("file is too"):
		return rejected("a file is too large for a Telegram bot (" + desc + ")")
	case status == http.StatusForbidden, status == http.StatusBadRequest:
		return rejected(desc)
	}
	return &Failure{Msg: fmt.Sprintf("Telegram answered %d: %s", status, desc)}
}
