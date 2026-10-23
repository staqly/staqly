// Package providers holds the platforms staqly publishes to: one Provider per
// platform (Mastodon, Bluesky, X...) plus "custom", your own automation. The
// app layer owns channels (connected accounts) and posts; a provider only
// knows how to connect an account and how to publish one post to it.
package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// AuthKind says how an account is connected.
type AuthKind string

const (
	AuthOAuth2 AuthKind = "oauth2" // the person is sent to the platform and comes back with a code
	AuthToken  AuthKind = "token"  // the person pastes a token, an app password, a bot token...
	AuthNone   AuthKind = "none"   // nothing to connect (custom)
)

// Status of a provider in this build.
const (
	StatusAvailable = "available"
	StatusSoon      = "soon"
)

// Field describes one input the dashboard renders: a credential a person pastes, or a per-target setting.
type Field struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Help  string `json:"help,omitempty"`
	// text, secret, url, select, textarea, boolean, number; list (comma-separated free values, e.g. tags or
	// usernames); channels (comma-separated ids of other connected channels of the same platform).
	Kind        string   `json:"kind"`
	Options     []Option `json:"options,omitempty"`
	Required    bool     `json:"required"`
	Default     string   `json:"default,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	// When shows the field (and makes Required count) only while another field has this value.
	When *Condition `json:"when,omitempty"`
	// MaxLength caps text, textarea and url values (characters); MaxItems caps list and channels values.
	MaxLength int `json:"max_length,omitempty"`
	MaxItems  int `json:"max_items,omitempty"`
}

// ListValues splits a list or channels value: comma-separated, trimmed, empty items dropped.
func ListValues(v string) []string {
	out := []string{}
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// CheckFields validates settings against their field definitions: required fields, select options,
// booleans, numbers, links, lengths and item counts. Keys the provider does not define are left alone
// (older clients may send them); channels values are checked by the app, which knows the workspace.
func CheckFields(fields []Field, s Settings) error {
	for _, f := range fields {
		if !f.Applies(s) {
			continue
		}
		v := strings.TrimSpace(s[f.Key])
		if v == "" {
			if f.Required && f.Default == "" {
				return invalid(f.Key, f.Label+" is required")
			}
			continue
		}
		switch f.Kind {
		case "select":
			ok := false
			for _, o := range f.Options {
				ok = ok || o.Value == v
			}
			if !ok {
				return invalid(f.Key, "choose one of the options for "+f.Label)
			}
		case "boolean":
			if v != "true" && v != "false" {
				return invalid(f.Key, f.Label+" is true or false")
			}
		case "number":
			if _, err := strconv.ParseFloat(v, 64); err != nil {
				return invalid(f.Key, f.Label+" must be a number")
			}
		case "url":
			if u, err := url.Parse(v); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return invalid(f.Key, f.Label+" must be a link starting with https://")
			}
		case "list", "channels":
			if n := len(ListValues(v)); f.MaxItems > 0 && n > f.MaxItems {
				return invalid(f.Key, fmt.Sprintf("at most %d for %s", f.MaxItems, f.Label))
			}
		}
		if f.MaxLength > 0 && utf8.RuneCountInString(v) > f.MaxLength {
			return invalid(f.Key, fmt.Sprintf("at most %d characters for %s", f.MaxLength, f.Label))
		}
	}
	return nil
}

// Condition: the field Key holds Value. The dashboard applies defaults as shown; the API only
// looks at what was sent, so a client that leaves the controlling field out keeps the old behaviour.
type Condition struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Applies says whether a field is in play given the submitted values.
func (f Field) Applies(values map[string]string) bool {
	return f.When == nil || strings.TrimSpace(values[f.When.Key]) == f.When.Value
}

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Limits the composer and the validation apply before publishing.
type Limits struct {
	MaxChars   int      `json:"max_chars"`             // 0 = no limit
	MaxMedia   int      `json:"max_media"`             // 0 = no media
	MediaKinds []string `json:"media_kinds,omitempty"` // image, video, gif
	NeedsTitle bool     `json:"needs_title"`           // a title setting is required (YouTube, Reddit, Dev.to...)
	NeedsMedia bool     `json:"needs_media"`           // text alone is not a post (Instagram, Pinterest, YouTube)
	NeedsLink  bool     `json:"needs_link"`            // a link setting is required (Pinterest)
	// Links is what a channel does with the links in a post's text unless its own "links" setting says
	// otherwise (LinksKeep when empty); LinkModes are the modes a channel may choose (all three when empty).
	// Platforms that charge for links or bury posts with them leave keep out.
	Links     string   `json:"links"`
	LinkModes []string `json:"link_modes"`
	// Image is what the platform refuses in a picture (its API's documented limits); nil when it states none.
	// The composer and the media editor check pictures against it; recommended shapes are not rules.
	Image *ImageRules `json:"image,omitempty"`
	// XWeighted: MaxChars is X's weighted length (TextLength), not characters.
	XWeighted bool `json:"-"`
}

// ImageRules are a platform's hard limits for one picture. Zero values mean no limit.
type ImageRules struct {
	MaxBytes  int64    `json:"max_bytes,omitempty"`
	Formats   []string `json:"formats,omitempty"`   // MIME types it takes; empty = any picture
	MinRatio  float64  `json:"min_ratio,omitempty"` // width / height
	MaxRatio  float64  `json:"max_ratio,omitempty"`
	MinWidth  int      `json:"min_width,omitempty"`
	MaxPixels int64    `json:"max_pixels,omitempty"` // width x height
	Note      string   `json:"note,omitempty"`       // which kind of post the ratio applies to, when not every one
}

// imageRules are the platforms' documented limits for pictures sent through their APIs.
var imageRules = func() map[string]*ImageRules {
	jpeg := []string{"image/jpeg"}
	igFeed := &ImageRules{MaxBytes: 8 << 20, Formats: jpeg, MinRatio: 0.8, MaxRatio: 1.91, MinWidth: 320,
		Note: "Feed posts take 4:5 to 1.91:1; stories and reels are 9:16."}
	return map[string]*ImageRules{
		"x":                  {MaxBytes: 5 << 20, Formats: []string{"image/jpeg", "image/png", "image/gif", "image/webp"}},
		"instagram":          igFeed,
		"instagram_business": igFeed,
		"threads":            {MaxBytes: 8 << 20, Formats: []string{"image/jpeg", "image/png"}, MinRatio: 0.1, MaxRatio: 10, MinWidth: 320},
		"facebook":           {MaxBytes: 10 << 20, Formats: []string{"image/jpeg", "image/png", "image/gif", "image/bmp", "image/tiff", "image/webp"}},
		"linkedin":           {Formats: []string{"image/jpeg", "image/png", "image/gif"}, MaxPixels: 36_152_320},
		"linkedin_page":      {Formats: []string{"image/jpeg", "image/png", "image/gif"}, MaxPixels: 36_152_320},
		"pinterest":          {MaxBytes: 20 << 20, Formats: []string{"image/jpeg", "image/png", "image/webp"}},
		"bluesky":            {MaxBytes: 1_000_000, Formats: []string{"image/jpeg", "image/png", "image/webp", "image/gif"}},
		"telegram":           {MaxBytes: 10 << 20, MinRatio: 0.05, MaxRatio: 20},
		"google_business":    {MaxBytes: 5 << 20, Formats: []string{"image/jpeg", "image/png"}, MinWidth: 250},
	}
}()

// AllowsLinks says whether a channel on this platform may use links mode m.
func (l Limits) AllowsLinks(m string) bool {
	for _, x := range l.LinkModes {
		if x == m {
			return true
		}
	}
	return false
}

// Credentials are what a channel stores, sealed: tokens, instance URLs, expiry.
type Credentials map[string]string

// App is an OAuth client the workspace (local) or the server (cloud) registered with the platform.
type App struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// Profile is what the platform knows the account as.
type Profile struct {
	Handle     string `json:"handle"`
	Name       string `json:"name"`
	AvatarURL  string `json:"avatar_url"`
	ProfileURL string `json:"profile_url"`
	// NoComments: this connection may not post comments under its posts (LinkedIn without the Community
	// Management API); the post's comments are then not sent on it.
	NoComments bool `json:"-"`
}

// Settings are the per-target values of SettingsFields (strings; booleans as "true"/"false").
type Settings map[string]string

// MediaFile is one attachment of a post. Providers read its bytes with Reader; a platform that fetches media
// itself gets SignedURL (a time-limited link to the bucket) when the server keeps files in S3.
type MediaFile struct {
	ID       string
	Name     string
	MimeType string
	Size     int64
	Path     string // local disk only (tests, older callers); Open wins when set
	URL      string // the API's own URL for the file (needs credentials)
	// SignedURL is a time-limited public link to the file in S3-compatible storage; "" on disk.
	SignedURL string
	// Open streams the file from the server's storage (disk or S3).
	Open func(ctx context.Context) (io.ReadCloser, error)
}

// Reader opens the file's bytes: from the server's storage, or from Path.
func (m MediaFile) Reader(ctx context.Context) (io.ReadCloser, error) {
	if m.Open != nil {
		return m.Open(ctx)
	}
	return os.Open(m.Path)
}

// PostContent is one post as a channel receives it (overrides already applied).
type PostContent struct {
	PostID    string
	ChannelID string
	Text      string
	Media     []MediaFile
	Tags      []string
	// Thread: the parts published as replies under the post after it (each with its own delay).
	// Providers that reply implement Replier; custom hands them to your automation in its payload.
	Thread []ThreadPart
}

// ThreadPart is one follow-up part of a post ("comment"), published as a reply DelaySeconds after the
// part before it (the post itself for the first one).
type ThreadPart struct {
	Text         string
	Media        []MediaFile
	DelaySeconds int64
}

// Replier is implemented by platforms that can publish a reply under a post they published: the app
// publishes a post's thread through it, one part per job, each under the one before.
type Replier interface {
	Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, s Settings) (Result, error)
}

// Reposter is implemented by platforms where another connected account can repost a published post (the
// "reposters" setting): the app runs one repost job per account once the post is out.
type Reposter interface {
	Repost(ctx context.Context, c Credentials, externalID string) error
}

// Metrics is what a platform reports about a published post. A nil field is a number the platform does not
// give (or not to this account), which is different from zero.
type Metrics struct {
	Impressions *int64 `json:"impressions,omitempty"`
	Likes       *int64 `json:"likes,omitempty"`
	Reposts     *int64 `json:"reposts,omitempty"`
	Replies     *int64 `json:"replies,omitempty"`
	Quotes      *int64 `json:"quotes,omitempty"`
	Bookmarks   *int64 `json:"bookmarks,omitempty"`
	LinkClicks  *int64 `json:"link_clicks,omitempty"`
}

// MetricsReader is implemented by platforms that report numbers for a post they published; the app reads them
// on demand and caches them on the target (never inside a DB transaction).
type MetricsReader interface {
	Metrics(ctx context.Context, c Credentials, externalID string) (Metrics, error)
}

// Result of a publish: how the platform names the post and where it lives.
type Result struct {
	ExternalID string `json:"external_id"`
	URL        string `json:"url"`
}

// ErrReauth: the platform no longer accepts the account's sign-in; the channel needs reconnecting and
// retrying will not help until it is.
var ErrReauth = errors.New("the account needs reconnecting")

// ErrRejected: the platform refused this post (duplicate, too long, not allowed); retrying will not help.
var ErrRejected = errors.New("the platform refused the post")

// Failure is a platform error with the message a person reads; Kind (ErrReauth, ErrRejected or nil for a
// retryable one) is what errors.Is matches.
type Failure struct {
	Kind error
	Msg  string
}

func (f *Failure) Error() string { return f.Msg }
func (f *Failure) Unwrap() error { return f.Kind }

// ErrAsync: the provider does not publish itself; the result comes back later
// (custom: POST /posts/{id}/targets/{channel}/result).
var ErrAsync = errors.New("published asynchronously")

// Provider is one platform.
type Provider interface {
	ID() string
	Name() string
	Auth() AuthKind
	// DocsURL points at the docs page that says what to register and which scopes to grant.
	DocsURL() string
	// OAuth2
	AuthURL(app App, redirect, state string, extra map[string]string) (string, error)
	Exchange(ctx context.Context, app App, redirect, code string, extra map[string]string) (Credentials, Profile, error)
	Refresh(ctx context.Context, app App, c Credentials) (Credentials, error)
	// Token: the fields a person pastes, then a check against the platform.
	CredentialFields() []Field
	Connect(ctx context.Context, fields map[string]string) (Credentials, Profile, error)
	// Posting
	Limits() Limits
	SettingsFields() []Field
	Validate(p PostContent, s Settings) error
	Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error)
}

// Entry is a provider as the catalog lists it.
type Entry struct {
	Provider
	Status string
}

var registry = map[string]Entry{}

// Register adds a provider to the catalog (call from init()).
func Register(p Provider, status string) {
	registry[p.ID()] = Entry{Provider: p, Status: status}
}

// Unregister removes a provider (tests that register a fake one).
func Unregister(id string) { delete(registry, id) }

// Get returns a registered provider.
func Get(id string) (Entry, bool) {
	e, ok := registry[id]
	return e, ok
}

// All lists the catalog in display order: available first, then by name.
func All() []Entry {
	out := make([]Entry, 0, len(registry))
	for _, e := range registry {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].ID() == "custom") != (out[j].ID() == "custom") {
			return out[i].ID() == "custom" // your own automation leads the grid
		}
		if out[i].Status != out[j].Status {
			return out[i].Status == StatusAvailable
		}
		return out[i].Name() < out[j].Name()
	})
	return out
}

// ValidationError names the field (or "content") that is wrong.
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Msg }

func invalid(field, msg string) error { return &ValidationError{Field: field, Msg: msg} }

// CheckLimits is the shared validation every provider starts with.
func CheckLimits(l Limits, p PostContent, s Settings) error {
	if n := TextLength(l, p.Text); l.MaxChars > 0 && n > l.MaxChars {
		return invalid("content", fmt.Sprintf("at most %d characters for this channel (%d)", l.MaxChars, n))
	}
	if len(p.Media) > l.MaxMedia {
		if l.MaxMedia == 0 {
			return invalid("media", "this channel takes no media")
		}
		return invalid("media", fmt.Sprintf("at most %d files for this channel", l.MaxMedia))
	}
	if l.NeedsMedia && len(p.Media) == 0 {
		return invalid("media", "this channel needs at least one image or video")
	}
	if l.NeedsTitle && s["title"] == "" {
		return invalid("title", "this channel needs a title")
	}
	if l.NeedsLink && s["link"] == "" {
		return invalid("link", "this channel needs a link")
	}
	if strings.TrimSpace(p.Text) == "" && len(p.Media) == 0 && s["title"] == "" {
		return invalid("content", "write something or add media")
	}
	return nil
}

// Base implements the parts most providers share; embed it and override what differs.
type Base struct {
	id, name, docs string
	auth           AuthKind
	limits         Limits
	settings       []Field
	credentials    []Field
}

func NewBase(id, name string, auth AuthKind, limits Limits) Base {
	return Base{id: id, name: name, auth: auth, limits: limits, docs: "https://docs.staqly.ai/channels/" + id}
}

func (b Base) ID() string      { return b.id }
func (b Base) Name() string    { return b.name }
func (b Base) Auth() AuthKind  { return b.auth }
func (b Base) DocsURL() string { return b.docs }
func (b Base) Limits() Limits {
	l := b.limits
	if l.Links == "" {
		l.Links = LinksKeep
	}
	if len(l.LinkModes) == 0 {
		l.LinkModes = []string{LinksKeep, LinksRemove, LinksDefang}
	}
	if l.Image == nil {
		l.Image = imageRules[b.id]
	}
	return l
}
func (b Base) SettingsFields() []Field   { return b.settings }
func (b Base) CredentialFields() []Field { return b.credentials }
func (b Base) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(b.limits, p, s); err != nil {
		return err
	}
	return CheckFields(b.settings, s)
}

// WithSettings gives a provider its per-post settings (the composer renders them, Validate checks them).
func (b Base) WithSettings(fields ...Field) Base {
	b.settings = fields
	return b
}

var errNotSupported = errors.New("not supported by this provider")

func (b Base) AuthURL(App, string, string, map[string]string) (string, error) {
	return "", errNotSupported
}
func (b Base) Exchange(context.Context, App, string, string, map[string]string) (Credentials, Profile, error) {
	return nil, Profile{}, errNotSupported
}
func (b Base) Refresh(_ context.Context, _ App, c Credentials) (Credentials, error) { return c, nil }
func (b Base) Connect(context.Context, map[string]string) (Credentials, Profile, error) {
	return nil, Profile{}, errNotSupported
}
func (b Base) Publish(context.Context, Credentials, PostContent, Settings) (Result, error) {
	return Result{}, errNotSupported
}
