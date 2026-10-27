package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/staqly/staqly/apps/api/internal/app"
)

// The workspace's analytics: publishing outcomes, automation sources, posting times and cached platform numbers.

func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Get("/analytics", s.getAnalytics)
	})
}

func (s *Server) getAnalytics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	days := 0
	if v := q.Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			n = -1 // refused by the service like any number out of range
		}
		days = n
	}
	out, err := s.svc.Analytics(r.Context(), principal(r), app.AnalyticsQuery{From: q.Get("from"), To: q.Get("to"), Days: days,
		Channel: q.Get("channel"), Collection: q.Get("collection"), Source: q.Get("source")})
	respond(w, r, 200, out, err)
}
