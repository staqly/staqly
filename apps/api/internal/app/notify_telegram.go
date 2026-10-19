package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"mime/multipart"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// Telegram: alerts as Bot API messages (sendMessage) to a chat the bot can
// post in. Nothing comes back from Telegram: there are no buttons to answer.

func (s *Service) telegramBase() string {
	return strings.TrimRight(s.Env("TELEGRAM_API_BASE", "https://api.telegram.org"), "/")
}

// tgButton is an inline button; alerts use URL buttons (Open in staqly).
type tgButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}
type tgMarkup struct {
	InlineKeyboard [][]tgButton `json:"inline_keyboard"`
}

// telegramCall invokes a Bot API method. Errors never include the token.
func (s *Service) telegramCall(ctx context.Context, ch *notifyChannel, method string, body any, out any) error {
	var resp struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	err := s.postJSON(ctx, s.telegramBase()+"/bot"+ch.cfg.BotToken+"/"+method, nil, body, &resp)
	return telegramResult(err, &resp.OK, resp.Result, resp.Description, out)
}

func telegramResult(err error, ok *bool, result json.RawMessage, desc string, out any) error {
	var he *httpError
	if errors.As(err, &he) && he.Status != 0 {
		var d struct {
			Description string `json:"description"`
		}
		if json.Unmarshal([]byte(he.Body), &d) == nil && d.Description != "" {
			return fmt.Errorf("telegram: %s", d.Description)
		}
	}
	if err != nil {
		return err
	}
	if !*ok {
		return fmt.Errorf("telegram: %s", notifyFirst(desc, "request failed"))
	}
	if out != nil && len(result) > 0 {
		return json.Unmarshal(result, out)
	}
	return nil
}

func tgEsc(s string) string { return html.EscapeString(s) }

// isPublicURL: Telegram refuses inline URL buttons pointing at localhost/LAN.
func isPublicURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if ip, err := netip.ParseAddr(h); err == nil {
		return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified())
	}
	return h != "localhost" && strings.Contains(h, ".") && !strings.HasSuffix(h, ".localhost") &&
		!strings.HasSuffix(h, ".local") && !strings.HasSuffix(h, ".internal")
}

func tgLink(u, label string) string {
	if isPublicURL(u) {
		return `<a href="` + tgEsc(u) + `">` + tgEsc(label) + `</a>`
	}
	return tgEsc(label) + ": " + tgEsc(u)
}

func telegramText(msg *notifyMessage, previewMax int) string {
	if msg.Headline != "" {
		return "🔔 " + tgEsc(msg.Headline)
	}
	var b strings.Builder
	if len(msg.Entries) == 1 {
		e := msg.Entries[0]
		fmt.Fprintf(&b, "🔔 <b>%s</b>\n<b>%s</b>\n<i>%s</i>", tgEsc(eventLabel(e)), tgEsc(e.Title), tgEsc(entryMeta(e)))
		if p := truncateRunes(e.Preview, previewMax); p != "" {
			b.WriteString("\n\n" + tgEsc(p))
		}
		if e.Note != "" {
			b.WriteString("\n\n📝 " + tgEsc(truncateRunes(e.Note, 300)))
		}
		if e.WebURL != "" {
			b.WriteString("\n\n" + tgLink(e.WebURL, "Open in staqly"))
		}
		return b.String()
	}
	fmt.Fprintf(&b, "🔔 <b>%d updates</b>", len(msg.Entries))
	for i, e := range msg.Entries {
		if i == 15 {
			fmt.Fprintf(&b, "\n\n...and %d more", len(msg.Entries)-15)
			break
		}
		fmt.Fprintf(&b, "\n\n<b>#%d %s</b>\n<i>%s · %s</i>", i+1, tgEsc(truncateRunes(e.Title, 120)), tgEsc(eventLabel(e)), tgEsc(entryMeta(e)))
		if p := truncateRunes(e.Preview, min(previewMax, 120)); p != "" {
			b.WriteString("\n" + tgEsc(p))
		}
	}
	return b.String()
}

// entryMeta is "type · collection".
func entryMeta(e notifyEntry) string {
	parts := []string{}
	for _, v := range []string{e.Channel, e.Collection} {
		if v != "" {
			parts = append(parts, v)
		}
	}
	if len(parts) == 0 {
		return e.EventType
	}
	return strings.Join(parts, " · ")
}

func telegramKeyboard(_ *notifyChannel, msg *notifyMessage) *tgMarkup {
	if len(msg.Entries) == 1 && isPublicURL(msg.Entries[0].WebURL) {
		return &tgMarkup{InlineKeyboard: [][]tgButton{{{Text: "🔗 Open", URL: msg.Entries[0].WebURL}}}}
	}
	return nil
}

func (s *Service) sendTelegram(ctx context.Context, ch *notifyChannel, msg *notifyMessage) error {
	kb := telegramKeyboard(ch, msg)
	if len(msg.Entries) == 1 {
		if img, name, _, ok := s.readImage(ctx, ch.row.WorkspaceID, msg.Entries[0], 10<<20); ok {
			caption := telegramText(msg, 500)
			if len([]rune(caption)) > 1024 {
				caption = telegramText(msg, 0)
			}
			return s.telegramPhoto(ctx, ch, caption, kb, img, name)
		}
	}
	body := map[string]any{"chat_id": ch.cfg.ChatID, "text": telegramText(msg, 1500), "parse_mode": "HTML",
		"link_preview_options": map[string]bool{"is_disabled": true}}
	if kb != nil {
		body["reply_markup"] = kb
	}
	return s.telegramCall(ctx, ch, "sendMessage", body, nil)
}

func (s *Service) telegramPhoto(ctx context.Context, ch *notifyChannel, caption string, kb *tgMarkup, img []byte, name string) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("chat_id", ch.cfg.ChatID)
	_ = w.WriteField("caption", caption)
	_ = w.WriteField("parse_mode", "HTML")
	if kb != nil {
		_ = w.WriteField("reply_markup", mustJSON(kb))
	}
	fw, _ := w.CreateFormFile("photo", cleanOriginalName(name))
	_, _ = fw.Write(img)
	_ = w.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.telegramBase()+"/bot"+ch.cfg.BotToken+"/sendPhoto", &buf)
	if err != nil {
		return &httpError{Body: "invalid Telegram API URL"}
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	var resp struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	err = s.doRequest(req, &resp)
	return telegramResult(err, &resp.OK, resp.Result, resp.Description, nil)
}
