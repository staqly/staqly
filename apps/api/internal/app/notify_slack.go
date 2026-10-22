package app

import (
	"context"
	"fmt"
	"strings"
)

// Slack: Block Kit messages through an incoming webhook or chat.postMessage.
// Alerts only: nothing comes back from Slack.

func (s *Service) slackBase() string {
	return strings.TrimRight(s.Env("SLACK_API_BASE", "https://slack.com/api"), "/")
}

var slackEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func slackText(t string) map[string]any { return map[string]any{"type": "mrkdwn", "text": t} }

func slackBlocks(ch *notifyChannel, msg *notifyMessage) []map[string]any {
	if msg.Headline != "" {
		return []map[string]any{{"type": "section", "text": slackText(":bell: " + slackEsc.Replace(msg.Headline))}}
	}
	var blocks []map[string]any
	if len(msg.Entries) > 1 {
		blocks = append(blocks, map[string]any{"type": "header", "text": map[string]any{"type": "plain_text",
			"text": fmt.Sprintf("%d updates in staqly", len(msg.Entries))}})
	}
	for i, e := range msg.Entries {
		if i == 15 {
			blocks = append(blocks, map[string]any{"type": "context", "elements": []any{slackText(fmt.Sprintf("...and %d more", len(msg.Entries)-15))}})
			break
		}
		title := slackEsc.Replace(truncateRunes(e.Title, 150))
		if e.WebURL != "" {
			title = "<" + e.WebURL + "|" + title + ">"
		}
		head := fmt.Sprintf("*%s*: %s", slackEsc.Replace(eventLabel(e)), "*"+title+"*")
		if len(msg.Entries) > 1 {
			head = fmt.Sprintf("*#%d* %s", i+1, head)
		}
		text := head + "\n_" + slackEsc.Replace(entryMeta(e)) + "_"
		previewMax := 1200
		if len(msg.Entries) > 1 {
			previewMax = 200
		}
		if p := truncateRunes(e.Preview, previewMax); p != "" {
			text += "\n>" + slackEsc.Replace(p)
		}
		if e.Note != "" {
			text += "\n:memo: " + slackEsc.Replace(truncateRunes(e.Note, 300))
		}
		blocks = append(blocks, map[string]any{"type": "section", "text": slackText(truncateRunes(text, 2900))})
	}
	return blocks
}

func (s *Service) sendSlack(ctx context.Context, ch *notifyChannel, msg *notifyMessage) error {
	body := map[string]any{"text": msg.title(), "blocks": slackBlocks(ch, msg), "unfurl_links": false, "unfurl_media": false}
	if ch.cfg.WebhookURL != "" {
		return s.postJSON(ctx, ch.cfg.WebhookURL, nil, body, nil)
	}
	body["channel"] = ch.cfg.Channel
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := s.postJSON(ctx, s.slackBase()+"/chat.postMessage", map[string]string{"Authorization": "Bearer " + ch.cfg.BotToken}, body, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("slack: %s", notifyFirst(resp.Error, "request failed"))
	}
	return nil
}
