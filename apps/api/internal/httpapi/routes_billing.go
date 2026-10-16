package httpapi

import (
	"context"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/staqly/staqly/apps/api/internal/app"
)

// Billing (cloud, Stripe):
//
//	GET  /v1/billing           subscription status (admin)
//	POST /v1/billing/checkout  Stripe Checkout URL for Pro (admin)
//	POST /v1/billing/portal    Stripe customer portal URL (admin)
//	POST /v1/billing/webhook   Stripe events (Stripe-Signature, no API key)
func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Get("/billing", s.getBilling)
		r.Post("/billing/checkout", s.billingURL(s.svc.CheckoutSession))
		r.Post("/billing/portal", s.billingURL(s.svc.PortalSession))
	})
	RegisterPublicRoutes(func(s *Server, r chi.Router) {
		r.Post("/billing/webhook", s.stripeWebhook)
	})
}

func (s *Server) getBilling(w http.ResponseWriter, r *http.Request) {
	b, err := s.svc.GetBilling(r.Context(), principal(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, b)
}

func (s *Server) billingURL(f func(ctx context.Context, p app.Principal) (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := f(r.Context(), principal(r))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		writeJSON(w, 200, map[string]string{"url": u})
	}
}

func (s *Server) stripeWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.svc.HandleStripeWebhook(r.Context(), body, r.Header.Get("Stripe-Signature")); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"received": true})
}
