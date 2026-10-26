package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
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
	assistantMaxAttachments = 10
	// ponytail: pictures go whole, as data URLs, with every turn of the chat; resize them first if large ones
	// make turns slow or costly.
	assistantMaxImageBytes = 5 << 20
)

// withAttachments gives each message with attachments its parts for the model; a file that can't be read is
// named as missing, so the model says so instead of guessing.
func (s *Service) withAttachments(ctx context.Context, p Principal, msgs []AssistantMessage) []AssistantMessage {
	out := make([]AssistantMessage, len(msgs))
	copy(out, msgs)
	for i, m := range out {
		if m.Role != "user" || len(m.Attachments) == 0 {
			continue
		}
		var notes []string
		var files []map[string]any
		for _, a := range m.Attachments[:min(len(m.Attachments), assistantMaxAttachments)] {
			part, note := s.attachmentPart(ctx, p, a)
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

// attachmentPart reads one file: its part for the model (nil when it goes by name only) and how the message
// names it.
func (s *Service) attachmentPart(ctx context.Context, p Principal, a AssistantAttachment) (map[string]any, string) {
	f, m, err := s.OpenMedia(ctx, p, a.MediaID)
	if err != nil {
		return nil, fmt.Sprintf("%s (could not be opened)", firstNonEmpty(a.Name, a.MediaID))
	}
	defer f.Close()
	note := fmt.Sprintf("%s, %s, media id %s", m.OriginalName, m.MimeType, m.ID)
	switch {
	case strings.HasPrefix(m.MimeType, "image/") && m.MimeType != "image/svg+xml":
		if m.SizeBytes > assistantMaxImageBytes {
			return nil, note + " (too large to show)"
		}
		b, err := io.ReadAll(io.LimitReader(f, assistantMaxImageBytes+1))
		if err != nil {
			return nil, note + " (could not be read)"
		}
		url := "data:" + m.MimeType + ";base64," + base64.StdEncoding.EncodeToString(b)
		return map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}, note
	}
	return nil, note
}
