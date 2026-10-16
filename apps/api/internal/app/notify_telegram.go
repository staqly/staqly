package app

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"mime/multipart"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Telegram: Bot API messages with inline Approve / Reject / "Reject with
// reason" buttons. Callbacks arrive either by long polling (getUpdates, the
// default: works on a laptop without a public URL) or by webhook
// (POST /v1/integrations/telegram/{channel_id} with Telegram's secret_token).

func (s *Service) telegramBase() string {
	return strings.TrimRight(s.Env("TELEGRAM_API_BASE", "https://api.telegram.org"), "/")
}

type tgUser struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

type tgButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

type tgMarkup struct {
	InlineKeyboard [][]tgButton `json:"inline_keyboard"`
}

type tgMessage struct {
	MessageID int64   `json:"message_id"`
	From      *tgUser `json:"from"`
	Chat      struct {
		ID       int64  `json:"id"`
		Type     string `json:"type"`
		Username string `json:"username"`
	} `json:"chat"`
	Text           string     `json:"text"`
	Caption        string     `json:"caption"`
	ReplyToMessage *tgMessage `json:"reply_to_message"`
	ReplyMarkup    *tgMarkup  `json:"reply_markup"`
}

type tgUpdate struct {
	UpdateID      int64      `json:"update_id"`
	Message       *tgMessage `json:"message"`
	CallbackQuery *struct {
		ID      string     `json:"id"`
		From    tgUser     `json:"from"`
		Message *tgMessage `json:"message"`
		Data    string     `json:"data"`
	} `json:"callback_query"`
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
	for _, v := range []string{e.ItemType, e.Collection} {
		if v != "" {
			parts = append(parts, v)
		}
	}
	if len(parts) == 0 {
		return e.EventType
	}
	return strings.Join(parts, " · ")
}

func telegramKeyboard(ch *notifyChannel, msg *notifyMessage) *tgMarkup {
	kb := &tgMarkup{InlineKeyboard: [][]tgButton{}}
	idx := msg.actionable()
	single := len(msg.Entries) == 1
	for n, i := range idx {
		if n == 10 {
			break
		}
		e := msg.Entries[i]
		btn := func(v, label string) tgButton {
			p := v + "|" + itemRef(e.ItemID, e.Version) // decide exactly this version
			return tgButton{Text: label, CallbackData: p + "|" + callbackSig(ch.sec.CallbackKey, p)}
		}
		if single {
			kb.InlineKeyboard = append(kb.InlineKeyboard,
				[]tgButton{btn("a", "✅ Approve"), btn("r", "❌ Reject")},
				[]tgButton{btn("f", "💬 Reject with reason")})
		} else {
			num := "#" + strconv.Itoa(i+1)
			kb.InlineKeyboard = append(kb.InlineKeyboard, []tgButton{btn("a", "✅ "+num), btn("r", "❌ "+num), btn("f", "💬 "+num)})
		}
	}
	if single && isPublicURL(msg.Entries[0].WebURL) {
		kb.InlineKeyboard = append(kb.InlineKeyboard, []tgButton{{Text: "🔗 Open", URL: msg.Entries[0].WebURL}})
	}
	if len(kb.InlineKeyboard) == 0 {
		return nil
	}
	return kb
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

// ---- receiving updates ----

// RunTelegramPoller long-polls getUpdates for every enabled Telegram channel
// in polling mode.
func (s *Service) RunTelegramPoller(ctx context.Context) {
	tick := time.NewTicker(s.envDuration("TELEGRAM_POLL_INTERVAL", 3*time.Second))
	defer tick.Stop()
	for {
		s.PollTelegramOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// PollTelegramOnce fetches and handles pending updates for each polling channel.
func (s *Service) PollTelegramOnce(ctx context.Context) {
	rows, err := s.Q.ListEnabledNotifyChannelsByType(ctx, ChannelTelegram)
	if err != nil {
		return
	}
	for _, r := range rows {
		ch, err := s.loadChannel(r)
		if err != nil || ch.cfg.Mode == "webhook" {
			continue
		}
		var updates []tgUpdate
		err = s.telegramCall(ctx, ch, "getUpdates", map[string]any{"offset": r.TelegramOffset, "timeout": 0,
			"allowed_updates": []string{"message", "callback_query"}}, &updates)
		if err != nil {
			if ctx.Err() == nil && r.LastError != "getUpdates: "+err.Error() {
				_ = s.W.SetNotifyChannelError(ctx, db.SetNotifyChannelErrorParams{LastError: "getUpdates: " + err.Error(), ID: r.ID})
			}
			continue
		}
		offset := r.TelegramOffset
		for _, u := range updates {
			s.handleTelegramUpdate(ctx, ch, u)
			offset = max(offset, u.UpdateID+1)
		}
		if offset != r.TelegramOffset {
			_ = s.W.SetTelegramOffset(ctx, db.SetTelegramOffsetParams{TelegramOffset: offset, ID: r.ID})
		}
	}
}

// HandleTelegramWebhook processes one update delivered by Telegram to
// /v1/integrations/telegram/{channel_id}.
func (s *Service) HandleTelegramWebhook(ctx context.Context, channelID, secret string, body []byte) error {
	ch, err := s.channelByID(ctx, channelID, ChannelTelegram)
	if err != nil {
		return err
	}
	if ch.cfg.Mode != "webhook" || ch.sec.TelegramToken == "" ||
		subtle.ConstantTimeCompare([]byte(secret), []byte(ch.sec.TelegramToken)) != 1 {
		return ErrUnauthorized
	}
	var u tgUpdate
	if err := json.Unmarshal(body, &u); err != nil {
		return invalid("body", "invalid update")
	}
	s.handleTelegramUpdate(ctx, ch, u)
	return nil
}

func (s *Service) tgChatMatches(ch *notifyChannel, m *tgMessage) bool {
	if m == nil {
		return false
	}
	if strings.HasPrefix(ch.cfg.ChatID, "@") {
		return strings.EqualFold("@"+m.Chat.Username, ch.cfg.ChatID)
	}
	return strconv.FormatInt(m.Chat.ID, 10) == ch.cfg.ChatID
}

// tgAllowed: the allowlist when set; otherwise only the private chat's owner.
func tgAllowed(ch *notifyChannel, userID int64) bool {
	id := strconv.FormatInt(userID, 10)
	if len(ch.cfg.ApproverUserIDs) > 0 {
		return approverAllowed(ch.cfg.ApproverUserIDs, id)
	}
	return id == ch.cfg.ChatID
}

func tgActor(u tgUser) (string, string) {
	name := u.FirstName
	if u.Username != "" {
		name = "@" + u.Username
	}
	if name == "" {
		name = strconv.FormatInt(u.ID, 10)
	}
	return "telegram:" + strconv.FormatInt(u.ID, 10), "Telegram: " + name
}

// reTgRef finds the signed item reference in a "reason" prompt:
// "ref: itm_x.7.<sig>" (version-bound) or the older "ref: itm_x.<sig>".
var reTgRef = regexp.MustCompile(`ref: (itm_[a-z0-9]+(?:\.[0-9]+)?)\.([A-Za-z0-9_-]{11})`)

func (s *Service) handleTelegramUpdate(ctx context.Context, ch *notifyChannel, u tgUpdate) {
	if cq := u.CallbackQuery; cq != nil {
		s.handleTelegramCallback(ctx, ch, cq.ID, cq.From, cq.Message, cq.Data)
		return
	}
	m := u.Message
	if m == nil || m.From == nil || m.From.IsBot {
		return
	}
	if strings.HasPrefix(m.Text, "/start") || strings.HasPrefix(m.Text, "/chatid") {
		_ = s.telegramCall(ctx, ch, "sendMessage", map[string]any{"chat_id": m.Chat.ID,
			"text": "staqly is connected. This chat's ID is " + strconv.FormatInt(m.Chat.ID, 10) + " and your user ID is " + strconv.FormatInt(m.From.ID, 10) + "."}, nil)
		return
	}
	// A reply to our "reason for rejecting" prompt.
	rt := m.ReplyToMessage
	if rt == nil || rt.From == nil || !rt.From.IsBot || !s.tgChatMatches(ch, m) {
		return
	}
	botID, _, _ := strings.Cut(ch.cfg.BotToken, ":")
	if strconv.FormatInt(rt.From.ID, 10) != botID {
		return
	}
	ref := reTgRef.FindStringSubmatch(rt.Text)
	if ref == nil || !verifyCallbackSig(ch.sec.CallbackKey, "f|"+ref[1], ref[2]) {
		return
	}
	reply := func(text string) {
		_ = s.telegramCall(ctx, ch, "sendMessage", map[string]any{"chat_id": m.Chat.ID, "text": text,
			"reply_parameters": map[string]any{"message_id": m.MessageID}}, nil)
	}
	if !tgAllowed(ch, m.From.ID) {
		reply("You are not on this channel's approver list.")
		return
	}
	feedback := strings.TrimSpace(m.Text)
	if feedback == "" {
		reply("Send the reason as text.")
		return
	}
	id, name := tgActor(*m.From)
	itemID, version := parseItemRef(ref[1])
	_, outcome, _ := s.chatDecide(ctx, ch, id, name, itemID, "reject", truncateRunes(feedback, 4900), version)
	reply(outcome)
}

func (s *Service) handleTelegramCallback(ctx context.Context, ch *notifyChannel, cbID string, from tgUser, m *tgMessage, data string) {
	answer := func(text string, alert bool) {
		_ = s.telegramCall(ctx, ch, "answerCallbackQuery", map[string]any{"callback_query_id": cbID, "text": text, "show_alert": alert}, nil)
	}
	parts := strings.Split(data, "|")
	if len(parts) != 3 || !s.tgChatMatches(ch, m) || !verifyCallbackSig(ch.sec.CallbackKey, parts[0]+"|"+parts[1], parts[2]) {
		answer("", false)
		return
	}
	verb := parts[0]
	itemID, version := parseItemRef(parts[1])
	if !tgAllowed(ch, from.ID) {
		answer("You are not on this channel's approver list.", true)
		return
	}
	switch verb {
	case "f":
		title := itemID
		if it, err := s.Q.GetItem(ctx, db.GetItemParams{WorkspaceID: ch.row.WorkspaceID, ID: itemID}); err == nil {
			if it.Status != "pending" {
				answer("Already "+it.Status, false)
				s.tgMarkDone(ctx, ch, m, itemID, "Already "+it.Status)
				return
			}
			if version > 0 && it.Version != version {
				answer("⚠️ "+errItemChanged.Message, false)
				return
			}
			title = it.Title
		}
		// The prompt carries the signed, version-bound ref; the reply decides
		// exactly the version the original message showed.
		ref := itemRef(itemID, version)
		_ = s.telegramCall(ctx, ch, "sendMessage", map[string]any{"chat_id": m.Chat.ID,
			"text":             "✍️ Reply to this message with the reason for rejecting “" + truncateRunes(title, 100) + "”.\nref: " + ref + "." + callbackSig(ch.sec.CallbackKey, "f|"+ref),
			"reply_parameters": map[string]any{"message_id": m.MessageID},
			"reply_markup":     map[string]any{"force_reply": true, "selective": true, "input_field_placeholder": "Reason for rejecting"}}, nil)
		answer("Reply with the reason", false)
	case "a", "r":
		action := map[string]string{"a": "approve", "r": "reject"}[verb]
		id, name := tgActor(from)
		_, outcome, err := s.chatDecide(ctx, ch, id, name, itemID, action, "", version)
		answer(outcome, false)
		if err == nil || isConflict(err) {
			s.tgMarkDone(ctx, ch, m, itemID, outcome)
		}
	default:
		answer("", false)
	}
}

// tgMarkDone replaces the item's button rows with one inert "outcome" button.
func (s *Service) tgMarkDone(ctx context.Context, ch *notifyChannel, m *tgMessage, itemID, outcome string) {
	if m == nil || m.ReplyMarkup == nil {
		return
	}
	kb := tgMarkup{InlineKeyboard: [][]tgButton{}}
	placed := false
	for _, row := range m.ReplyMarkup.InlineKeyboard {
		mine := false
		for _, b := range row {
			if refMatches(b.CallbackData, itemID) {
				mine = true
			}
		}
		if !mine {
			kb.InlineKeyboard = append(kb.InlineKeyboard, row)
			continue
		}
		if !placed {
			kb.InlineKeyboard = append(kb.InlineKeyboard, []tgButton{{Text: outcome, CallbackData: "n"}})
			placed = true
		}
	}
	_ = s.telegramCall(ctx, ch, "editMessageReplyMarkup", map[string]any{"chat_id": m.Chat.ID, "message_id": m.MessageID,
		"reply_markup": kb}, nil)
}
