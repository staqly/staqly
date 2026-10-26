package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Connecting a Telegram chat through the server's own bot (PROVIDER_TELEGRAM_BOT_TOKEN): the dashboard asks for a
// code, the person adds the bot to the channel or group and sends /connect CODE there, and the bot's poller
// (getUpdates, so no public address is needed) connects that chat to the workspace that asked. Without the
// setting, Telegram connects with a bot of the person's own (token and chat in the form).

const (
	telegramCodeTTL = 10 * time.Minute
	// ponytail: one poller per bot token; Telegram answers 409 to a second one (several servers on one token), so
	// one of them would need to own it.
	telegramPollWait = 25 // seconds getUpdates waits for news
)

func init() {
	RegisterWorker(BackgroundWorker{Name: "telegram-bot", Run: func(ctx context.Context, s *Service) { s.runTelegramBot(ctx) }})
}

// TelegramConnect is a code to send in the chat, and what became of it.
type TelegramConnect struct {
	Code      string `json:"code"`
	Command   string `json:"command"`
	Bot       string `json:"bot"` // @username of the bot to add
	ExpiresAt string `json:"expires_at"`
	// Status: waiting (no /connect yet, or the last one failed: Error says why), connected (ChannelID), expired.
	Status    string `json:"status"`
	ChannelID string `json:"channel_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// telegramBot: the server's bot, once its token was checked at start.
func (s *Service) telegramBot() (token, username string) { return providers.TelegramBot() }

// StartTelegramConnect makes a code for connecting a chat through the server's bot.
func (s *Service) StartTelegramConnect(ctx context.Context, p Principal, groupID string) (TelegramConnect, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return TelegramConnect{}, err
	}
	if _, err := s.availableProvider("telegram"); err != nil {
		return TelegramConnect{}, err
	}
	_, bot := s.telegramBot()
	if bot == "" {
		return TelegramConnect{}, &Error{Code: "not_available", Status: 409, Message: "this server has no Telegram bot of its own; connect with your bot's token instead"}
	}
	now := s.Now()
	_ = s.W.DeleteOldTelegramCodes(ctx, formatTime(now.Add(-24*time.Hour)))
	for range 5 {
		code := telegramCode()
		err := s.W.CreateTelegramCode(ctx, db.CreateTelegramCodeParams{Code: code, WorkspaceID: p.WorkspaceID, CreatedBy: p.actor(),
			ActorName: p.Name, GroupID: groupID, CreatedAt: formatTime(now), ExpiresAt: formatTime(now.Add(telegramCodeTTL))})
		if err == nil {
			return TelegramConnect{Code: code, Command: telegramCommand(bot, code), Bot: "@" + bot,
				ExpiresAt: formatTime(now.Add(telegramCodeTTL)), Status: "waiting"}, nil
		}
	}
	return TelegramConnect{}, errors.New("no free Telegram connect code")
}

// GetTelegramConnect tells the dashboard whether the chat is connected yet.
func (s *Service) GetTelegramConnect(ctx context.Context, p Principal, code string) (TelegramConnect, error) {
	if err := p.require(ScopeChannelsRead); err != nil {
		return TelegramConnect{}, err
	}
	r, err := s.Q.GetTelegramCode(ctx, db.GetTelegramCodeParams{WorkspaceID: p.WorkspaceID, Code: code})
	if isNoRows(err) {
		return TelegramConnect{}, notFound("code")
	}
	if err != nil {
		return TelegramConnect{}, err
	}
	_, bot := s.telegramBot()
	out := TelegramConnect{Code: r.Code, Command: telegramCommand(bot, r.Code), Bot: "@" + bot, ExpiresAt: r.ExpiresAt,
		Status: "waiting", ChannelID: r.ChannelID, Error: r.Error}
	switch {
	case r.ChannelID != "":
		out.Status, out.Error = "connected", ""
	case r.ExpiresAt <= s.now():
		out.Status = "expired"
	}
	return out, nil
}

// telegramCommand is addressed to the server's bot (/connect@bot CODE): other bots in a group that know /connect
// (moderation bots do) leave it alone.
func telegramCommand(bot, code string) string {
	if bot == "" {
		return "/connect " + code
	}
	return "/connect@" + bot + " " + code
}

// telegramCode: 6 letters and digits that read unambiguously (no 0/O, 1/I/L).
func telegramCode() string {
	const abc = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = abc[int(b[i])%len(abc)]
	}
	return string(b)
}

// runTelegramBot checks the server's bot at start and then waits for /connect commands until ctx ends.
func (s *Service) runTelegramBot(ctx context.Context) {
	token := s.Env("PROVIDER_TELEGRAM_BOT_TOKEN", "")
	if token == "" || !s.providerEnabled("telegram") {
		return
	}
	client := &http.Client{Timeout: time.Duration(telegramPollWait+15) * time.Second}
	cctx := providers.WithHTTPClient(ctx, client)
	log := s.logger(ctx)
	var username string
	for username == "" {
		u, name, err := providers.TelegramMe(cctx, token)
		if err == nil {
			username = u
			providers.SetTelegramBotName(name)
			break
		}
		log.Warn("telegram bot not reachable", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
	providers.SetTelegramBot(token, username)
	log.Info("telegram bot ready", "bot", username)
	var offset int64
	for ctx.Err() == nil {
		cmds, next, err := providers.TelegramCommands(cctx, token, offset, telegramPollWait)
		if err != nil {
			if ctx.Err() == nil {
				log.Warn("telegram updates", "err", err)
				select {
				case <-ctx.Done():
				case <-time.After(10 * time.Second):
				}
			}
			continue
		}
		offset = next
		for _, c := range cmds {
			s.telegramConnectChat(cctx, token, c)
		}
	}
}

// telegramConnectChat connects the chat a /connect came from to the workspace whose code it names.
func (s *Service) telegramConnectChat(ctx context.Context, token string, c providers.TelegramCommand) {
	r, err := s.Q.FindTelegramCode(ctx, db.FindTelegramCodeParams{Code: c.Code, ExpiresAt: s.now()})
	if errors.Is(err, sql.ErrNoRows) {
		providers.TelegramSay(ctx, token, c.ChatID, "This code is not valid any more. Get a new one in staqly: Channels, Add channel, Telegram.")
		return
	}
	if err != nil {
		s.logger(ctx).Warn("telegram code", "err", err)
		return
	}
	e, err := s.availableProvider("telegram")
	var ch Channel
	if err == nil {
		var creds providers.Credentials
		var profile providers.Profile
		creds, profile, err = e.Connect(s.providerCtx(ctx),
			map[string]string{"bot": "shared", "chat": strconv.FormatInt(c.ChatID, 10)})
		if err == nil {
			p := Principal{WorkspaceID: r.WorkspaceID, Kind: PrincipalSystem, ID: "telegram-bot", Name: r.ActorName}
			if !strings.Contains(r.CreatedBy, ":") { // a person, not "key:..." or "system:..."
				p.UserID = r.CreatedBy
			}
			ch, err = s.saveOrReconnect(ctx, p, e, creds, profile, r.GroupID)
		}
	}
	if err != nil {
		msg := connectError(err).Error()
		_ = s.W.FinishTelegramCode(ctx, db.FinishTelegramCodeParams{Code: r.Code, ChannelID: "", Error: truncateRunes(msg, 500)})
		providers.TelegramSay(ctx, token, c.ChatID, "Could not connect this chat: "+msg)
		return
	}
	_ = s.W.FinishTelegramCode(ctx, db.FinishTelegramCodeParams{Code: r.Code, ChannelID: ch.ID, Error: ""})
	// The command did its job: it goes, so the chat stays clean; a bot that may not delete says so.
	if providers.TelegramDelete(ctx, token, c.ChatID, c.MessageID) != nil {
		providers.TelegramSay(ctx, token, c.ChatID, "Connected to staqly. I may not delete messages here, so please remove the /connect message yourself.")
	}
}
