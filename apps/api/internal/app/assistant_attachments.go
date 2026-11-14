package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// What the person adds to a message for the assistant: files of the media library (uploaded from the chat, or
// already there), sent to the model with the message. Pictures go as pictures (models that see read them); videos
// and sounds are named with their media id, which the tools take (a post's media). Text files never get here: the
// dashboard puts their text in the message.

// AssistantAttachment names one file of the media library.
type AssistantAttachment struct {
	MediaID  string `json:"media_id"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

const (
	assistantMaxAttachments        = 10  // of one message
	assistantMaxRequestAttachments = 200 // of one request's conversation
	// ponytail: pictures go whole, as data URLs, with every turn of the chat; resize them first if large ones
	// make turns slow or costly.
	assistantMaxImageBytes = 5 << 20
	// The pictures of one turn, the most recent first: what the server holds in memory (and sends) for a request.
	assistantMaxTurnImageBytes = 20 << 20
)

// withAttachments gives each message with attachments its parts for the model; a file that can't be read is
// named as missing, so the model says so instead of guessing. A picture is read and sent once, with the latest
// message that has it, and pictures over the turn's budget go by name.
func (s *Service) withAttachments(ctx context.Context, p Principal, msgs []AssistantMessage) []AssistantMessage {
	out := slices.Clone(msgs)
	shown := map[string]bool{}
	budget := int64(assistantMaxTurnImageBytes)
	for i := len(out) - 1; i >= 0; i-- {
		m := out[i]
		if m.Role != "user" || len(m.Attachments) == 0 {
			continue
		}
		var notes []string
		var files []map[string]any
		for _, a := range m.Attachments[:min(len(m.Attachments), assistantMaxAttachments)] {
			part, note := s.attachmentPart(ctx, p, a, shown, &budget)
			notes = append(notes, note)
			if part != nil {
				files = append(files, part)
			}
		}
		text := m.Content + "\n\n[Attached: " + strings.Join(notes, "; ") + "]"
		out[i].parts = append([]map[string]any{{"type": "text", "text": text}}, files...)
	}
	return out
}

// attachmentPart is one file: its part for the model (nil when it goes by name only) and how the message names
// it. Only a picture that is shown is read; shown and budget are the turn's (withAttachments).
func (s *Service) attachmentPart(ctx context.Context, p Principal, a AssistantAttachment, shown map[string]bool, budget *int64) (map[string]any, string) {
	m, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: a.MediaID})
	if err != nil || p.require(ScopeMediaRead) != nil {
		return nil, fmt.Sprintf("%s (could not be opened)", firstNonEmpty(a.Name, a.MediaID))
	}
	note := fmt.Sprintf("%s, %s, media id %s", m.OriginalName, m.MimeType, m.ID)
	if !strings.HasPrefix(m.MimeType, "image/") || m.MimeType == "image/svg+xml" {
		return nil, note
	}
	switch {
	case m.SizeBytes > assistantMaxImageBytes:
		return nil, note + " (too large to show)"
	case shown[m.ID]:
		return nil, note + " (shown with a later message)"
	case m.SizeBytes > *budget:
		return nil, note + " (not shown: the conversation has too many pictures)"
	}
	f, err := s.openMediaFile(ctx, m.WorkspaceID, m.StorageName)
	if err != nil {
		return nil, note + " (could not be opened)"
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, assistantMaxImageBytes+1))
	if err != nil {
		return nil, note + " (could not be read)"
	}
	shown[m.ID] = true
	*budget -= int64(len(b))
	url := "data:" + m.MimeType + ";base64," + base64.StdEncoding.EncodeToString(b)
	return map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}, note
}
