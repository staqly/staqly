package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// The assistant's conversations, kept on the server for the person who had them (in this workspace): the
// dashboard sends a chat id with each turn, the server saves the conversation after it, and the history lists
// them newest first, a page at a time, searched by title.

// AssistantChat is one conversation; the list leaves out its messages.
type AssistantChat struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	CreatedAt string             `json:"created_at"`
	UpdatedAt string             `json:"updated_at"`
	Messages  []AssistantMessage `json:"messages,omitempty"`
	Pending   *AssistantToolCall `json:"pending,omitempty"`
}

// AssistantChatPage is one page of the history; NextCursor fetches the older ones.
type AssistantChatPage struct {
	Data       []AssistantChat `json:"data"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

const assistantChatKeep = 200 // messages kept per chat, the most recent

// The dashboard names a new chat itself (so its first turn can carry the id); ach_ and a few letters or digits.
var assistantChatID = regexp.MustCompile(`^ach_[a-z0-9]{8,32}$`)

// ValidAssistantChatID: whether id may name a chat.
func ValidAssistantChatID(id string) bool { return assistantChatID.MatchString(id) }

// SaveAssistantChat keeps the conversation after a turn. A chat id another person uses saves nothing.
func (s *Service) SaveAssistantChat(ctx context.Context, p Principal, id string, msgs []AssistantMessage, pending *AssistantToolCall) error {
	if !ValidAssistantChatID(id) || len(msgs) == 0 {
		return nil
	}
	if len(msgs) > assistantChatKeep {
		msgs = msgs[len(msgs)-assistantChatKeep:]
	}
	title := "New chat"
	for _, m := range msgs {
		if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
			title = truncateRunes(strings.Join(strings.Fields(m.Content), " "), 80)
			break
		}
	}
	pend := ""
	if pending != nil {
		pend = mustJSON(pending)
	}
	now := s.now()
	return s.W.SaveAssistantChat(ctx, db.SaveAssistantChatParams{ID: id, WorkspaceID: p.WorkspaceID, Owner: p.actor(),
		Title: title, Messages: mustJSON(msgs), Pending: pend, CreatedAt: now, UpdatedAt: now})
}

// ListAssistantChats: the person's chats, newest first; q finds words in their titles.
func (s *Service) ListAssistantChats(ctx context.Context, p Principal, q, cursor string, limit int) (AssistantChatPage, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	afterAt, afterID := "", ""
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		at, id, ok := strings.Cut(string(raw), "|")
		if err != nil || !ok {
			return AssistantChatPage{}, invalid("cursor", "is invalid")
		}
		afterAt, afterID = at, id
	}
	rows, err := s.Q.ListAssistantChats(ctx, db.ListAssistantChatsParams{WorkspaceID: p.WorkspaceID, Owner: p.actor(),
		AfterAt: afterAt, AfterID: afterID, Q: strings.ToLower(strings.TrimSpace(q)), Lim: int64(limit + 1)})
	if err != nil {
		return AssistantChatPage{}, err
	}
	page := AssistantChatPage{Data: []AssistantChat{}}
	for i, r := range rows {
		if i == limit {
			last := rows[i-1]
			page.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(last.UpdatedAt + "|" + last.ID))
			break
		}
		page.Data = append(page.Data, AssistantChat{ID: r.ID, Title: r.Title, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt})
	}
	return page, nil
}

// GetAssistantChat: one of the person's chats with its messages.
func (s *Service) GetAssistantChat(ctx context.Context, p Principal, id string) (AssistantChat, error) {
	r, err := s.Q.GetAssistantChat(ctx, db.GetAssistantChatParams{WorkspaceID: p.WorkspaceID, Owner: p.actor(), ID: id})
	if isNoRows(err) {
		return AssistantChat{}, notFound("chat")
	}
	if err != nil {
		return AssistantChat{}, err
	}
	out := AssistantChat{ID: r.ID, Title: r.Title, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Messages: []AssistantMessage{}}
	_ = json.Unmarshal([]byte(r.Messages), &out.Messages)
	if r.Pending != "" {
		var c AssistantToolCall
		if json.Unmarshal([]byte(r.Pending), &c) == nil {
			out.Pending = &c
		}
	}
	return out, nil
}

// DeleteAssistantChat removes one of the person's chats.
func (s *Service) DeleteAssistantChat(ctx context.Context, p Principal, id string) error {
	n, err := s.W.DeleteAssistantChat(ctx, db.DeleteAssistantChatParams{WorkspaceID: p.WorkspaceID, Owner: p.actor(), ID: id})
	if err == nil && n == 0 {
		return notFound("chat")
	}
	return err
}
