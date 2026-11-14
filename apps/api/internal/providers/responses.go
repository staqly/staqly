package providers

import (
	"context"
	"time"
)

// Response is one comment or reply someone left under a published post (or under another response).
type Response struct {
	ID           string
	ParentID     string // the response it answers; empty for a direct comment on the post
	Author       string
	AuthorHandle string
	Text         string
	URL          string
	CreatedAt    time.Time
	Likes        *int64 // nil when the platform doesn't say
	Own          bool   // written by the connected account itself
}

// ResponseReader is implemented by platforms whose comments and replies under a published post staqly can read.
// externalID is what Publish returned. It returns at most 500 responses created after since (zero time = all), in
// any order; ErrReauth when the sign-in no longer works, ErrRejected (with the reason) when the platform refuses.
type ResponseReader interface {
	Responses(ctx context.Context, c Credentials, externalID string, since time.Time) ([]Response, error)
}
