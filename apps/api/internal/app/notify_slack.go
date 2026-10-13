package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Slack: Block Kit messages through an incoming webhook or chat.postMessage.
// With the app's signing secret configured, Approve/Reject are interactive
// buttons handled at POST /v1/integrations/slack/interactions; otherwise
// they are one-tap decision links.

func (s *Service) slackBase() string {
	return strings.TrimRight(s.Env("SLACK_API_BASE", "https://slack.com/api"), "/")
}

var slackEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func slackText(t string) map[string]any { return map[string]any{"type": "mrkdwn", "text": t} }

func slackButton(text, style string) map[string]any {
	b := map[string]any{"type": "button", "text": map[string]any{"type": "plain_text", "text": text, "emoji": true}}
	if style != "" {
		b["style"] = style
	}
	return b
}

func slackBlocks(ch *notifyChannel, msg *notifyMessage) []map[string]any {
	if msg.Headline != "" {
		return []map[string]any{{"type": "section", "text": slackText(":bell: " + slackEsc.Replace(msg.Headline))}}
	}
	var blocks []map[string]any
	if len(msg.Entries) > 1 {
		blocks = append(blocks, map[string]any{"type": "header", "text": map[string]any{"type": "plain_text",
			"text": fmt.Sprintf("%d updates in staq.ly", len(msg.Entries))}})
	}
	for i, e := range msg.Entries {
		if i == 15 {
			blocks = append(blocks, map[string]any{"type": "context", "elements": []any{slackText(fmt.Sprintf("…and %d more", len(msg.Entries)-15))}})
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
		if !e.Actionable || i >= 10 {
			continue
		}
		var els []any
		if ch.cfg.SigningSecret != "" {
			for _, v := range []struct{ verb, label, style string }{{"a", "✅ Approve", "primary"}, {"r", "❌ Reject", "danger"}} {
				p := v.verb + "|" + e.ItemID
				b := slackButton(v.label, v.style)
				b["action_id"] = "staqly_" + v.verb
				b["value"] = ch.row.ID + "|" + p + "|" + callbackSig(ch.sec.CallbackKey, p)
				els = append(els, b)
			}
			if e.RejectURL != "" {
				b := slackButton("💬 Reject with reason…", "")
				b["action_id"], b["url"] = "staqly_link_reason", e.RejectURL
				els = append(els, b)
			}
		} else if e.ApproveURL != "" {
			a, r := slackButton("✅ Approve…", "primary"), slackButton("❌ Reject…", "danger")
			a["action_id"], a["url"] = "staqly_link_approve", e.ApproveURL
			r["action_id"], r["url"] = "staqly_link_reject", e.RejectURL
			els = append(els, a, r)
		}
		if len(els) > 0 {
			blocks = append(blocks, map[string]any{"type": "actions", "block_id": "sq_" + e.ItemID, "elements": els})
		}
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

// VerifySlackSignature checks X-Slack-Signature (v0 HMAC-SHA256 over
// "v0:<ts>:<body>") and a 5-minute timestamp window.
func VerifySlackSignature(secret string, now time.Time, ts, sig string, body []byte) bool {
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || secret == "" {
		return false
	}
	if d := now.Unix() - n; d > 300 || d < -300 {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(m, "v0:%s:", ts)
	m.Write(body)
	return hmac.Equal([]byte("v0="+hex.EncodeToString(m.Sum(nil))), []byte(sig))
}

type slackPayload struct {
	Type string `json:"type"`
	User struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"user"`
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
	ResponseURL string `json:"response_url"`
	Message     *struct {
		Blocks []map[string]any `json:"blocks"`
	} `json:"message"`
}

// HandleSlackInteraction processes a block_actions request. The channel is
// identified by the button value, then the request must carry a valid
// signature made with that channel's signing secret.
func (s *Service) HandleSlackInteraction(ctx context.Context, body []byte, ts, sig string) error {
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return invalid("payload", "invalid form body")
	}
	var p slackPayload
	if err := json.Unmarshal([]byte(form.Get("payload")), &p); err != nil {
		return invalid("payload", "invalid JSON")
	}
	if p.Type != "block_actions" || len(p.Actions) == 0 || !strings.HasPrefix(p.Actions[0].ActionID, "staqly_") ||
		strings.HasPrefix(p.Actions[0].ActionID, "staqly_link") {
		return nil // e.g. URL buttons: nothing to do
	}
	parts := strings.Split(p.Actions[0].Value, "|")
	if len(parts) != 4 {
		return invalid("payload", "unknown action")
	}
	ch, err := s.channelByID(ctx, parts[0], ChannelSlack)
	if err != nil {
		return ErrUnauthorized // don't reveal which channel IDs exist
	}
	if !VerifySlackSignature(ch.cfg.SigningSecret, s.Now(), ts, sig, body) {
		return ErrUnauthorized
	}
	verb, itemID := parts[1], parts[2]
	if !verifyCallbackSig(ch.sec.CallbackKey, verb+"|"+itemID, parts[3]) || (verb != "a" && verb != "r") {
		return invalid("payload", "invalid action signature")
	}
	if !approverAllowed(ch.cfg.ApproverUserIDs, p.User.ID) {
		s.slackRespond(ctx, p.ResponseURL, map[string]any{"response_type": "ephemeral", "replace_original": false,
			"text": "You are not on this channel's approver list."})
		return nil
	}
	name := notifyFirst(p.User.Username, p.User.Name, p.User.ID)
	_, outcome, derr := s.chatDecide(ctx, ch, "slack:"+p.User.ID, "Slack: "+name, itemID, map[string]string{"a": "approve", "r": "reject"}[verb], "")
	if (derr == nil || isConflict(derr)) && p.Message != nil && len(p.Message.Blocks) > 0 {
		blocks := p.Message.Blocks
		for i, b := range blocks {
			if b["block_id"] == "sq_"+itemID {
				blocks[i] = map[string]any{"type": "context", "elements": []any{slackText(slackEsc.Replace(outcome))}}
			}
		}
		s.slackRespond(ctx, p.ResponseURL, map[string]any{"replace_original": true, "text": outcome, "blocks": blocks})
		return nil
	}
	s.slackRespond(ctx, p.ResponseURL, map[string]any{"response_type": "ephemeral", "replace_original": false, "text": outcome})
	return nil
}

// slackRespond posts to response_url (only Slack's own hosts) in the
// background so the interaction is acknowledged within Slack's 3 s limit.
func (s *Service) slackRespond(ctx context.Context, responseURL string, body any) {
	u, err := url.Parse(responseURL)
	if err != nil || u.Scheme == "" {
		return
	}
	allowed := u.Scheme == "https" && (u.Hostname() == "hooks.slack.com" || strings.HasSuffix(u.Hostname(), ".slack.com"))
	if base, err := url.Parse(s.Env("SLACK_API_BASE", "")); err == nil && base.Host != "" && base.Host == u.Host {
		allowed = true // test / self-hosted override
	}
	if !allowed {
		return
	}
	go func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if err := s.postJSON(c, responseURL, nil, body, nil); err != nil {
			s.Log.Warn("slack response_url failed", "err", err)
		}
	}()
}
