package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Discord: messages through a channel webhook (one-tap links in the text),
// or — with an application (bot token, channel, application ID, public key) —
// as a bot message with buttons handled at
// POST /v1/integrations/discord/interactions (Ed25519-verified).

func (s *Service) discordBase() string {
	return strings.TrimRight(s.Env("DISCORD_API_BASE", "https://discord.com/api/v10"), "/")
}

var discordEscRe = regexp.MustCompile(`([\\*_~` + "`" + `|>\[\]()#<@])`)

// discordEsc neutralizes markdown, links and mentions in untrusted text.
func discordEsc(s string) string { return discordEscRe.ReplaceAllString(s, `\$1`) }

var reFileName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func discordEmbeds(ch *notifyChannel, msg *notifyMessage, image string) ([]map[string]any, string) {
	if msg.Headline != "" {
		return nil, "🔔 " + discordEsc(msg.Headline)
	}
	var embeds []map[string]any
	content := ""
	if len(msg.Entries) > 1 {
		content = fmt.Sprintf("🔔 **%d updates in staqly**", len(msg.Entries))
	}
	for i, e := range msg.Entries {
		if i == 10 {
			content += fmt.Sprintf("\n...and %d more", len(msg.Entries)-10)
			break
		}
		title := eventLabel(e) + ": " + discordEsc(e.Title)
		if len(msg.Entries) > 1 {
			title = "#" + strconv.Itoa(i+1) + " " + title
		}
		desc := ""
		previewMax := 1500
		if len(msg.Entries) > 1 {
			previewMax = 300
		}
		if p := truncateRunes(e.Preview, previewMax); p != "" {
			desc = discordEsc(p)
		}
		if e.Note != "" {
			desc += "\n\n📝 " + discordEsc(truncateRunes(e.Note, 300))
		}
		if e.ApproveURL != "" {
			desc += "\n\n[✅ Approve...](" + e.ApproveURL + ")  ·  [❌ Reject...](" + e.RejectURL + ")"
		}
		if e.WebURL != "" && !isPublicURL(e.WebURL) {
			desc += "\nOpen: " + e.WebURL
		}
		em := map[string]any{"title": truncateRunes(title, 250), "description": strings.TrimSpace(desc),
			"footer": map[string]any{"text": truncateRunes(entryMeta(e), 200)}, "color": 0x6366f1}
		if isPublicURL(e.WebURL) {
			em["url"] = e.WebURL
		}
		if image != "" && i == 0 {
			em["image"] = map[string]any{"url": "attachment://" + image}
		}
		embeds = append(embeds, em)
	}
	return embeds, content
}

func discordComponents(ch *notifyChannel, msg *notifyMessage) []any {
	if !ch.discordApp() {
		return nil
	}
	var rows []any
	for n, i := range msg.actionable() {
		if n == 5 {
			break
		}
		e := msg.Entries[i]
		suffix := ""
		if len(msg.Entries) > 1 {
			suffix = " #" + strconv.Itoa(i+1)
		}
		btn := func(verb, label string, style int) map[string]any {
			ref := itemRef(e.ItemID, e.Version) // decide exactly this version
			p := verb + "|" + ref
			return map[string]any{"type": 2, "style": style, "label": label + suffix,
				"custom_id": "sq|" + verb + "|" + ch.row.ID + "|" + ref + "|" + callbackSig(ch.sec.CallbackKey, p)}
		}
		rows = append(rows, map[string]any{"type": 1, "components": []any{
			btn("a", "✅ Approve", 3), btn("r", "❌ Reject", 4), btn("f", "💬 Reject with reason", 2)}})
	}
	return rows
}

func (s *Service) sendDiscord(ctx context.Context, ch *notifyChannel, msg *notifyMessage) error {
	var img []byte
	var imgName string
	if len(msg.Entries) == 1 {
		if b, name, mime, ok := s.readImage(ctx, ch.row.WorkspaceID, msg.Entries[0], 8<<20); ok {
			ext := ".png"
			if exts, ok := allowedMedia[mime]; ok {
				ext = exts
			}
			img, imgName = b, strings.TrimSuffix(reFileName.ReplaceAllString(name, "_"), ext)
			imgName = truncateRunes(imgName, 60) + ext
			imgName = strings.ReplaceAll(imgName, "...", "")
		}
	}
	embeds, content := discordEmbeds(ch, msg, imgName)
	body := map[string]any{"content": content, "allowed_mentions": map[string]any{"parse": []string{}}}
	if len(embeds) > 0 {
		body["embeds"] = embeds
	}
	if comps := discordComponents(ch, msg); len(comps) > 0 {
		body["components"] = comps
	}
	var target string
	hdr := map[string]string{}
	if ch.discordApp() || ch.cfg.WebhookURL == "" {
		target = s.discordBase() + "/channels/" + ch.cfg.Channel + "/messages"
		hdr["Authorization"] = "Bot " + ch.cfg.BotToken
	} else {
		target = ch.cfg.WebhookURL
		if strings.Contains(target, "?") {
			target += "&wait=true"
		} else {
			target += "?wait=true"
		}
	}
	if img == nil {
		return s.postJSON(ctx, target, hdr, body, nil)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	pj, _ := w.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="payload_json"`}, "Content-Type": {"application/json"}})
	_, _ = pj.Write([]byte(mustJSON(body)))
	fw, _ := w.CreateFormFile("files[0]", imgName)
	_, _ = fw.Write(img)
	_ = w.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, &buf)
	if err != nil {
		return &httpError{Body: "invalid Discord URL"}
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("User-Agent", "staqly-notify/1.0")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return s.doRequest(req, nil)
}

// VerifyDiscordSignature checks X-Signature-Ed25519 over timestamp+body with
// the application's public key, within a 5-minute window.
func VerifyDiscordSignature(publicKeyHex string, now time.Time, ts, sigHex string, body []byte) bool {
	pk, err := hex.DecodeString(publicKeyHex)
	if err != nil || len(pk) != ed25519.PublicKeySize {
		return false
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	if d := now.Unix() - n; d > 300 || d < -300 {
		return false
	}
	return ed25519.Verify(pk, append([]byte(ts), body...), sig)
}

type discordUser struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
}

type discordInteraction struct {
	Type          int    `json:"type"`
	ApplicationID string `json:"application_id"`
	Data          struct {
		CustomID   string `json:"custom_id"`
		Components []struct {
			Components []struct {
				CustomID string `json:"custom_id"`
				Value    string `json:"value"`
			} `json:"components"`
		} `json:"components"`
	} `json:"data"`
	Member *struct {
		User discordUser `json:"user"`
	} `json:"member"`
	User    *discordUser `json:"user"`
	Message *struct {
		Components []map[string]any `json:"components"`
	} `json:"message"`
}

func ephemeral(text string) map[string]any {
	return map[string]any{"type": 4, "data": map[string]any{"content": text, "flags": 64, "allowed_mentions": map[string]any{"parse": []string{}}}}
}

// HandleDiscordInteraction verifies and answers one interaction request. It
// returns the JSON response body for Discord.
func (s *Service) HandleDiscordInteraction(ctx context.Context, body []byte, sigHex, ts string) (any, error) {
	var in discordInteraction
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, ErrUnauthorized
	}
	var ch *notifyChannel
	var parts []string
	if in.Data.CustomID != "" {
		parts = strings.Split(in.Data.CustomID, "|")
		var chID string
		switch {
		case len(parts) == 5 && parts[0] == "sq":
			chID = parts[2]
		case len(parts) == 4 && parts[0] == "sqm":
			chID = parts[1]
		default:
			return nil, ErrUnauthorized
		}
		c, err := s.channelByID(ctx, chID, ChannelDiscord)
		if err != nil || !c.discordApp() || !VerifyDiscordSignature(c.cfg.PublicKey, s.Now(), ts, sigHex, body) {
			return nil, ErrUnauthorized
		}
		ch = c
	} else {
		rows, err := s.Q.ListEnabledNotifyChannelsByType(ctx, ChannelDiscord)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			// Match the (public) application id first: only the candidate
			// channel's secrets get decrypted.
			var pub struct {
				ApplicationID string `json:"application_id"`
			}
			if json.Unmarshal([]byte(r.Config), &pub) != nil || in.ApplicationID == "" || pub.ApplicationID != in.ApplicationID {
				continue
			}
			c, err := s.loadChannel(r)
			if err == nil && c.discordApp() && c.cfg.ApplicationID == in.ApplicationID &&
				VerifyDiscordSignature(c.cfg.PublicKey, s.Now(), ts, sigHex, body) {
				ch = c
				break
			}
		}
		if ch == nil {
			return nil, ErrUnauthorized
		}
	}
	if in.Type == 1 { // PING
		return map[string]any{"type": 1}, nil
	}
	user := in.User
	if in.Member != nil {
		user = &in.Member.User
	}
	if user == nil || !approverAllowed(ch.cfg.ApproverUserIDs, user.ID) {
		return ephemeral("You are not on this channel's approver list."), nil
	}
	actorID, actorName := "discord:"+user.ID, "Discord: "+notifyFirst(user.GlobalName, user.Username, user.ID)
	switch {
	case in.Type == 3 && len(parts) == 5 && parts[0] == "sq":
		verb := parts[1]
		if !verifyCallbackSig(ch.sec.CallbackKey, verb+"|"+parts[3], parts[4]) {
			return ephemeral("Invalid button."), nil
		}
		itemID, version := parseItemRef(parts[3])
		switch verb {
		case "f":
			if cur, err := s.Q.GetItem(ctx, db.GetItemParams{WorkspaceID: ch.row.WorkspaceID, ID: itemID}); err == nil &&
				cur.Status == "pending" && version > 0 && cur.Version != version {
				return ephemeral("⚠️ " + errItemChanged.Message), nil
			}
			// The modal keeps the signed, version-bound ref of the button.
			return map[string]any{"type": 9, "data": map[string]any{
				"custom_id": "sqm|" + ch.row.ID + "|" + parts[3] + "|" + parts[4], "title": "Reject with reason",
				"components": []any{map[string]any{"type": 1, "components": []any{map[string]any{
					"type": 4, "custom_id": "reason", "style": 2, "label": "Reason", "required": true, "max_length": 2000}}}}}}, nil
		case "a", "r":
			_, outcome, err := s.chatDecide(ctx, ch, actorID, actorName, itemID, map[string]string{"a": "approve", "r": "reject"}[verb], "", version)
			return discordUpdate(in, itemID, outcome, err), nil
		}
	case in.Type == 5 && len(parts) == 4 && parts[0] == "sqm":
		if !verifyCallbackSig(ch.sec.CallbackKey, "f|"+parts[2], parts[3]) {
			return ephemeral("Invalid form."), nil
		}
		itemID, version := parseItemRef(parts[2])
		reason := ""
		for _, row := range in.Data.Components {
			for _, c := range row.Components {
				if c.CustomID == "reason" {
					reason = strings.TrimSpace(c.Value)
				}
			}
		}
		if reason == "" {
			return ephemeral("A reason is required."), nil
		}
		_, outcome, err := s.chatDecide(ctx, ch, actorID, actorName, itemID, "reject", truncateRunes(reason, 4900), version)
		return discordUpdate(in, itemID, outcome, err), nil
	}
	return ephemeral("Unsupported interaction."), nil
}

// discordUpdate edits the original message: the item's button row becomes a
// disabled button showing the outcome.
func discordUpdate(in discordInteraction, itemID, outcome string, err error) any {
	if (err != nil && !isConflict(err)) || in.Message == nil {
		return ephemeral(outcome)
	}
	rows := []map[string]any{}
	for _, row := range in.Message.Components {
		if refMatches(mustJSON(row), itemID) {
			rows = append(rows, map[string]any{"type": 1, "components": []any{map[string]any{
				"type": 2, "style": 2, "label": truncateRunes(outcome, 78), "custom_id": "sq|n|" + itemID, "disabled": true}}})
			continue
		}
		rows = append(rows, row)
	}
	return map[string]any{"type": 7, "data": map[string]any{"components": rows}}
}
