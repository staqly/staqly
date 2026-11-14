package cli

import "encoding/json"

// Response shapes from apps/api/openapi.yaml. Only fields the CLI renders
// are declared; --json prints the server response verbatim.

type Post struct {
	ID             string         `json:"id"`
	CollectionID   string         `json:"collection_id"`
	CollectionName string         `json:"collection_name"`
	Title          string         `json:"title"`
	Content        string         `json:"content"`
	Media          []string       `json:"media"`
	MediaItems     []Media        `json:"media_items"`
	Status         string         `json:"status"`
	PublishAt      *string        `json:"publish_at"`
	PublishedAt    *string        `json:"published_at"`
	UnpublishedAt  *string        `json:"unpublished_at"`
	UnpublishedBy  string         `json:"unpublished_by"`
	Tags           []string       `json:"tags"`
	Metadata       map[string]any `json:"metadata"`
	Source         string         `json:"source"`
	ExternalRef    string         `json:"external_ref"`
	RepeatEvery    string         `json:"repeat_every"`
	Thread         []ThreadPart   `json:"thread"`
	Version        int            `json:"version"`
	Targets        []PostTarget   `json:"targets"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
}

type PostTarget struct {
	ChannelID    string            `json:"channel_id"`
	ChannelName  string            `json:"channel_name"`
	Provider     string            `json:"provider"`
	Content      *string           `json:"content"`
	Settings     map[string]string `json:"settings"`
	Status       string            `json:"status"`
	ExternalID   string            `json:"external_id"`
	ExternalURL  string            `json:"external_url"`
	Error        string            `json:"error"`
	Attempts     int               `json:"attempts"`
	PublishedAt  *string           `json:"published_at"`
	Removal      string            `json:"removal"`
	RemovalError string            `json:"removal_error"`
	RemovedAt    *string           `json:"removed_at"`
	Replies      []Reply           `json:"replies"`
}

// ThreadPart is one reply under a post.
type ThreadPart struct {
	Content      string   `json:"content"`
	Media        []string `json:"media"`
	DelaySeconds int64    `json:"delay_seconds"`
}

// Reply is how one thread part went on one channel.
type Reply struct {
	Index       int     `json:"index"`
	Status      string  `json:"status"`
	ExternalID  string  `json:"external_id"`
	ExternalURL string  `json:"external_url"`
	Error       string  `json:"error"`
	PublishedAt *string `json:"published_at"`
}

type PostPage struct {
	Data       []Post `json:"data"`
	NextCursor string `json:"next_cursor"`
}

type PostPreview struct {
	Targets []struct {
		ChannelID  string   `json:"channel_id"`
		Text       string   `json:"text"`
		Links      string   `json:"links"`
		LinksFound []string `json:"links_found"`
		Warnings   []string `json:"warnings"`
		OK         bool     `json:"ok"`
	} `json:"targets"`
}

type Tag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type Collection struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// ProcessingTimeoutSeconds: how long a target on your own automation may wait for a result; nil = the server default.
	ProcessingTimeoutSeconds *int64         `json:"processing_timeout_seconds"`
	IsDefault                bool           `json:"is_default"`
	Archived                 bool           `json:"archived"`
	PostCount                int            `json:"post_count"`
	StatusCounts             map[string]int `json:"status_counts"`
	CreatedAt                string         `json:"created_at"`
	UpdatedAt                string         `json:"updated_at"`
}

type Channel struct {
	ID         string         `json:"id"`
	Provider   string         `json:"provider"`
	Name       string         `json:"name"`
	Handle     string         `json:"handle"`
	AvatarURL  string         `json:"avatar_url"`
	ProfileURL string         `json:"profile_url"`
	Status     string         `json:"status"`
	Settings   map[string]any `json:"settings"`
	Links      string         `json:"links"`
	GroupID    string         `json:"group_id"`
	LastError  string         `json:"last_error"`
	CreatedAt  string         `json:"created_at"`
	UpdatedAt  string         `json:"updated_at"`
	Activity   *struct {
		LastPublishedAt *string `json:"last_published_at"`
		LastPostID      *string `json:"last_post_id"`
		NextPublishAt   *string `json:"next_publish_at"`
		Published30d    int     `json:"published_30d"`
	} `json:"activity"`
}

// lastAndNext: the channel's last published and next scheduled post times ("-" when none).
func (ch Channel) lastAndNext() (string, string) {
	if ch.Activity == nil {
		return "-", "-"
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	return fmtTime(str(ch.Activity.LastPublishedAt)), fmtTime(str(ch.Activity.NextPublishAt))
}

// ChannelGroup: the one group a channel is in; the Default group always exists.
type ChannelGroup struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Position int    `json:"position"`
	Default  bool   `json:"default"`
	Channels int    `json:"channels"`
}

type ProviderField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Help     string `json:"help"`
	Kind     string `json:"kind"`
	Required bool   `json:"required"`
	Default  string `json:"default"`
	// When: the field applies only while another field holds this value (as sent).
	When *struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"when"`
	Options []struct {
		Value string `json:"value"`
		Label string `json:"label"`
	} `json:"options"`
}

type ProviderLimits struct {
	MaxChars   int      `json:"max_chars"`
	MaxMedia   int      `json:"max_media"`
	MediaKinds []string `json:"media_kinds"`
	NeedsTitle bool     `json:"needs_title"`
	NeedsMedia bool     `json:"needs_media"`
	NeedsLink  bool     `json:"needs_link"`
}

type Provider struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Auth             string          `json:"auth"`
	Status           string          `json:"status"`
	Limits           ProviderLimits  `json:"limits"`
	CredentialFields []ProviderField `json:"credential_fields"`
	SettingsFields   []ProviderField `json:"settings_fields"`
	AppConfigured    bool            `json:"app_configured"`
	DocsURL          string          `json:"docs_url"`
}

type ScheduleRule struct {
	Times        []string `json:"times,omitempty"`
	Weekdays     []int    `json:"weekdays,omitempty"`
	TZ           string   `json:"tz,omitempty"`
	EveryMinutes int      `json:"every_minutes,omitempty"`
}

type Schedule struct {
	ID             string       `json:"id"`
	CollectionID   string       `json:"collection_id"`
	CollectionName string       `json:"collection_name"`
	Name           string       `json:"name"`
	Channels       []string     `json:"channels"`
	ChannelNames   []string     `json:"channel_names"`
	Kind           string       `json:"kind"`
	RunAt          *string      `json:"run_at"`
	Rule           ScheduleRule `json:"rule"`
	Pick           string       `json:"pick"`
	PostIDs        []string     `json:"post_ids"`
	Source         string       `json:"source"`
	EndpointURL    string       `json:"endpoint_url"`
	EndpointSecret string       `json:"endpoint_secret,omitempty"` // in full only in the response that set it
	Enabled        bool         `json:"enabled"`
	NextRunAt      *string      `json:"next_run_at"`
	LastRunAt      *string      `json:"last_run_at"`
	LastResult     string       `json:"last_result"`
	CreatedAt      string       `json:"created_at"`
}

type Job struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Key         string `json:"key"`
	Status      string `json:"status"`
	RunAt       string `json:"run_at"`
	Attempts    int    `json:"attempts"`
	MaxAttempts int    `json:"max_attempts"`
	LeasedBy    string `json:"leased_by"`
	LastError   string `json:"last_error"`
	Payload     struct {
		PostID     string `json:"post_id"`
		ChannelID  string `json:"channel_id"`
		ScheduleID string `json:"schedule_id"`
	} `json:"payload"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	DoneAt    *string `json:"done_at"`
}

type JobPage struct {
	Data       []Job  `json:"data"`
	NextCursor string `json:"next_cursor"`
}

type JobStats struct {
	Queued  int `json:"queued"`
	Running int `json:"running"`
	Failed  int `json:"failed"`
	Done24h int `json:"done_24h"`
	ByKind  map[string]struct {
		Queued, Running, Failed int
	} `json:"by_kind"`
}

type WorkItem struct {
	Kind        string      `json:"kind"`
	Post        *Post       `json:"post"`
	Channel     *Channel    `json:"channel"`
	Schedule    *Schedule   `json:"schedule"`
	Collection  *Collection `json:"collection"`
	RequestedAt string      `json:"requested_at"`
}

type Actor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Event struct {
	ID           string          `json:"id"`
	Seq          int64           `json:"seq"`
	Type         string          `json:"type"`
	CollectionID *string         `json:"collection_id"`
	PostID       *string         `json:"post_id"`
	ChannelID    *string         `json:"channel_id"`
	ScheduleID   *string         `json:"schedule_id"`
	Actor        Actor           `json:"actor"`
	Data         json.RawMessage `json:"data"`
	CreatedAt    string          `json:"created_at"`
}

type EventPage struct {
	Data       []Event `json:"data"`
	NextCursor string  `json:"next_cursor"`
}

type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Plan string `json:"plan"`
}

type Plan struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Limits Limits `json:"limits"`
}

// Limits are a plan's numeric limits (-1 = unlimited). Non-numeric entries,
// such as the data_export flag, are skipped instead of failing the decode:
// a new plan feature must not break the CLI.
type Limits map[string]int64

func (l *Limits) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*l = Limits{}
	for k, v := range raw {
		var n int64
		if json.Unmarshal(v, &n) == nil {
			(*l)[k] = n
		}
	}
	return nil
}

type Usage struct {
	Plan   Plan             `json:"plan"`
	Period string           `json:"period"`
	Usage  map[string]int64 `json:"usage"`
}

type SessionInfo struct {
	Mode      string    `json:"mode"`
	Workspace Workspace `json:"workspace"`
	Principal struct {
		Type   string   `json:"type"`
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	} `json:"principal"`
}

type APIKey struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	Scopes     []string `json:"scopes"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt *string  `json:"last_used_at"`
	ExpiresAt  *string  `json:"expires_at"`
	RevokedAt  *string  `json:"revoked_at"`
	Key        string   `json:"key"`
}

type Webhook struct {
	ID            string   `json:"id"`
	URL           string   `json:"url"`
	Description   string   `json:"description"`
	Secret        string   `json:"secret"`
	Events        []string `json:"events"`
	CollectionIDs []string `json:"collection_ids"`
	Active        bool     `json:"active"`
	TimeoutMS     int      `json:"timeout_ms"`
	MaxAttempts   int      `json:"max_attempts"`
	CreatedAt     string   `json:"created_at"`
}

type Delivery struct {
	ID             string  `json:"id"`
	WebhookID      string  `json:"webhook_id"`
	EventType      string  `json:"event_type"`
	Status         string  `json:"status"`
	Attempts       int     `json:"attempts"`
	LastStatusCode *int    `json:"last_status_code"`
	LastError      string  `json:"last_error"`
	CreatedAt      string  `json:"created_at"`
	DeliveredAt    *string `json:"delivered_at"`
}

type ResponseInsight struct {
	Positive  int64    `json:"positive"`
	Neutral   int64    `json:"neutral"`
	Negative  int64    `json:"negative"`
	Summary   string   `json:"summary"`
	Themes    []string `json:"themes"`
	Questions []string `json:"questions"`
	Status    string   `json:"status"`
	Error     string   `json:"error"`
}

type PostResponse struct {
	Author       string `json:"author"`
	AuthorHandle string `json:"author_handle"`
	Text         string `json:"text"`
	Sentiment    string `json:"sentiment"`
	Likes        *int64 `json:"likes"`
	CreatedAt    string `json:"created_at"`
}

type ChannelResponses struct {
	ChannelID   string           `json:"channel_id"`
	ChannelName string           `json:"channel_name"`
	Available   bool             `json:"available"`
	Error       string           `json:"error"`
	Insight     *ResponseInsight `json:"insight"`
	Responses   []PostResponse   `json:"responses"`
}

type PostResponses struct {
	Overall  *ResponseInsight   `json:"overall"`
	Channels []ChannelResponses `json:"channels"`
}
