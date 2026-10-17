package app

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Notification channels: per-workspace Telegram / Slack / Discord / ntfy /
// email targets that receive a message when matching events happen (by
// default: a new pending item, or an escalation).

const (
	ChannelTelegram = "telegram"
	ChannelSlack    = "slack"
	ChannelDiscord  = "discord"
	ChannelNtfy     = "ntfy"
	ChannelEmail    = "email"
)

var ChannelTypes = []string{ChannelTelegram, ChannelSlack, ChannelDiscord, ChannelNtfy, ChannelEmail}

// DefaultChannelEvents: a new pending item ("item.created" only matches when
// the created item is pending) and escalations.
var DefaultChannelEvents = []string{"item.created"}

// Plan limits for channels.
// ponytail: belongs in PlanLimits (plans.go, shared); kept here until merged.

const LimitNotificationChannels = "notification_channels"

// flexStrings accepts ["1", 2, "U123"] (Telegram user IDs are numbers).
type flexStrings []string

func (f *flexStrings) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return errors.New("must be an array of IDs")
	}
	out := flexStrings{}
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) != nil {
			var n json.Number
			if err := json.Unmarshal(r, &n); err != nil {
				return errors.New("must be an array of IDs")
			}
			s = n.String()
		}
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	*f = out
	return nil
}

// ChannelConfig is the union of every type's settings. Fields tagged secret
// are stored encrypted and masked in responses.
type ChannelConfig struct {
	// telegram
	BotToken string `json:"bot_token,omitempty"` // secret (also Slack/Discord bot token)
	ChatID   string `json:"chat_id,omitempty"`
	Mode     string `json:"mode,omitempty"` // telegram: polling (default) | webhook
	// slack / discord
	WebhookURL    string `json:"webhook_url,omitempty"`    // secret
	Channel       string `json:"channel,omitempty"`        // Slack channel ID / Discord channel ID (bot mode)
	SigningSecret string `json:"signing_secret,omitempty"` // secret (Slack)
	ApplicationID string `json:"application_id,omitempty"` // Discord
	PublicKey     string `json:"public_key,omitempty"`     // Discord (hex Ed25519, not secret)
	// telegram / slack / discord
	ApproverUserIDs flexStrings `json:"approver_user_ids,omitempty"`
	// ntfy
	ServerURL   string `json:"server_url,omitempty"`
	Topic       string `json:"topic,omitempty"`
	AccessToken string `json:"access_token,omitempty"` // secret
	Priority    int    `json:"priority,omitempty"`
	// email (SMTP)
	Host     string   `json:"host,omitempty"`
	Port     int      `json:"port,omitempty"`
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"` // secret
	From     string   `json:"from,omitempty"`
	To       []string `json:"to,omitempty"`
	TLS      string   `json:"tls,omitempty"` // starttls (default) | tls | none
}

// channelSecrets is the encrypted part of a channel.
type channelSecrets struct {
	BotToken      string `json:"bot_token,omitempty"`
	WebhookURL    string `json:"webhook_url,omitempty"`
	SigningSecret string `json:"signing_secret,omitempty"`
	AccessToken   string `json:"access_token,omitempty"`
	Password      string `json:"password,omitempty"`
	// Generated, never shown: HMAC key for button payloads, Telegram webhook secret_token.
	CallbackKey   string `json:"callback_key,omitempty"`
	TelegramToken string `json:"telegram_webhook_secret,omitempty"`
}

var secretFields = []string{"bot_token", "webhook_url", "signing_secret", "access_token", "password"}

const maskPrefix = "••••"

type NotificationChannel struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	Type                string         `json:"type"`
	Enabled             bool           `json:"enabled"`
	Config              map[string]any `json:"config"`
	Events              []string       `json:"events"`
	CollectionIDs       []string       `json:"collection_ids"`
	DigestWindowSeconds int64          `json:"digest_window_seconds"`
	FailCount           int64          `json:"fail_count"`
	LastError           string         `json:"last_error"`
	DisabledReason      string         `json:"disabled_reason"`
	LastSentAt          *string        `json:"last_sent_at"`
	// Interactive is true when approve/reject buttons are delivered in chat;
	// otherwise messages carry one-tap decision links.
	Interactive bool `json:"interactive"`
	// IntegrationURL is where the chat platform must send callbacks
	// (Telegram webhook mode, Slack interactivity, Discord interactions).
	IntegrationURL string `json:"integration_url,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// notifyChannel is a decrypted channel used by senders.
type notifyChannel struct {
	row db.NotificationChannel
	cfg ChannelConfig // secrets merged in
	sec channelSecrets
	events,
	collections []string
}

func (c *notifyChannel) interactive() bool {
	switch c.row.Type {
	case ChannelTelegram:
		return true
	case ChannelSlack:
		return c.slackInteractive()
	case ChannelDiscord:
		return c.discordApp()
	}
	return false
}

// slackInteractive: Slack buttons decide in place only when interactions are
// verifiable (signing secret) and someone may use them (approver list).
func (c *notifyChannel) slackInteractive() bool {
	return c.cfg.SigningSecret != "" && len(c.cfg.ApproverUserIDs) > 0
}

func (c *notifyChannel) discordApp() bool {
	return c.cfg.BotToken != "" && c.cfg.Channel != "" && c.cfg.ApplicationID != "" && c.cfg.PublicKey != ""
}

type ChannelInput struct {
	Name                *string                    `json:"name"`
	Type                string                     `json:"type"`
	Enabled             *bool                      `json:"enabled"`
	Config              map[string]json.RawMessage `json:"config"`
	Events              *[]string                  `json:"events"`
	CollectionIDs       *[]string                  `json:"collection_ids"`
	DigestWindowSeconds *int64                     `json:"digest_window_seconds"`
}

type NotificationDelivery struct {
	ID            string  `json:"id"`
	ChannelID     string  `json:"channel_id"`
	EventID       string  `json:"event_id"`
	EventType     string  `json:"event_type"`
	ItemID        *string `json:"item_id"`
	Status        string  `json:"status"`
	Attempts      int64   `json:"attempts"`
	NextAttemptAt *string `json:"next_attempt_at"`
	LastError     string  `json:"last_error"`
	SentAt        *string `json:"sent_at"`
	CreatedAt     string  `json:"created_at"`
}

// ---- secret encryption ----

var notifyKeys sync.Map // *Service -> []byte

// MinNotifySecretKeyLen is the minimum length of NOTIFY_SECRET_KEY (e.g.
// `openssl rand -hex 32` gives 64 characters); shorter keys refuse to start.
const MinNotifySecretKeyLen = 32

// notifyKey derives the AES-256 key from NOTIFY_SECRET_KEY, or from a random
// key generated once into <DATA_DIR>/notify_secret.key.
func (s *Service) notifyKey() ([]byte, error) {
	if k, ok := notifyKeys.Load(s); ok {
		return k.([]byte), nil
	}
	var material []byte
	if v := s.Env("NOTIFY_SECRET_KEY", ""); v != "" {
		if len(v) < MinNotifySecretKeyLen {
			return nil, fmt.Errorf("NOTIFY_SECRET_KEY must be at least %d bytes", MinNotifySecretKeyLen)
		}
		material = []byte(v)
	} else {
		path := filepath.Join(s.notifyDataDir(), "notify_secret.key")
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				return nil, err
			}
			gen := []byte(hex.EncodeToString(randomBytes(32)))
			f, ferr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if ferr == nil {
				_, ferr = f.Write(gen)
				if cerr := f.Close(); ferr == nil {
					ferr = cerr
				}
				b, err = gen, ferr
			} else {
				b, err = os.ReadFile(path) // created concurrently
			}
		}
		if err != nil {
			return nil, fmt.Errorf("notification secret key: %w", err)
		}
		material = bytes.TrimSpace(b)
		if len(material) < 32 {
			return nil, errors.New("notification secret key file is too short")
		}
	}
	sum := sha256.Sum256(append([]byte("staqly-notify-v1:"), material...))
	k, _ := notifyKeys.LoadOrStore(s, sum[:])
	return k.([]byte), nil
}

func (s *Service) notifyDataDir() string {
	def := "./data"
	if s.Cfg.UploadDir != "" {
		def = filepath.Dir(filepath.Clean(s.Cfg.UploadDir))
	}
	return s.Env("DATA_DIR", def)
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// sealSecrets encrypts with AES-256-GCM; the channel ID is authenticated
// data, so a blob can't be moved to another channel.
func (s *Service) sealSecrets(channelID string, sec channelSecrets) (string, error) {
	key, err := s.notifyKey()
	if err != nil {
		return "", err
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := randomBytes(gcm.NonceSize())
	ct := gcm.Seal(nonce, nonce, []byte(mustJSON(sec)), []byte(channelID))
	return "v1:" + base64.StdEncoding.EncodeToString(ct), nil
}

func (s *Service) openSecrets(channelID, blob string) (channelSecrets, error) {
	var sec channelSecrets
	if blob == "" {
		return sec, nil
	}
	raw, ok := strings.CutPrefix(blob, "v1:")
	if !ok {
		return sec, errors.New("unknown secret format")
	}
	ct, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return sec, err
	}
	key, err := s.notifyKey()
	if err != nil {
		return sec, err
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	if len(ct) < gcm.NonceSize() {
		return sec, errors.New("secret blob too short")
	}
	pt, err := gcm.Open(nil, ct[:gcm.NonceSize()], ct[gcm.NonceSize():], []byte(channelID))
	if err != nil {
		return sec, errors.New("cannot decrypt channel secrets (was NOTIFY_SECRET_KEY changed?)")
	}
	err = json.Unmarshal(pt, &sec)
	return sec, err
}

// loadChannel decrypts a stored channel.
func (s *Service) loadChannel(r db.NotificationChannel) (*notifyChannel, error) {
	c := &notifyChannel{row: r}
	if err := json.Unmarshal([]byte(r.Config), &c.cfg); err != nil {
		return nil, err
	}
	sec, err := s.openSecrets(r.ID, r.Secrets)
	if err != nil {
		return nil, err
	}
	c.sec = sec
	c.cfg.BotToken, c.cfg.WebhookURL, c.cfg.SigningSecret = sec.BotToken, sec.WebhookURL, sec.SigningSecret
	c.cfg.AccessToken, c.cfg.Password = sec.AccessToken, sec.Password
	_ = json.Unmarshal([]byte(r.Events), &c.events)
	_ = json.Unmarshal([]byte(r.CollectionIds), &c.collections)
	return c, nil
}

// splitConfig separates the stored (public) config from secrets.
func splitConfig(cfg ChannelConfig, sec channelSecrets) (string, channelSecrets) {
	sec.BotToken, sec.WebhookURL, sec.SigningSecret = cfg.BotToken, cfg.WebhookURL, cfg.SigningSecret
	sec.AccessToken, sec.Password = cfg.AccessToken, cfg.Password
	cfg.BotToken, cfg.WebhookURL, cfg.SigningSecret, cfg.AccessToken, cfg.Password = "", "", "", "", ""
	return mustJSON(cfg), sec
}

func maskSecret(v string) string {
	if v == "" {
		return ""
	}
	if u, err := url.Parse(v); err == nil && u.Scheme != "" && u.Host != "" {
		return u.Scheme + "://" + u.Host + "/" + maskPrefix
	}
	if len(v) >= 16 {
		return maskPrefix + v[len(v)-4:]
	}
	return maskPrefix
}

func (s *Service) toChannel(c *notifyChannel) NotificationChannel {
	masked := c.cfg
	masked.BotToken, masked.WebhookURL = maskSecret(masked.BotToken), maskSecret(masked.WebhookURL)
	masked.SigningSecret, masked.AccessToken, masked.Password = maskSecret(masked.SigningSecret), maskSecret(masked.AccessToken), maskSecret(masked.Password)
	var cfg map[string]any
	_ = json.Unmarshal([]byte(mustJSON(masked)), &cfg)
	out := NotificationChannel{ID: c.row.ID, Name: c.row.Name, Type: c.row.Type, Enabled: c.row.Enabled == 1, Config: cfg,
		Events: c.events, CollectionIDs: c.collections, DigestWindowSeconds: c.row.DigestWindowSeconds,
		FailCount: c.row.FailCount, LastError: c.row.LastError, DisabledReason: c.row.DisabledReason,
		LastSentAt: ptr(c.row.LastSentAt), Interactive: c.interactive(), CreatedAt: c.row.CreatedAt, UpdatedAt: c.row.UpdatedAt}
	if out.Events == nil {
		out.Events = []string{}
	}
	if out.CollectionIDs == nil {
		out.CollectionIDs = []string{}
	}
	base := s.publicAPIBase()
	switch {
	case c.row.Type == ChannelTelegram && c.cfg.Mode == "webhook":
		out.IntegrationURL = base + "/v1/integrations/telegram/" + c.row.ID
	case c.row.Type == ChannelSlack && c.cfg.SigningSecret != "":
		out.IntegrationURL = base + "/v1/integrations/slack/interactions"
	case c.row.Type == ChannelDiscord && c.discordApp():
		out.IntegrationURL = base + "/v1/integrations/discord/interactions"
	}
	return out
}

// ---- validation ----

var (
	reTelegramToken = regexp.MustCompile(`^[0-9]{3,20}:[A-Za-z0-9_-]{20,100}$`)
	reTelegramChat  = regexp.MustCompile(`^(-?[0-9]{1,20}|@[A-Za-z0-9_]{4,64})$`)
	reNtfyTopic     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	reSlackID       = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	reHex64         = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	reDigits        = regexp.MustCompile(`^[0-9]{1,25}$`)
)

func validHTTPURL(field, v string) error {
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || len(v) > 2000 {
		return invalid(field, "must be an absolute http(s) URL")
	}
	if u.User != nil {
		return invalid(field, "must not contain credentials")
	}
	return nil
}

func validateChannelConfig(typ string, c *ChannelConfig) error {
	for i, id := range c.ApproverUserIDs {
		if len(id) > 64 || !reSlackID.MatchString(id) {
			return invalid("config.approver_user_ids", "invalid ID at position "+strconv.Itoa(i))
		}
	}
	if len(c.ApproverUserIDs) > 100 {
		return invalid("config.approver_user_ids", "at most 100 IDs")
	}
	switch typ {
	case ChannelTelegram:
		if !reTelegramToken.MatchString(c.BotToken) {
			return invalid("config.bot_token", "must be a Telegram bot token (123456:ABC...) from @BotFather")
		}
		if !reTelegramChat.MatchString(c.ChatID) {
			return invalid("config.chat_id", "must be a numeric chat ID or @channelname")
		}
		if c.Mode == "" {
			c.Mode = "polling"
		}
		if c.Mode != "polling" && c.Mode != "webhook" {
			return invalid("config.mode", "must be polling or webhook")
		}
		for _, id := range c.ApproverUserIDs {
			if !reDigits.MatchString(id) {
				return invalid("config.approver_user_ids", "Telegram user IDs are numeric")
			}
		}
	case ChannelSlack:
		if c.WebhookURL == "" && (c.BotToken == "" || c.Channel == "") {
			return invalid("config.webhook_url", "set an incoming webhook URL, or bot_token + channel")
		}
		if c.WebhookURL != "" {
			if err := validHTTPURL("config.webhook_url", c.WebhookURL); err != nil {
				return err
			}
		}
		if c.Channel != "" && !reSlackID.MatchString(c.Channel) {
			return invalid("config.channel", "must be a Slack channel ID (C...)")
		}
		if c.SigningSecret != "" && len(c.SigningSecret) < 16 {
			return invalid("config.signing_secret", "looks too short")
		}
	case ChannelDiscord:
		if c.WebhookURL == "" && (c.BotToken == "" || c.Channel == "") {
			return invalid("config.webhook_url", "set a webhook URL, or bot_token + channel (+ application_id, public_key for buttons)")
		}
		if c.WebhookURL != "" {
			if err := validHTTPURL("config.webhook_url", c.WebhookURL); err != nil {
				return err
			}
		}
		if c.PublicKey != "" && !reHex64.MatchString(c.PublicKey) {
			return invalid("config.public_key", "must be the application's hex Ed25519 public key")
		}
		if c.ApplicationID != "" && !reDigits.MatchString(c.ApplicationID) {
			return invalid("config.application_id", "must be numeric")
		}
		if c.Channel != "" && !reDigits.MatchString(c.Channel) {
			return invalid("config.channel", "must be a numeric Discord channel ID")
		}
		for _, id := range c.ApproverUserIDs {
			if !reDigits.MatchString(id) {
				return invalid("config.approver_user_ids", "Discord user IDs are numeric")
			}
		}
	case ChannelNtfy:
		if c.ServerURL == "" {
			c.ServerURL = "https://ntfy.sh"
		}
		c.ServerURL = strings.TrimRight(c.ServerURL, "/")
		if err := validHTTPURL("config.server_url", c.ServerURL); err != nil {
			return err
		}
		if !reNtfyTopic.MatchString(c.Topic) {
			return invalid("config.topic", "must be 1-64 letters, digits, - or _")
		}
		if c.Priority < 0 || c.Priority > 5 {
			return invalid("config.priority", "must be 1-5")
		}
	case ChannelEmail:
		if c.Host == "" || len(c.Host) > 253 || strings.ContainsAny(c.Host, " /:\r\n") {
			return invalid("config.host", "SMTP host is required")
		}
		if c.TLS == "" {
			c.TLS = "starttls"
			if c.Port == 465 {
				c.TLS = "tls"
			}
		}
		if c.TLS != "starttls" && c.TLS != "tls" && c.TLS != "none" {
			return invalid("config.tls", "must be starttls, tls or none")
		}
		if c.Port == 0 {
			c.Port = 587
			if c.TLS == "tls" {
				c.Port = 465
			}
		}
		if c.Port < 1 || c.Port > 65535 {
			return invalid("config.port", "must be a TCP port")
		}
		if _, err := mail.ParseAddress(c.From); err != nil || strings.ContainsAny(c.From, "\r\n") {
			return invalid("config.from", "must be an email address")
		}
		if len(c.To) == 0 || len(c.To) > 20 {
			return invalid("config.to", "1-20 recipients required")
		}
		for _, to := range c.To {
			if a, err := mail.ParseAddress(to); err != nil || a.Name != "" || strings.ContainsAny(to, "\r\n") {
				return invalid("config.to", "invalid address "+to)
			}
		}
		if strings.ContainsAny(c.Username, "\r\n") {
			return invalid("config.username", "is invalid")
		}
	default:
		return invalid("type", "must be one of "+strings.Join(ChannelTypes, ", "))
	}
	return nil
}

// retargetRules: when one of these destination fields changes, the stored
// secret that would be sent to the new destination must be supplied again in
// the same request. Otherwise a channels:manage caller who can't read secrets
// could point a channel at their own server and receive them.
var retargetRules = map[string][]struct {
	field, secret string
	get           func(c ChannelConfig) string
}{
	ChannelTelegram: {{"chat_id", "bot_token", func(c ChannelConfig) string { return c.ChatID }}},
	ChannelSlack:    {{"channel", "bot_token", func(c ChannelConfig) string { return c.Channel }}},
	ChannelDiscord:  {{"channel", "bot_token", func(c ChannelConfig) string { return c.Channel }}},
	ChannelNtfy:     {{"server_url", "access_token", func(c ChannelConfig) string { return c.ServerURL }}},
	ChannelEmail: {
		{"host", "password", func(c ChannelConfig) string { return c.Host }},
		{"port", "password", func(c ChannelConfig) string { return strconv.Itoa(c.Port) }},
		{"tls", "password", func(c ChannelConfig) string { return c.TLS }},
		{"username", "password", func(c ChannelConfig) string { return c.Username }},
	},
}

func secretOf(c ChannelConfig, field string) string {
	switch field {
	case "bot_token":
		return c.BotToken
	case "access_token":
		return c.AccessToken
	case "password":
		return c.Password
	}
	return ""
}

func checkRetarget(typ string, old, next ChannelConfig, in map[string]json.RawMessage) error {
	provided := func(k string) bool {
		var v string
		return json.Unmarshal(in[k], &v) == nil && v != "" && !strings.Contains(v, maskPrefix)
	}
	norm := func(typ, field, v string) string { // defaults applied by validation
		switch {
		case typ == ChannelNtfy && field == "server_url":
			return strings.TrimRight(notifyFirst(v, "https://ntfy.sh"), "/")
		case field == "port" && v == "0", field == "tls" && v == "":
			return ""
		}
		return v
	}
	for _, r := range retargetRules[typ] {
		if secretOf(old, r.secret) == "" || provided(r.secret) {
			continue
		}
		if _, set := in[r.field]; !set {
			continue
		}
		if norm(typ, r.field, r.get(old)) != norm(typ, r.field, r.get(next)) {
			return invalid("config."+r.secret, "must be re-entered when "+r.field+" changes")
		}
	}
	return nil
}

// applyChannelInput merges input onto ch (config keys merge; masked secret
// values are ignored so a client can echo back what it read).
func (s *Service) applyChannelInput(ctx context.Context, p Principal, ch *notifyChannel, in ChannelInput) error {
	if in.Name != nil {
		n, err := trimmed(*in.Name, "name", 100, true)
		if err != nil {
			return err
		}
		ch.row.Name = n
	}
	if in.Enabled != nil {
		ch.row.Enabled = 0
		if *in.Enabled {
			ch.row.Enabled = 1
			ch.row.FailCount, ch.row.DisabledReason = 0, ""
		}
	}
	if in.Config != nil {
		old := ch.cfg
		cur := map[string]json.RawMessage{}
		_ = json.Unmarshal([]byte(mustJSON(ch.cfg)), &cur)
		for k, v := range in.Config {
			var sv string
			if slices.Contains(secretFields, k) && json.Unmarshal(v, &sv) == nil && strings.Contains(sv, maskPrefix) {
				continue // echoed mask: keep the stored secret
			}
			if string(bytes.TrimSpace(v)) == "null" {
				delete(cur, k)
				continue
			}
			cur[k] = v
		}
		var cfg ChannelConfig
		dec := json.NewDecoder(bytes.NewReader([]byte(mustJSON(cur))))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cfg); err != nil {
			return invalid("config", strings.TrimPrefix(err.Error(), "json: "))
		}
		if err := checkRetarget(ch.row.Type, old, cfg, in.Config); err != nil {
			return err
		}
		ch.cfg = cfg
	}
	if err := validateChannelConfig(ch.row.Type, &ch.cfg); err != nil {
		return err
	}
	if in.Events != nil {
		if err := s.validateEventPatterns(ctx, p, *in.Events); err != nil {
			return err
		}
		ch.events = *in.Events
	}
	if in.CollectionIDs != nil {
		for _, id := range *in.CollectionIDs {
			if _, err := s.Q.GetCollection(ctx, db.GetCollectionParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
				return invalid("collection_ids", "unknown collection "+id)
			}
		}
		ch.collections = *in.CollectionIDs
	}
	if in.DigestWindowSeconds != nil {
		if *in.DigestWindowSeconds < 0 || *in.DigestWindowSeconds > 86400 {
			return invalid("digest_window_seconds", "must be between 0 and 86400")
		}
		ch.row.DigestWindowSeconds = *in.DigestWindowSeconds
	}
	if ch.sec.CallbackKey == "" {
		ch.sec.CallbackKey = randomString(32)
	}
	if ch.sec.TelegramToken == "" && ch.row.Type == ChannelTelegram {
		ch.sec.TelegramToken = randomString(32)
	}
	var sec channelSecrets
	ch.row.Config, sec = splitConfig(ch.cfg, ch.sec)
	ch.sec = sec
	blob, err := s.sealSecrets(ch.row.ID, sec)
	if err != nil {
		return err
	}
	ch.row.Secrets = blob
	ch.row.Events, ch.row.CollectionIds = mustJSON(ch.events), mustJSON(ch.collections)
	return nil
}

// ---- CRUD ----

func (s *Service) CreateNotificationChannel(ctx context.Context, p Principal, in ChannelInput) (NotificationChannel, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return NotificationChannel{}, err
	}
	if !slices.Contains(ChannelTypes, in.Type) {
		return NotificationChannel{}, invalid("type", "must be one of "+strings.Join(ChannelTypes, ", "))
	}
	if in.Name == nil {
		return NotificationChannel{}, invalid("name", "is required")
	}
	if in.Config == nil {
		return NotificationChannel{}, invalid("config", "is required")
	}
	now := s.now()
	ch := &notifyChannel{row: db.NotificationChannel{ID: newID("nch"), WorkspaceID: p.WorkspaceID, Type: in.Type, Enabled: 1,
		CreatedAt: now, UpdatedAt: now}, events: DefaultChannelEvents, collections: []string{}}
	if err := s.applyChannelInput(ctx, p, ch, in); err != nil {
		return NotificationChannel{}, err
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		if err := s.checkChannelLimit(ctx, q, p.WorkspaceID); err != nil {
			return err
		}
		r := ch.row
		if err := q.CreateNotifyChannel(ctx, db.CreateNotifyChannelParams{ID: r.ID, WorkspaceID: r.WorkspaceID, Name: r.Name,
			Type: r.Type, Enabled: r.Enabled, Config: r.Config, Secrets: r.Secrets, Events: r.Events,
			CollectionIds: r.CollectionIds, DigestWindowSeconds: r.DigestWindowSeconds, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		_, err := s.emit(ctx, q, p, "notification_channel.created", "", "", map[string]any{"channel": s.toChannel(ch)})
		return err
	})
	if err != nil {
		return NotificationChannel{}, err
	}
	s.afterChannelSaved(ctx, ch)
	return s.toChannel(ch), nil
}

func (s *Service) checkChannelLimit(ctx context.Context, q *db.Queries, ws string) error {
	plan, err := s.planFor(ctx, q, ws)
	if err != nil {
		return err
	}
	max := plan.Limits.NotificationChannels
	n, err := q.CountNotifyChannels(ctx, ws)
	if err != nil {
		return err
	}
	return checkCount(plan, LimitNotificationChannels, max, n)
}

func (s *Service) getChannel(ctx context.Context, p Principal, id string) (*notifyChannel, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return nil, err
	}
	r, err := s.Q.GetNotifyChannel(ctx, db.GetNotifyChannelParams{WorkspaceID: p.WorkspaceID, ID: id})
	if isNoRows(err) {
		return nil, notFound("notification channel")
	}
	if err != nil {
		return nil, err
	}
	return s.loadChannel(r)
}

func (s *Service) GetNotificationChannel(ctx context.Context, p Principal, id string) (NotificationChannel, error) {
	ch, err := s.getChannel(ctx, p, id)
	if err != nil {
		return NotificationChannel{}, err
	}
	return s.toChannel(ch), nil
}

func (s *Service) ListNotificationChannels(ctx context.Context, p Principal) ([]NotificationChannel, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListNotifyChannels(ctx, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	out := []NotificationChannel{}
	for _, r := range rows {
		ch, err := s.loadChannel(r)
		if err != nil {
			return nil, err
		}
		out = append(out, s.toChannel(ch))
	}
	return out, nil
}

func (s *Service) UpdateNotificationChannel(ctx context.Context, p Principal, id string, in ChannelInput) (NotificationChannel, error) {
	ch, err := s.getChannel(ctx, p, id)
	if err != nil {
		return NotificationChannel{}, err
	}
	if in.Type != "" && in.Type != ch.row.Type {
		return NotificationChannel{}, invalid("type", "cannot be changed; create a new channel")
	}
	if in.Enabled != nil && *in.Enabled == (ch.row.Enabled == 1) && onlyChange(in, "Enabled", "Type") {
		if *in.Enabled {
			return NotificationChannel{}, alreadyErr("notification channel", "enabled")
		}
		return NotificationChannel{}, alreadyErr("notification channel", "disabled")
	}
	if err := s.applyChannelInput(ctx, p, ch, in); err != nil {
		return NotificationChannel{}, err
	}
	r := &ch.row
	r.UpdatedAt = s.now()
	// disabled_at starts the NOTIFY_DISABLED_GRACE clock for queued rows.
	if r.Enabled == 1 {
		r.DisabledAt = sql.NullString{}
	} else if !r.DisabledAt.Valid {
		r.DisabledAt = nullStr(r.UpdatedAt)
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := q.UpdateNotifyChannel(ctx, db.UpdateNotifyChannelParams{Name: r.Name, Enabled: r.Enabled, Config: r.Config,
			Secrets: r.Secrets, Events: r.Events, CollectionIds: r.CollectionIds, DigestWindowSeconds: r.DigestWindowSeconds,
			FailCount: r.FailCount, LastError: r.LastError, DisabledReason: r.DisabledReason, DisabledAt: r.DisabledAt,
			UpdatedAt: r.UpdatedAt, WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		if r.Enabled != 1 { // disabling revokes the one-tap links it sent
			if err := q.DeleteChannelDecisionLinks(ctx, db.DeleteChannelDecisionLinksParams{WorkspaceID: p.WorkspaceID, ChannelID: nullStr(id)}); err != nil {
				return err
			}
		}
		_, err := s.emit(ctx, q, p, "notification_channel.updated", "", "", map[string]any{"channel": s.toChannel(ch)})
		return err
	})
	if err != nil {
		return NotificationChannel{}, err
	}
	s.afterChannelSaved(ctx, ch)
	return s.toChannel(ch), nil
}

func (s *Service) DeleteNotificationChannel(ctx context.Context, p Principal, id string) error {
	ch, err := s.getChannel(ctx, p, id)
	if err != nil {
		return err
	}
	var n int64
	err = s.tx(ctx, func(q *db.Queries) error {
		// Its one-tap links die with it (they would otherwise outlive it).
		if err := q.DeleteChannelDecisionLinks(ctx, db.DeleteChannelDecisionLinksParams{WorkspaceID: p.WorkspaceID,
			ChannelID: nullStr(id)}); err != nil {
			return err
		}
		var err error
		if n, err = q.DeleteNotifyChannel(ctx, db.DeleteNotifyChannelParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil || n == 0 {
			return err
		}
		_, err = s.emit(ctx, q, p, "notification_channel.deleted", "", "", map[string]any{"channel_id": id, "type": ch.row.Type})
		return err
	})
	if err == nil && n == 0 {
		return notFound("notification channel")
	}
	if err == nil && ch.row.Type == ChannelTelegram && ch.cfg.Mode == "webhook" {
		_ = s.telegramCall(ctx, ch, "deleteWebhook", map[string]any{}, nil)
	}
	return err
}

// afterChannelSaved registers/unregisters the Telegram webhook (best effort;
// failures surface as last_error).
func (s *Service) afterChannelSaved(ctx context.Context, ch *notifyChannel) {
	if ch.row.Type != ChannelTelegram || ch.row.Enabled != 1 || s.Env("NOTIFY_TELEGRAM_AUTOREGISTER", "true") != "true" {
		return
	}
	var err error
	if ch.cfg.Mode == "webhook" {
		err = s.telegramCall(ctx, ch, "setWebhook", map[string]any{
			"url": s.publicAPIBase() + "/v1/integrations/telegram/" + ch.row.ID, "secret_token": ch.sec.TelegramToken,
			"allowed_updates": []string{"message", "callback_query"}}, nil)
	} else {
		err = s.telegramCall(ctx, ch, "deleteWebhook", map[string]any{}, nil)
	}
	if err != nil {
		ch.row.LastError = "telegram: " + err.Error()
		_ = s.W.SetNotifyChannelError(ctx, db.SetNotifyChannelErrorParams{LastError: ch.row.LastError, ID: ch.row.ID})
	}
}

// TestNotificationChannel sends a test message right away.
func (s *Service) TestNotificationChannel(ctx context.Context, p Principal, id string) (map[string]any, error) {
	ch, err := s.getChannel(ctx, p, id)
	if err != nil {
		return nil, err
	}
	msg := &notifyMessage{Headline: "Test notification from staqly: channel “" + ch.row.Name + "” works."}
	if err := s.sendNotification(ctx, ch, msg); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}, nil
	}
	return map[string]any{"ok": true}, nil
}

func (s *Service) ListNotificationDeliveries(ctx context.Context, p Principal, channelID string, limit int) ([]NotificationDelivery, error) {
	if _, err := s.getChannel(ctx, p, channelID); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListNotifyOutbox(ctx, db.ListNotifyOutboxParams{WorkspaceID: p.WorkspaceID, ChannelID: channelID,
		Limit: int64(clampLimit(limit, 50, 200))})
	out := []NotificationDelivery{}
	for _, r := range rows {
		d := NotificationDelivery{ID: r.ID, ChannelID: r.ChannelID, EventID: r.EventID, EventType: r.EventType,
			ItemID: ptr(r.ItemID), Status: r.Status, Attempts: r.Attempts, LastError: r.LastError, SentAt: ptr(r.SentAt),
			CreatedAt: r.CreatedAt}
		if r.Status == "pending" {
			d.NextAttemptAt = &r.NextAttemptAt
		}
		out = append(out, d)
	}
	return out, err
}
