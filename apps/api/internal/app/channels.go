package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Channel is a connected account on a platform. Credentials never leave the
// server: they are sealed in the row and only opened to publish or refresh.
type Channel struct {
	ID         string         `json:"id"`
	Provider   string         `json:"provider"`
	Name       string         `json:"name"`
	Handle     string         `json:"handle"`
	AvatarURL  string         `json:"avatar_url"`
	ProfileURL string         `json:"profile_url"`
	Status     string         `json:"status"`
	Settings   map[string]any `json:"settings"`
	// Links is what this channel does with the links in a post's text: keep, remove or defang (shown as
	// example[.]com). Set it with settings.links; without one the platform's default applies.
	Links string `json:"links"`
	// GroupID is the channel group it belongs to (every channel is in exactly one; Default unless moved).
	GroupID   string `json:"group_id"`
	LastError string `json:"last_error"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	// Activity is read from the channel's targets; only GET /channels and GET /channels/{id} include it.
	Activity *ChannelActivity `json:"activity,omitempty"`
}

// ChannelActivity: when the channel last published (and which post), its next scheduled post and how many
// posts went out on it in the last 30 days.
type ChannelActivity struct {
	LastPublishedAt *string `json:"last_published_at"`
	LastPostID      *string `json:"last_post_id"`
	NextPublishAt   *string `json:"next_publish_at"`
	Published30d    int64   `json:"published_30d"`
}

const activityWindow = 30 * 24 * time.Hour

func toChannelOut(c db.Channel) Channel {
	out := Channel{ID: c.ID, Provider: c.Provider, Name: c.Name, Handle: c.Handle, AvatarURL: c.AvatarUrl, ProfileURL: c.ProfileUrl,
		Status: c.Status, Settings: map[string]any{}, GroupID: c.GroupID, LastError: c.LastError,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
	_ = json.Unmarshal([]byte(c.Settings), &out.Settings)
	out.Links = channelLinks(c)
	return out
}

// channelLinks is the links mode a channel publishes with: its own setting, else its platform's default.
func channelLinks(c db.Channel) string {
	var settings struct {
		Links string `json:"links"`
	}
	_ = json.Unmarshal([]byte(c.Settings), &settings)
	e, ok := providers.Get(c.Provider)
	if !ok {
		return providers.LinksKeep
	}
	if l := e.Limits(); l.AllowsLinks(settings.Links) {
		return settings.Links
	}
	return e.Limits().Links
}

// checkChannelSettings validates the channel settings staqly reads itself.
func checkChannelSettings(e providers.Entry, settings map[string]any) error {
	v, ok := settings["links"]
	if !ok || v == nil {
		return nil
	}
	if m, _ := v.(string); !e.Limits().AllowsLinks(m) {
		return invalid("settings.links", "must be one of "+strings.Join(e.Limits().LinkModes, ", ")+" for this platform")
	}
	return nil
}

// ProviderInfo is one catalog entry as clients see it.
type ProviderInfo struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Auth             string            `json:"auth"`
	Status           string            `json:"status"`
	Limits           providers.Limits  `json:"limits"`
	CredentialFields []providers.Field `json:"credential_fields"`
	SettingsFields   []providers.Field `json:"settings_fields"`
	AppConfigured    bool              `json:"app_configured"`
	DocsURL          string            `json:"docs_url"`
}

func providerInfo(e providers.Entry, appConfigured bool) ProviderInfo {
	info := ProviderInfo{ID: e.ID(), Name: e.Name(), Auth: string(e.Auth()), Status: e.Status, Limits: e.Limits(),
		CredentialFields: e.CredentialFields(), SettingsFields: e.SettingsFields(), AppConfigured: appConfigured, DocsURL: e.DocsURL()}
	if info.CredentialFields == nil {
		info.CredentialFields = []providers.Field{}
	}
	if info.SettingsFields == nil {
		info.SettingsFields = []providers.Field{}
	}
	if e.Auth() != providers.AuthOAuth2 {
		info.AppConfigured = true // nothing to register
	}
	return info
}

// ListProviders is the catalog: every platform, available or soon, with what connecting it needs.
func (s *Service) ListProviders(ctx context.Context, p Principal) ([]ProviderInfo, error) {
	if err := p.require(ScopeChannelsRead); err != nil {
		return nil, err
	}
	return s.providerCatalog(ctx, p.WorkspaceID)
}

// providerCatalog is the catalog as one workspace sees it (shared with connect links' public page).
func (s *Service) providerCatalog(ctx context.Context, workspaceID string) ([]ProviderInfo, error) {
	configured := map[string]bool{}
	ids, err := s.Q.ListProviderApps(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		configured[id] = true
	}
	out := []ProviderInfo{}
	for _, e := range providers.All() {
		if !s.providerEnabled(e.ID()) {
			continue // turned off on this server: no tile, no connecting, no publishing
		}
		app, _ := s.serverApp(e.ID())
		info := providerInfo(e, configured[e.ID()] || app.ClientID != "")
		if !info.AppConfigured {
			info.Status = providers.StatusSoon // an OAuth platform waits for its app's keys in the environment
		}
		out = append(out, info)
	}
	return out, nil
}

// providerEnabled reads PROVIDER_<ID>_ENABLED (default true); false turns the platform off on this server.
func (s *Service) providerEnabled(providerID string) bool {
	on, err := strconv.ParseBool(s.Env(providerEnv(providerID)+"ENABLED", "true"))
	return err != nil || on
}

// providerEnv is the prefix of a provider's settings: PROVIDER_X_, PROVIDER_LINKEDIN_PAGE_.
func providerEnv(providerID string) string {
	return "PROVIDER_" + strings.ToUpper(providerID) + "_"
}

// serverApp is the OAuth client the operator configured for a provider (PROVIDER_<ID>_CLIENT_ID/SECRET).
func (s *Service) serverApp(providerID string) (providers.App, bool) {
	key := providerEnv(providerID) + "CLIENT_"
	app := providers.App{ClientID: s.Env(key+"ID", ""), ClientSecret: s.Env(key+"SECRET", "")}
	return app, app.ClientID != ""
}

// providerApp: the workspace's developer app, else the server's.
func (s *Service) providerApp(ctx context.Context, workspaceID, providerID string) (providers.App, error) {
	row, err := s.Q.GetProviderApp(ctx, db.GetProviderAppParams{WorkspaceID: workspaceID, Provider: providerID})
	if err == nil {
		var app providers.App
		if err := s.unseal(row.Credentials, &app); err != nil {
			return app, err
		}
		return app, nil
	}
	if !isNoRows(err) {
		return providers.App{}, err
	}
	if app, ok := s.serverApp(providerID); ok {
		return app, nil
	}
	return providers.App{}, &Error{Code: "app_not_configured", Status: 409,
		Message: "this platform needs an app of your own first: register one with the platform and save its client id and secret for this provider"}
}

func (s *Service) availableProvider(id string) (providers.Entry, error) {
	e, ok := providers.Get(id)
	if !ok {
		return e, invalid("provider", "unknown provider")
	}
	if !s.providerEnabled(id) {
		return e, &Error{Code: "provider_disabled", Status: 409, Message: e.Name() + " is turned off on this server"}
	}
	if e.Status != providers.StatusAvailable {
		return e, &Error{Code: "provider_soon", Status: 409, Message: e.Name() + " is not available in this version yet"}
	}
	return e, nil
}

// SetProviderApp stores a workspace's own OAuth client for a provider (local mode: the person registers the app).
func (s *Service) SetProviderApp(ctx context.Context, p Principal, providerID string, app providers.App) error {
	if err := p.require(ScopeChannelsManage); err != nil {
		return err
	}
	e, ok := providers.Get(providerID)
	if !ok {
		return invalid("provider", "unknown provider")
	}
	if e.Auth() != providers.AuthOAuth2 {
		return invalid("provider", "this provider connects with a token, no app is needed")
	}
	if strings.TrimSpace(app.ClientID) == "" || strings.TrimSpace(app.ClientSecret) == "" {
		return invalid("client_id", "client_id and client_secret are required")
	}
	sealed, err := s.seal(app)
	if err != nil {
		return err
	}
	return s.W.UpsertProviderApp(ctx, db.UpsertProviderAppParams{WorkspaceID: p.WorkspaceID, Provider: providerID, Credentials: sealed, UpdatedAt: s.now()})
}

func (s *Service) DeleteProviderApp(ctx context.Context, p Principal, providerID string) error {
	if err := p.require(ScopeChannelsManage); err != nil {
		return err
	}
	n, err := s.W.DeleteProviderApp(ctx, db.DeleteProviderAppParams{WorkspaceID: p.WorkspaceID, Provider: providerID})
	if err == nil && n == 0 {
		return notFound("developer app")
	}
	return err
}

// ChannelInput connects a Token or None provider: the fields its catalog entry asks for.
type ChannelInput struct {
	Provider string            `json:"provider"`
	Name     string            `json:"name"`
	Fields   map[string]string `json:"fields"`
	Settings map[string]any    `json:"settings"`
	// GroupID puts the channel in a channel group (empty: Default).
	GroupID string `json:"group_id"`
}

// ChannelPatch renames, changes defaults, turns a channel off and on, or (Token providers) reconnects it
// with fresh credential fields.
type ChannelPatch struct {
	Name     *string           `json:"name"`
	Settings map[string]any    `json:"settings"`
	Status   *string           `json:"status"`
	Fields   map[string]string `json:"fields"`
	// GroupID moves the channel to another channel group ("" moves it to Default).
	GroupID *string `json:"group_id"`
}

// ChannelFilter narrows GET /channels to one channel group.
type ChannelFilter struct {
	Group string
}

const connectTimeout = 30 * time.Second

func (s *Service) ConnectChannel(ctx context.Context, p Principal, in ChannelInput) (Channel, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return Channel{}, err
	}
	e, err := s.availableProvider(in.Provider)
	if err != nil {
		return Channel{}, err
	}
	if e.Auth() == providers.AuthOAuth2 {
		return Channel{}, invalid("provider", "this platform connects through its sign-in page: start with GET /channels/oauth/{provider}/start")
	}
	fields, err := credentialValues(e, in.Fields)
	if err != nil {
		return Channel{}, err
	}
	// The dashboard asks for a name for your own automation; API clients may leave it out (the platform's name is used).
	name := strings.TrimSpace(in.Name)
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	creds, profile, err := e.Connect(cctx, fields)
	if err != nil {
		return Channel{}, connectError(err)
	}
	if name == "" {
		name = firstNonEmpty(profile.Name, profile.Handle, e.Name())
	}
	return s.saveChannel(ctx, p, e, name, creds, profile, in.Settings, in.GroupID)
}

// credentialValues trims the submitted credential fields and checks the required ones that apply.
func credentialValues(e providers.Entry, in map[string]string) (map[string]string, error) {
	all := e.CredentialFields()
	fields := map[string]string{}
	for _, f := range all {
		v := strings.TrimSpace(in[f.Key])
		if f.Required && v == "" && f.Applies(in) {
			return nil, invalid("fields."+f.Key, f.Label+" is required")
		}
		if v != "" {
			fields[f.Key] = v
		}
	}
	return fields, nil
}

func connectError(err error) error {
	var ve *providers.ValidationError
	if errors.As(err, &ve) {
		return invalid("fields."+ve.Field, ve.Msg)
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: "connect_failed", Status: 422, Message: "the platform did not accept these details: " + err.Error()}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func (s *Service) saveChannel(ctx context.Context, p Principal, e providers.Entry, name string, creds providers.Credentials,
	profile providers.Profile, settings map[string]any, groupID string) (Channel, error) {
	name, err := trimmed(name, "name", 100, true)
	if err != nil {
		return Channel{}, err
	}
	sealed, err := s.seal(creds)
	if err != nil {
		return Channel{}, err
	}
	if settings == nil {
		settings = map[string]any{}
	}
	if err := checkChannelSettings(e, settings); err != nil {
		return Channel{}, err
	}
	now := s.now()
	row := db.Channel{ID: newID("ch"), WorkspaceID: p.WorkspaceID, Provider: e.ID(), Name: name, Handle: profile.Handle,
		AvatarUrl: profile.AvatarURL, ProfileUrl: profile.ProfileURL, Status: "connected", Credentials: sealed,
		Settings: mustJSON(settings), CreatedAt: now, UpdatedAt: now}
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := s.useChannelQuota(ctx, q, p.WorkspaceID); err != nil {
			return err
		}
		if row.GroupID, err = s.channelGroupFor(ctx, q, p.WorkspaceID, groupID); err != nil {
			return err
		}
		if err := q.CreateChannel(ctx, db.CreateChannelParams{ID: row.ID, WorkspaceID: row.WorkspaceID, Provider: row.Provider,
			Name: row.Name, Handle: row.Handle, AvatarUrl: row.AvatarUrl, ProfileUrl: row.ProfileUrl, Status: row.Status,
			Credentials: row.Credentials, Settings: row.Settings, GroupID: row.GroupID, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		_, err := s.emitRef(ctx, q, p, "channel.connected", Ref{Channel: row.ID}, map[string]any{"channel": toChannelOut(row)})
		return err
	})
	if err != nil {
		return Channel{}, err
	}
	s.scheduleRefresh(ctx, row, creds)
	return toChannelOut(row), nil
}

// ---- OAuth ----

// oauthState is self-contained and signed: provider, workspace, nonce and expiry, so the callback needs no
// server-side state and a state from another workspace or an old one is refused.
type oauthState struct {
	Provider  string `json:"p"`
	Workspace string `json:"w"`
	Nonce     string `json:"n"`
	Expires   int64  `json:"e"`
	// Link: the connect link (channel_links.id) a customer started from; "" for the team's own sign-ins. A state
	// carries one or the other, so neither flow can finish the other's sign-in.
	Link string `json:"l,omitempty"`
}

func (s *Service) signState(st oauthState) (string, error) {
	if len(s.Cfg.SecretKey) == 0 {
		return "", errNoSecretKey
	}
	body := base64.RawURLEncoding.EncodeToString([]byte(mustJSON(st)))
	mac := hmac.New(sha256.New, s.Cfg.SecretKey)
	mac.Write([]byte(body))
	return body + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func (s *Service) verifyState(raw, provider, workspace string) (oauthState, error) {
	st, err := s.decodeState(raw)
	if err != nil {
		return st, err
	}
	if st.Provider != provider || st.Workspace != workspace {
		return st, invalid("state", "belongs to another connection")
	}
	return st, nil
}

// decodeState checks a state's signature and expiry; the caller checks whose it is.
func (s *Service) decodeState(raw string) (oauthState, error) {
	var st oauthState
	body, sig, ok := strings.Cut(raw, ".")
	if !ok || len(s.Cfg.SecretKey) == 0 {
		return st, invalid("state", "is invalid")
	}
	mac := hmac.New(sha256.New, s.Cfg.SecretKey)
	mac.Write([]byte(body))
	want, err := hex.DecodeString(sig)
	if err != nil || !hmac.Equal(want, mac.Sum(nil)) {
		return st, invalid("state", "is invalid")
	}
	b, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || json.Unmarshal(b, &st) != nil {
		return st, invalid("state", "is invalid")
	}
	if s.Now().Unix() > st.Expires {
		return st, &Error{Code: "state_expired", Status: 410, Message: "the connection took too long; start again"}
	}
	return st, nil
}

// pkceVerifier is the PKCE code_verifier of one sign-in (RFC 7636). It is derived from the state's nonce with
// the server's key, so the callback recomputes it and nothing is stored; without the key nobody can.
func (s *Service) pkceVerifier(st oauthState) string {
	mac := hmac.New(sha256.New, s.Cfg.SecretKey)
	mac.Write([]byte("pkce:" + st.Provider + ":" + st.Workspace + ":" + st.Nonce))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) // 43 characters, as RFC 7636 asks
}

// withVerifier copies the caller's extras and sets the server-side PKCE verifier (a client cannot supply one).
func withVerifier(extra map[string]string, verifier string) map[string]string {
	out := make(map[string]string, len(extra)+1)
	for k, v := range extra {
		out[k] = v
	}
	out["code_verifier"] = verifier
	return out
}

// StartOAuth returns the platform's sign-in URL for a provider. redirect is where the platform sends the
// browser back (the dashboard's callback page), which then calls CompleteOAuth.
func (s *Service) StartOAuth(ctx context.Context, p Principal, providerID, redirect string, extra map[string]string) (string, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return "", err
	}
	e, err := s.availableProvider(providerID)
	if err != nil {
		return "", err
	}
	if e.Auth() != providers.AuthOAuth2 {
		return "", invalid("provider", "this platform connects with a token: POST /channels with its fields")
	}
	if u, err := url.Parse(redirect); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", invalid("redirect", "must be an absolute http(s) URL")
	}
	app, err := s.providerApp(ctx, p.WorkspaceID, providerID)
	if err != nil {
		return "", err
	}
	st := oauthState{Provider: providerID, Workspace: p.WorkspaceID, Nonce: randomString(12), Expires: s.Now().Add(10 * time.Minute).Unix()}
	state, err := s.signState(st)
	if err != nil {
		return "", err
	}
	return e.AuthURL(app, redirect, state, withVerifier(extra, s.pkceVerifier(st)))
}

// CompleteOAuth exchanges the code the platform sent back and saves the account as a channel. An existing
// channel of the same provider and handle is reconnected instead of duplicated.
func (s *Service) CompleteOAuth(ctx context.Context, p Principal, providerID, code, state, redirect string, extra map[string]string) (Channel, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return Channel{}, err
	}
	e, err := s.availableProvider(providerID)
	if err != nil {
		return Channel{}, err
	}
	st, err := s.verifyState(state, providerID, p.WorkspaceID)
	if err != nil {
		return Channel{}, err
	}
	if st.Link != "" {
		return Channel{}, invalid("state", "belongs to another connection")
	}
	return s.completeOAuth(ctx, p, e, st, code, redirect, extra, "")
}

// completeOAuth exchanges the code and saves (or reconnects) the channel; a new channel joins groupID ("" = Default).
func (s *Service) completeOAuth(ctx context.Context, p Principal, e providers.Entry, st oauthState, code, redirect string,
	extra map[string]string, groupID string) (Channel, error) {
	providerID := e.ID()
	if code == "" {
		return Channel{}, invalid("code", "is required")
	}
	app, err := s.providerApp(ctx, p.WorkspaceID, providerID)
	if err != nil {
		return Channel{}, err
	}
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	creds, profile, err := e.Exchange(cctx, app, redirect, code, withVerifier(extra, s.pkceVerifier(st)))
	if err != nil {
		return Channel{}, connectError(err)
	}
	if profile.Handle != "" {
		rows, err := s.Q.ListChannels(ctx, p.WorkspaceID)
		if err != nil {
			return Channel{}, err
		}
		for _, row := range rows {
			if row.Provider == providerID && row.Handle == profile.Handle {
				return s.reconnectChannel(ctx, p, row, creds, profile)
			}
		}
	}
	return s.saveChannel(ctx, p, e, firstNonEmpty(profile.Name, profile.Handle, e.Name()), creds, profile, nil, groupID)
}

func (s *Service) reconnectChannel(ctx context.Context, p Principal, row db.Channel, creds providers.Credentials, profile providers.Profile) (Channel, error) {
	sealed, err := s.seal(creds)
	if err != nil {
		return Channel{}, err
	}
	row.Credentials, row.Status, row.LastError, row.UpdatedAt = sealed, "connected", "", s.now()
	row.AvatarUrl, row.ProfileUrl = firstNonEmpty(profile.AvatarURL, row.AvatarUrl), firstNonEmpty(profile.ProfileURL, row.ProfileUrl)
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := q.UpdateChannel(ctx, db.UpdateChannelParams{Name: row.Name, Handle: row.Handle, AvatarUrl: row.AvatarUrl,
			ProfileUrl: row.ProfileUrl, Status: row.Status, Credentials: row.Credentials, Settings: row.Settings, LastError: "",
			UpdatedAt: row.UpdatedAt, WorkspaceID: p.WorkspaceID, ID: row.ID}); err != nil {
			return err
		}
		_, err := s.emitRef(ctx, q, p, "channel.connected", Ref{Channel: row.ID}, map[string]any{"channel": toChannelOut(row), "reconnected": true})
		return err
	})
	if err != nil {
		return Channel{}, err
	}
	s.scheduleRefresh(ctx, row, creds)
	return toChannelOut(row), nil
}

// ---- read, update, delete, test ----

// ListChannels lists the workspace's channels with their group and activity; the filter keeps one channel group.
func (s *Service) ListChannels(ctx context.Context, p Principal, f ChannelFilter) ([]Channel, error) {
	if err := p.require(ScopeChannelsRead); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListChannels(ctx, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	activity, err := s.channelActivity(ctx, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	group, err := s.groupResolver(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	inGroup := strings.TrimSpace(f.Group)
	out := make([]Channel, 0, len(rows))
	for _, r := range rows {
		c := toChannelOut(r)
		c.GroupID = group(r.GroupID)
		if inGroup != "" && c.GroupID != inGroup {
			continue
		}
		c.Activity = activity(c.ID)
		out = append(out, c)
	}
	return out, nil
}

// channelActivity reads every channel's activity in one query; the result answers per channel id.
func (s *Service) channelActivity(ctx context.Context, workspaceID string) (func(id string) *ChannelActivity, error) {
	rows, err := s.Q.ChannelActivity(ctx, db.ChannelActivityParams{WorkspaceID: workspaceID,
		Since: nullStr(formatTime(s.Now().UTC().Add(-activityWindow)))})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]db.ChannelActivityRow, len(rows))
	for _, r := range rows {
		byID[r.ChannelID] = r
	}
	opt := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	return func(id string) *ChannelActivity {
		r := byID[id]
		return &ChannelActivity{LastPublishedAt: opt(r.LastPublishedAt), LastPostID: opt(r.LastPostID),
			NextPublishAt: opt(r.NextPublishAt), Published30d: r.PublishedSince}
	}, nil
}

func (s *Service) GetChannel(ctx context.Context, p Principal, id string) (Channel, error) {
	if err := p.require(ScopeChannelsRead); err != nil {
		return Channel{}, err
	}
	row, err := s.channelRow(ctx, p, id)
	if err != nil {
		return Channel{}, err
	}
	activity, err := s.channelActivity(ctx, p.WorkspaceID)
	if err != nil {
		return Channel{}, err
	}
	group, err := s.groupResolver(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return Channel{}, err
	}
	out := toChannelOut(row)
	out.GroupID = group(row.GroupID)
	out.Activity = activity(row.ID)
	return out, nil
}

func (s *Service) channelRow(ctx context.Context, p Principal, id string) (db.Channel, error) {
	row, err := s.Q.GetChannel(ctx, db.GetChannelParams{WorkspaceID: p.WorkspaceID, ID: id})
	if isNoRows(err) {
		return row, notFound("channel")
	}
	return row, err
}

func (s *Service) UpdateChannel(ctx context.Context, p Principal, id string, in ChannelPatch) (Channel, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return Channel{}, err
	}
	// Reconnecting with new fields talks to the platform first, outside the transaction.
	var reconnected *struct {
		sealed  string
		profile providers.Profile
	}
	if in.Fields != nil {
		row, err := s.channelRow(ctx, p, id)
		if err != nil {
			return Channel{}, err
		}
		e, err := s.availableProvider(row.Provider)
		if err != nil {
			return Channel{}, err
		}
		if e.Auth() == providers.AuthOAuth2 {
			return Channel{}, invalid("fields", "this platform reconnects through its sign-in page: start with GET /channels/oauth/{provider}/start")
		}
		fields, err := credentialValues(e, in.Fields)
		if err != nil {
			return Channel{}, err
		}
		cctx, cancel := context.WithTimeout(ctx, connectTimeout)
		defer cancel()
		creds, profile, err := e.Connect(cctx, fields)
		if err != nil {
			return Channel{}, connectError(err)
		}
		sealed, err := s.seal(creds)
		if err != nil {
			return Channel{}, err
		}
		reconnected = &struct {
			sealed  string
			profile providers.Profile
		}{sealed, profile}
	}
	var out db.Channel
	err := s.tx(ctx, func(q *db.Queries) error {
		row, err := q.GetChannel(ctx, db.GetChannelParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("channel")
		}
		if err != nil {
			return err
		}
		if reconnected != nil {
			row.Credentials, row.Status, row.LastError = reconnected.sealed, "connected", ""
			if reconnected.profile.Handle != "" {
				row.Handle = reconnected.profile.Handle
			}
			if reconnected.profile.AvatarURL != "" {
				row.AvatarUrl = reconnected.profile.AvatarURL
			}
			if reconnected.profile.ProfileURL != "" {
				row.ProfileUrl = reconnected.profile.ProfileURL
			}
		}
		if in.Name != nil {
			if row.Name, err = trimmed(*in.Name, "name", 100, true); err != nil {
				return err
			}
		}
		if in.Settings != nil {
			e, ok := providers.Get(row.Provider)
			if !ok {
				return invalid("settings", "this channel's provider is not part of this build")
			}
			if err := checkChannelSettings(e, in.Settings); err != nil {
				return err
			}
			row.Settings = mustJSON(in.Settings)
		}
		if in.GroupID != nil {
			if row.GroupID, err = s.channelGroupFor(ctx, q, p.WorkspaceID, *in.GroupID); err != nil {
				return err
			}
			if err := q.SetChannelGroup(ctx, db.SetChannelGroupParams{GroupID: row.GroupID, UpdatedAt: s.now(),
				WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
				return err
			}
		}
		if in.Status != nil {
			switch *in.Status {
			case "disabled":
				row.Status = "disabled"
			case "connected":
				if row.Status == "needs_reconnect" && reconnected == nil {
					return conflict("this channel needs reconnecting: send its credential fields again or go through its sign-in page")
				}
				row.Status, row.LastError = "connected", ""
			default:
				return invalid("status", "must be connected or disabled")
			}
		}
		row.UpdatedAt = s.now()
		if err := q.UpdateChannel(ctx, db.UpdateChannelParams{Name: row.Name, Handle: row.Handle, AvatarUrl: row.AvatarUrl,
			ProfileUrl: row.ProfileUrl, Status: row.Status, Credentials: row.Credentials, Settings: row.Settings,
			LastError: row.LastError, UpdatedAt: row.UpdatedAt, WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		out = row
		_, err = s.emitRef(ctx, q, p, "channel.updated", Ref{Channel: id}, map[string]any{"channel": toChannelOut(row)})
		return err
	})
	if err != nil {
		return Channel{}, err
	}
	return toChannelOut(out), nil
}

// DeleteChannel removes the account; targets on it go with it (posts keep their other targets).
func (s *Service) DeleteChannel(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeChannelsManage); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		row, err := q.GetChannel(ctx, db.GetChannelParams{WorkspaceID: p.WorkspaceID, ID: id})
		if isNoRows(err) {
			return notFound("channel")
		}
		if err != nil {
			return err
		}
		// Posts still publishing to it lose that target with the channel (ON DELETE CASCADE): their other targets
		// decide how they end, or they would wait for this one for good.
		publishing, err := q.PublishingPostsOfChannel(ctx, id)
		if err != nil {
			return err
		}
		if _, err := q.DeleteChannel(ctx, db.DeleteChannelParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		if err := s.cancelJobs(ctx, q, "refresh_channel:"+id); err != nil {
			return err
		}
		for _, postID := range publishing {
			for _, k := range []string{"publish_target:", "target_timeout:"} {
				if err := s.cancelJobs(ctx, q, k+postID+":"+id); err != nil {
					return err
				}
			}
			if err := s.settlePost(ctx, q, p, p.WorkspaceID, postID); err != nil {
				return err
			}
		}
		_, err = s.emitRef(ctx, q, p, "channel.disconnected", Ref{Channel: id}, map[string]any{"channel": toChannelOut(row)})
		return err
	})
}

// TestChannel checks the stored credentials against the platform and reports the profile.
func (s *Service) TestChannel(ctx context.Context, p Principal, id string) (map[string]any, error) {
	if err := p.require(ScopeChannelsManage); err != nil {
		return nil, err
	}
	row, err := s.channelRow(ctx, p, id)
	if err != nil {
		return nil, err
	}
	e, ok := providers.Get(row.Provider)
	if !ok {
		return nil, conflict("this channel's provider is not part of this build")
	}
	if e.Auth() == providers.AuthOAuth2 {
		defer lockRefresh(row.ID)() // a refresh rotates the refresh token: read it under the lock
	}
	_, creds, err := s.storedCredentials(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	var profile providers.Profile
	switch e.Auth() {
	case providers.AuthOAuth2:
		app, err := s.providerApp(ctx, p.WorkspaceID, row.Provider)
		if err != nil {
			return nil, err
		}
		fresh, err := e.Refresh(cctx, app, creds)
		if err != nil {
			if errors.Is(err, providers.ErrReauth) { // a platform that can't be reached now is no reason to reconnect
				s.markChannel(ctx, row.ID, "needs_reconnect", err.Error())
			}
			return map[string]any{"ok": false, "error": err.Error()}, nil
		}
		if sealed, err := s.seal(fresh); err == nil {
			_ = s.W.SetChannelCredentials(ctx, db.SetChannelCredentialsParams{Credentials: sealed, UpdatedAt: s.now(), ID: row.ID})
		}
		profile = providers.Profile{Handle: row.Handle, Name: row.Name}
	default:
		_, profile, err = e.Connect(cctx, map[string]string(creds))
		if err != nil {
			s.markChannel(ctx, row.ID, "needs_reconnect", err.Error())
			return map[string]any{"ok": false, "error": err.Error()}, nil
		}
	}
	if row.Status == "needs_reconnect" {
		s.markChannel(ctx, row.ID, "connected", "")
	}
	return map[string]any{"ok": true, "profile": profile}, nil
}

func (s *Service) markChannel(ctx context.Context, id, status, lastError string) {
	if err := s.W.SetChannelHealth(ctx, db.SetChannelHealthParams{Status: status, LastError: truncateRunes(lastError, 500), UpdatedAt: s.now(), ID: id}); err != nil {
		s.logger(ctx).Warn("channel health not saved", "channel_id", id, "err", err)
	}
}

// scheduleRefresh queues a token refresh a day before the credentials expire ("expires_at", RFC3339 or unix).
func (s *Service) scheduleRefresh(ctx context.Context, row db.Channel, creds providers.Credentials) {
	exp, ok := credentialsExpiry(creds)
	if !ok {
		return
	}
	runAt := exp.Add(-24 * time.Hour)
	if runAt.Before(s.Now()) {
		// Short-lived tokens (X: 2 hours) refresh half an hour before they run out, not again and again
		// right away; one about to expire is refreshed when it is used (refreshIfDue).
		runAt = exp.Add(-30 * time.Minute)
		if runAt.Before(s.Now().Add(time.Minute)) {
			return
		}
	}
	_ = s.enqueue(ctx, s.W, row.WorkspaceID, "refresh_channel", "refresh_channel:"+row.ID, map[string]any{"channel_id": row.ID}, runAt, 3)
}

func credentialsExpiry(creds providers.Credentials) (time.Time, bool) {
	v := creds["expires_at"]
	if v == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, true
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Unix(n, 0), true
	}
	return time.Time{}, false
}

// refreshChannel is the refresh_channel job: a failed refresh marks the channel needs_reconnect.
func (s *Service) refreshChannel(ctx context.Context, channelID string) error {
	defer lockRefresh(channelID)() // the credentials are read under the lock: a publish may have just refreshed them
	row, err := s.Q.GetChannelByID(ctx, channelID)
	if isNoRows(err) {
		return nil // gone
	}
	if err != nil {
		return err
	}
	e, ok := providers.Get(row.Provider)
	if !ok || e.Auth() != providers.AuthOAuth2 || row.Status == "disabled" {
		return nil
	}
	var creds providers.Credentials
	if err := s.unseal(row.Credentials, &creds); err != nil {
		return err
	}
	app, err := s.providerApp(ctx, row.WorkspaceID, row.Provider)
	if err != nil {
		return s.channelNeedsReconnect(ctx, row, err)
	}
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	fresh, err := e.Refresh(cctx, app, creds)
	if errors.Is(err, providers.ErrReauth) {
		return s.channelNeedsReconnect(ctx, row, err)
	}
	if err != nil {
		return err // the platform could not be reached: the job tries again; its last failure needs reconnecting
	}
	sealed, err := s.seal(fresh)
	if err != nil {
		return err
	}
	if err := s.W.SetChannelCredentials(ctx, db.SetChannelCredentialsParams{Credentials: sealed, UpdatedAt: s.now(), ID: row.ID}); err != nil {
		return err
	}
	if old, ok := credentialsExpiry(creds); ok {
		if exp, ok := credentialsExpiry(fresh); ok && exp.Equal(old) {
			// Nothing was renewed (a platform without refresh tokens, like LinkedIn for most apps): look again when
			// the token runs out, so the channel turns needs_reconnect and alerts then, not at its next post.
			_ = s.enqueue(ctx, s.W, row.WorkspaceID, "refresh_channel", "refresh_channel:"+row.ID, map[string]any{"channel_id": row.ID}, exp.Add(time.Minute), 3)
			return nil
		}
	}
	s.scheduleRefresh(ctx, row, fresh)
	return nil
}

// refreshLocks: one token refresh per channel at a time in this process. Platforms like X rotate refresh
// tokens (each works once), so two runs refreshing with the same one would fail the second and mark the
// channel needs_reconnect. ponytail: per process and never pruned (one small mutex per channel ever
// refreshed); several servers on one database would need a row lock or an advisory lock.
var refreshLocks sync.Map // channel id -> *sync.Mutex

func lockRefresh(channelID string) func() {
	m, _ := refreshLocks.LoadOrStore(channelID, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// storedCredentials reads a channel's credentials as saved now (another run may have refreshed them).
func (s *Service) storedCredentials(ctx context.Context, channelID string) (db.Channel, providers.Credentials, error) {
	row, err := s.Q.GetChannelByID(ctx, channelID)
	if err != nil {
		return row, nil, err
	}
	var creds providers.Credentials
	if err := s.unseal(row.Credentials, &creds); err != nil {
		return row, nil, err
	}
	return row, creds, nil
}

// refreshIfDue refreshes an OAuth channel whose access token runs out within two minutes (X tokens last two
// hours) and saves the new credentials; a failed refresh marks the channel needs_reconnect.
func (s *Service) refreshIfDue(ctx context.Context, row db.Channel, e providers.Entry, creds providers.Credentials) (providers.Credentials, error) {
	due := func(c providers.Credentials) bool {
		exp, ok := credentialsExpiry(c)
		return ok && !exp.After(s.Now().Add(2*time.Minute))
	}
	if e.Auth() != providers.AuthOAuth2 || !due(creds) {
		return creds, nil
	}
	defer lockRefresh(row.ID)()
	if _, stored, err := s.storedCredentials(ctx, row.ID); err == nil {
		if !due(stored) {
			return stored, nil // refreshed by another run while this one waited
		}
		creds = stored
	}
	app, err := s.providerApp(ctx, row.WorkspaceID, row.Provider)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	fresh, err := e.Refresh(cctx, app, creds)
	if errors.Is(err, providers.ErrReauth) {
		_ = s.channelNeedsReconnect(ctx, row, err)
		return nil, &providers.Failure{Kind: providers.ErrReauth, Msg: "the sign-in expired and could not be renewed: " + err.Error()}
	}
	if err != nil {
		// Not reachable right now: the publish is retried, the channel stays connected.
		return nil, &providers.Failure{Msg: "the sign-in could not be renewed right now: " + err.Error()}
	}
	sealed, err := s.seal(fresh)
	if err != nil {
		return nil, err
	}
	if err := s.W.SetChannelCredentials(ctx, db.SetChannelCredentialsParams{Credentials: sealed, UpdatedAt: s.now(), ID: row.ID}); err != nil {
		return nil, err
	}
	s.scheduleRefresh(ctx, row, fresh)
	return fresh, nil
}

func (s *Service) channelNeedsReconnect(ctx context.Context, row db.Channel, cause error) error {
	sys := Principal{WorkspaceID: row.WorkspaceID, Kind: PrincipalSystem, ID: "channels", Name: "Channels"}
	return s.tx(ctx, func(q *db.Queries) error {
		if err := q.SetChannelHealth(ctx, db.SetChannelHealthParams{Status: "needs_reconnect", LastError: truncateRunes(cause.Error(), 500), UpdatedAt: s.now(), ID: row.ID}); err != nil {
			return err
		}
		row.Status = "needs_reconnect"
		_, err := s.emitRef(ctx, q, sys, "channel.needs_reconnect", Ref{Channel: row.ID}, map[string]any{"channel": toChannelOut(row), "error": cause.Error()})
		return err
	})
}
