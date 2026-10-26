package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/staqly/staqly/apps/api/internal/app"
)

func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Post("/assistant/chat", s.assistantChat)
		r.Post("/ai/text", s.aiText)
		r.Get("/assistant/chats", s.listAssistantChats)
		r.Get("/assistant/chats/{id}", s.getAssistantChat)
		r.Delete("/assistant/chats/{id}", s.deleteAssistantChat)
	})
}

// aiText: POST /v1/ai/text, a text rewritten by the assistant's model (the design editor's text tools).
func (s *Server) aiText(w http.ResponseWriter, r *http.Request) {
	var in app.AITextRequest
	if !decode(w, r, &in) {
		return
	}
	out, err := s.svc.AIText(r.Context(), principal(r), in)
	respond(w, r, 200, out, err)
}

func (s *Server) listAssistantChats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	out, err := s.svc.ListAssistantChats(r.Context(), principal(r), q.Get("q"), q.Get("cursor"), limit)
	respond(w, r, 200, out, err)
}

func (s *Server) getAssistantChat(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetAssistantChat(r.Context(), principal(r), chi.URLParam(r, "id"))
	respond(w, r, 200, out, err)
}

func (s *Server) deleteAssistantChat(w http.ResponseWriter, r *http.Request) {
	respondEmpty(w, r, s.svc.DeleteAssistantChat(r.Context(), principal(r), chi.URLParam(r, "id")))
}

// assistantChat: POST /v1/assistant/chat, one turn of the dashboard's assistant, answered as server-sent events
// (data: one app.AssistantEvent per line).
func (s *Server) assistantChat(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AssistantTools == nil {
		writeErr(w, r, &app.Error{Code: "not_available", Status: 409, Message: "the assistant is not available on this server"})
		return
	}
	var req app.AssistantRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	if err := dec.Decode(&req); err != nil {
		writeErr(w, r, &app.Error{Code: "bad_request", Status: 400, Message: "the body must be the conversation as JSON"})
		return
	}
	if req.ChatID != "" && !app.ValidAssistantChatID(req.ChatID) {
		writeErr(w, r, &app.Error{Code: "validation_failed", Status: 422, Message: "chat_id must be ach_ and 8 to 32 lowercase letters or digits"})
		return
	}
	if err := s.svc.CheckAssistant(req); err != nil {
		writeErr(w, r, err)
		return
	}
	p := principal(r)
	tools, done, err := s.cfg.AssistantTools(r.Context(), p)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer done()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	// The conversation as the turn leaves it, saved in the chat once it ends (also when the browser went away).
	msgs, pending := req.Messages, (*app.AssistantToolCall)(nil)
	defer func() {
		if err := s.svc.SaveAssistantChat(context.WithoutCancel(r.Context()), p, req.ChatID, msgs, pending); err != nil {
			s.log.Warn("assistant chat not saved", "err", err)
		}
	}()
	emit := func(e app.AssistantEvent) {
		switch {
		case e.Type == "message" && e.Message != nil:
			msgs = append(msgs, *e.Message)
		case e.Type == "confirm":
			pending = e.ToolCall
		}
		raw, _ := json.Marshal(e)
		fmt.Fprintf(w, "data: %s\n\n", raw)
		_ = rc.Flush()
	}
	if err := s.svc.Assistant(r.Context(), p, req, tools, emit); err != nil {
		var ae *app.Error
		if !errors.As(err, &ae) {
			s.log.Error("assistant", "err", err)
			ae = &app.Error{Code: "internal", Status: 500, Message: "Something went wrong; try again."}
		}
		emit(app.AssistantEvent{Type: "error", Error: ae})
	}
}
