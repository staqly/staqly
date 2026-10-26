package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/staqly/staqly/apps/api/internal/app"
)

func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Post("/assistant/chat", s.assistantChat)
	})
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
	emit := func(e app.AssistantEvent) {
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
