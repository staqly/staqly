package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

var (
	_ Remover = Slack{}
	_ Remover = Discord{}
	_ Remover = DiscordWebhook{}
	_ Remover = Telegram{}
)

// Remove deletes every message the post made: ExternalID lists "<channel>:<ts>" per channel (a reply's
// "<channel>:<thread ts>/<reply ts>"; a post from before several channels holds the bare ts of its one channel).
// The app's token goes first; a message posted as the person needs their token.
func (Slack) Remove(ctx context.Context, c Credentials, externalID string) error {
	if c["access_token"] == "" {
		return &Failure{Kind: ErrReauth, Msg: "Slack: no sign-in stored; reconnect the workspace"}
	}
	var parts []slackPart
	if !strings.Contains(externalID, ":") {
		_, ts, _ := lastCut(externalID, "/")
		if ts != "" && c["channel_id"] != "" {
			parts = append(parts, slackPart{channel: c["channel_id"], ts: ts})
		}
	} else {
		for _, e := range strings.Split(externalID, ",") {
			ch, rest, _ := strings.Cut(e, ":")
			if _, ts, _ := lastCut(rest, "/"); ch != "" && ts != "" {
				parts = append(parts, slackPart{channel: ch, ts: ts})
			}
		}
	}
	if len(parts) == 0 {
		return &Failure{Kind: ErrRejected, Msg: "Slack: nothing to delete (the post has no id)"}
	}
	for _, p := range parts {
		err := slackDelete(ctx, c["access_token"], p)
		if err != nil && c["user_token"] != "" && strings.Contains(err.Error(), "(cant_delete_message)") {
			err = slackDelete(ctx, c["user_token"], p)
		}
		if err != nil {
			if strings.Contains(err.Error(), "(cant_delete_message)") {
				return &Failure{Kind: ErrManual, Msg: "Slack would not let the app delete the message in " + p.channel + ": delete it in Slack"}
			}
			return err
		}
	}
	return nil
}

// slackDelete is one chat.delete; a message that is gone already counts as deleted.
func slackDelete(ctx context.Context, token string, p slackPart) error {
	req, err := slackRequest(ctx, token, "chat.delete", map[string]any{"channel": p.channel, "ts": p.ts})
	if err != nil {
		return err
	}
	if err := slackDo(ctx, req, nil); err != nil && !strings.Contains(err.Error(), "(message_not_found)") {
		return err
	}
	return nil
}

// lastCut splits at the last sep: before, after, found (the whole string is "after" when sep is missing).
func lastCut(s, sep string) (before, after string, found bool) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return "", s, false
}

// Remove deletes the bot's message in every channel: ExternalID lists "<channel>:<message id>".
func (Discord) Remove(ctx context.Context, _ Credentials, externalID string) error {
	n := 0
	for _, e := range strings.Split(externalID, ",") {
		ch, id, ok := strings.Cut(e, ":")
		if !ok || ch == "" || id == "" {
			continue
		}
		n++
		token := strings.TrimSpace(getenv(ctx, discordBotEnv))
		if token == "" {
			return &Failure{Kind: ErrRejected, Msg: "Discord: this staqly server has no Discord bot token (" + discordBotEnv + "); ask whoever runs it to set it"}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, discordAPI+"/channels/"+url.PathEscape(ch)+"/messages/"+url.PathEscape(id), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bot "+token)
		if err := discordDelete(ctx, req, func(s int, h http.Header, raw []byte) error {
			return discordBotError(s, h.Get("Retry-After"), raw, false)
		}); err != nil {
			return err
		}
	}
	if n == 0 {
		return &Failure{Kind: ErrRejected, Msg: "Discord: nothing to delete (the post has no id)"}
	}
	return nil
}

// Remove deletes a message through its webhook: ExternalID is "<message id>" or "<thread id>/<message id>".
func (DiscordWebhook) Remove(ctx context.Context, c Credentials, externalID string) error {
	path, ok := discordWebhookPath(c["webhook_url"])
	if !ok {
		return &Failure{Kind: ErrReauth, Msg: "Discord: no webhook stored; reconnect the channel"}
	}
	thread, id, _ := lastCut(externalID, "/")
	if id == "" {
		return &Failure{Kind: ErrRejected, Msg: "Discord: nothing to delete (the post has no id)"}
	}
	u := discordAPI + "/webhooks/" + path + "/messages/" + url.PathEscape(id)
	if thread != "" {
		u += "?" + url.Values{"thread_id": {thread}}.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	// The URL holds the webhook's token: a transport error names the host only. 10015 is the webhook itself gone.
	return discordDelete(ctx, req, func(s int, _ http.Header, raw []byte) error { return discordError(s, raw) })
}

// discordDelete sends a DELETE; 2xx and "message or channel is gone" (404) are success.
func discordDelete(ctx context.Context, req *http.Request, fail func(int, http.Header, []byte) error) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return &Failure{Msg: "Discord could not be reached (" + req.URL.Host + ")"}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == http.StatusNotFound {
		var e struct {
			Code int `json:"code"`
		}
		_ = json.Unmarshal(raw, &e)
		// Unknown guild (bot removed) and unknown webhook mean the connection is broken, not the message gone.
		if e.Code != 10004 && e.Code != 10015 {
			return nil
		}
	}
	return fail(resp.StatusCode, resp.Header, raw)
}

// Remove deletes the message (Telegram refuses after 48 hours in some chats: the person deletes it).
func (Telegram) Remove(ctx context.Context, c Credentials, externalID string) error {
	token, err := telegramToken(c)
	if err != nil {
		return err
	}
	if token == "" || c["chat_id"] == "" {
		return &Failure{Kind: ErrReauth, Msg: "Telegram: no bot token stored; reconnect the channel"}
	}
	id, err := strconv.ParseInt(strings.TrimSpace(externalID), 10, 64)
	if err != nil {
		return &Failure{Kind: ErrRejected, Msg: "Telegram: nothing to delete (the post has no id)"}
	}
	err = telegramCall(ctx, token, "deleteMessage", map[string]any{"chat_id": c["chat_id"], "message_id": id}, nil)
	if err == nil {
		return nil
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "message to delete not found"):
		return nil
	case strings.Contains(lower, "can't be deleted"), strings.Contains(lower, "cant be deleted"):
		return &Failure{Kind: ErrManual, Msg: "Telegram would not let the bot delete the message (older than 48 hours, or no right to): delete it in Telegram"}
	}
	return err
}
