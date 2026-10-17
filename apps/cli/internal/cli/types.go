package cli

import "encoding/json"

// Response shapes from apps/api/openapi.yaml. Only fields the CLI renders
// are declared; --json prints the server response verbatim.

type Item struct {
	ID           string         `json:"id"`
	CollectionID string         `json:"collection_id"`
	Title        string         `json:"title"`
	Type         string         `json:"type"`
	Content      string         `json:"content"`
	URL          string         `json:"url"`
	Media        *MediaRef      `json:"media"`
	Metadata     map[string]any `json:"metadata"`
	Status       string         `json:"status"`
	Source       string         `json:"source"`
	ExternalRef  string         `json:"external_ref"`
	Feedback     string         `json:"feedback"`
	Version      int            `json:"version"`
	DecidedAt    *string        `json:"decided_at"`
	ExpiresAt    *string        `json:"expires_at"`
	Editable     []string       `json:"editable"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
}

type MediaRef struct {
	ID           string `json:"id"`
	MimeType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
	OriginalName string `json:"original_name"`
	URL          string `json:"url"`
}

type ItemPage struct {
	Data       []Item `json:"data"`
	NextCursor string `json:"next_cursor"`
}

// ActionDef is a built-in (approve, reject) or catalog action (regenerate, publish, retry, archive).
type ActionDef struct {
	Name             string   `json:"name"`
	Label            string   `json:"label"`
	Event            string   `json:"event"`
	RequiresFeedback bool     `json:"requires_feedback"`
	SetStatus        string   `json:"set_status"`
	AllowedStatuses  []string `json:"allowed_statuses"`
	Builtin          bool     `json:"builtin"`
	AutoRun          bool     `json:"auto_run"`
}

type Collection struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	DefaultView  string         `json:"default_view"`
	Actions      []ActionDef    `json:"actions"`
	Archived     bool           `json:"archived"`
	ItemCount    int            `json:"item_count"`
	StatusCounts map[string]int `json:"status_counts"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
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
	ItemID       *string         `json:"item_id"`
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
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Limits map[string]int64 `json:"limits"`
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

type Watcher struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Kind         string  `json:"kind"`
	URL          string  `json:"url"`
	CollectionID string  `json:"collection_id"`
	Every        string  `json:"every"`
	Status       string  `json:"status"`
	LastError    string  `json:"last_error"`
	LastRunAt    *string `json:"last_run_at"`
	NextRunAt    string  `json:"next_run_at"`
	ItemsCreated int     `json:"items_created"`
}
