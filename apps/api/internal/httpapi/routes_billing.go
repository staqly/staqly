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
//	GET  /v1/billing/details   subscription period, cards on file and invoices, read from Stripe (admin)
//	POST /v1/billing/checkout  Stripe Checkout URL for a plan and interval, or a switch of the live subscription (admin)
//	POST /v1/billing/portal    Stripe customer portal URL (admin)
//	POST /v1/billing/sync      read the subscription from Stripe now, then the status (admin; back from Checkout)
//	GET  /v1/billing/x-posts   posts to X this period: included, used, bought credits left (posts:read)
//	POST /v1/billing/credits/checkout  Stripe Checkout URL for a bundle of extra X posts (account owner)
//	GET  /v1/billing/addons    add-ons on sale and which the workspace has (admin)
//	POST /v1/billing/addons/{id}    add an add-on to the live subscription (account owner)
//	DELETE /v1/billing/addons/{id}  remove it (account owner)
//	POST /v1/billing/webhook   Stripe events (Stripe-Signature, no API key)
func init() {
	RegisterAuthedRoutes(func(s *Server, r chi.Router) {
		r.Get("/billing", s.getBilling)
		r.Get("/billing/details", s.getBillingDetails)
		r.Post("/billing/checkout", s.billingCheckout)
		r.Post("/billing/portal", s.billingURL(s.svc.PortalSession))
		r.Post("/billing/sync", s.syncBilling)
		r.Get("/billing/x-posts", s.getXPosts)
		r.Post("/billing/credits/checkout", s.creditsCheckout)
		r.Get("/billing/addons", s.listAddons)
		r.Post("/billing/addons/{id}", s.addAddon)
		r.Delete("/billing/addons/{id}", s.removeAddon)
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

func (s *Server) syncBilling(w http.ResponseWriter, r *http.Request) {
	b, err := s.svc.SyncBilling(r.Context(), principal(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, b)
}

func (s *Server) getBillingDetails(w http.ResponseWriter, r *http.Request) {
	d, err := s.svc.GetBillingDetails(r.Context(), principal(r), r.URL.Query().Get("starting_after"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, d)
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

// billingCheckout: {"plan": "plus", "interval": "year"}; both optional (Pro, monthly), an empty body too.
func (s *Server) billingCheckout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Plan          string `json:"plan"`
		Interval      string `json:"interval"`
		ReturnTo      string `json:"return_to"`
		PromotionCode string `json:"promotion_code"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	u, err := s.svc.CheckoutSession(r.Context(), principal(r), in.Plan, in.Interval,
		app.CheckoutOptions{ReturnTo: in.ReturnTo, PromotionCode: in.PromotionCode})
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]string{"url": u})
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

func (s *Server) getXPosts(w http.ResponseWriter, r *http.Request) {
	x, err := s.svc.GetXPosts(r.Context(), principal(r))
	respond(w, r, 200, x, err)
}

// creditsCheckout: {"provider": "x", "posts": 1000}; provider defaults to x.
func (s *Server) creditsCheckout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Provider string `json:"provider"`
		Posts    int64  `json:"posts"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, err := s.svc.CreditsCheckout(r.Context(), principal(r), in.Provider, in.Posts)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]string{"url": u})
}

func (s *Server) listAddons(w http.ResponseWriter, r *http.Request) {
	a, err := s.svc.ListAddons(r.Context(), principal(r))
	respond(w, r, 200, map[string]any{"data": a}, err)
}

func (s *Server) addAddon(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.AddAddon(r.Context(), principal(r), chi.URLParam(r, "id")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeAddon(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.RemoveAddon(r.Context(), principal(r), chi.URLParam(r, "id")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
