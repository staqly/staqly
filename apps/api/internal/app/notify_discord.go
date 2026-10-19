package app

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
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
