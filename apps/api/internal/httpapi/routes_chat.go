package httpapi

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// Public, self-authenticating endpoints for notifications:
//
//	GET/POST /v1/decide/{token}                    one-tap decision links
//	POST     /v1/integrations/telegram/{channel}   Telegram webhook (secret_token)
//	POST     /v1/integrations/slack/interactions   Slack (X-Slack-Signature)
//	POST     /v1/integrations/discord/interactions Discord (Ed25519)
//
// They are served from a middleware rather than the /v1 router because the
// router only admits JSON/multipart bodies, while HTML forms and Slack send
// application/x-www-form-urlencoded.
// ponytail: a shared-file change could mount these as public routes and exempt them from AllowContentType.

func init() {
	RegisterMiddleware(func(s *Server) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p := r.URL.Path
				switch {
				case strings.HasPrefix(p, "/v1/decide/") && !strings.Contains(p[len("/v1/decide/"):], "/"):
					tok := p[len("/v1/decide/"):]
					// The token is a credential: keep it out of request logs.
					r.URL.Path, r.URL.RawPath = "/v1/decide/{token}", ""
					s.decide(w, r, tok)
				case strings.HasPrefix(p, "/v1/integrations/telegram/") && r.Method == http.MethodPost:
					s.telegramWebhook(w, r, p[len("/v1/integrations/telegram/"):])
				case p == "/v1/integrations/slack/interactions" && r.Method == http.MethodPost:
					s.slackInteractions(w, r)
				case p == "/v1/integrations/discord/interactions" && r.Method == http.MethodPost:
					s.discordInteractions(w, r)
				default:
					next.ServeHTTP(w, r)
				}
			})
		}
	})
}

func readBody(w http.ResponseWriter, r *http.Request, max int64) ([]byte, bool) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	if err != nil {
		writeErr(w, &app.Error{Code: "too_large", Status: 413, Message: "request body too large"})
		return nil, false
	}
	return b, true
}

func (s *Server) telegramWebhook(w http.ResponseWriter, r *http.Request, channelID string) {
	body, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	err := s.svc.HandleTelegramWebhook(r.Context(), channelID, r.Header.Get("X-Telegram-Bot-Api-Secret-Token"), body)
	respond(w, 200, map[string]bool{"ok": true}, err)
}

func (s *Server) slackInteractions(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	err := s.svc.HandleSlackInteraction(r.Context(), body, r.Header.Get("X-Slack-Request-Timestamp"), r.Header.Get("X-Slack-Signature"))
	if err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(200)
}

func (s *Server) discordInteractions(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	out, err := s.svc.HandleDiscordInteraction(r.Context(), body, r.Header.Get("X-Signature-Ed25519"), r.Header.Get("X-Signature-Timestamp"))
	respond(w, 200, out, err)
}

// ---- one-tap decision pages ----

type decidePage struct {
	Heading, Message string
	View             *app.DecisionLinkView
	Form             bool
	Reject           bool
	Done             string // "approved" / "rejected"
}

var decideTmpl = template.Must(template.New("decide").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex,nofollow"><title>{{.Heading}} · staq.ly</title>
<style>
body{font-family:system-ui,-apple-system,Segoe UI,sans-serif;background:#f8fafc;color:#0f172a;margin:0;padding:24px}
main{max-width:560px;margin:0 auto;background:#fff;border:1px solid #e2e8f0;border-radius:12px;padding:24px}
h1{font-size:20px;margin:0 0 12px}.meta{font-size:13px;color:#64748b}.title{font-size:17px;font-weight:600;margin:6px 0}
.preview{white-space:pre-wrap;color:#334155;background:#f1f5f9;border-radius:8px;padding:12px;margin:12px 0;max-height:280px;overflow:auto}
textarea{width:100%;box-sizing:border-box;min-height:90px;border:1px solid #cbd5e1;border-radius:8px;padding:8px;font:inherit}
button{font:inherit;font-weight:600;border:0;border-radius:8px;padding:10px 20px;color:#fff;cursor:pointer;margin-top:12px}
.approve{background:#16a34a}.reject{background:#dc2626}.done{font-size:18px}a{color:#4f46e5}
</style></head><body><main>
<h1>{{.Heading}}</h1>
{{with .View}}<div class="meta">{{.Collection}}{{if .ItemType}} · {{.ItemType}}{{end}}</div>
<div class="title">{{.Title}}</div>{{if .Preview}}<div class="preview">{{.Preview}}</div>{{end}}{{end}}
{{if .Message}}<p class="done">{{.Message}}</p>{{end}}
{{if .Form}}<form method="post">
{{if .Reject}}<label for="feedback">Reason (optional)</label><textarea id="feedback" name="feedback" maxlength="5000"></textarea>
<button class="reject" type="submit">Reject</button>{{else}}<button class="approve" type="submit">Approve</button>{{end}}
</form>{{end}}
{{with .View}}<p class="meta">Link from “{{.ChannelName}}”. Single use. <a href="{{.WebURL}}" rel="noreferrer">Open in staq.ly</a></p>{{end}}
</main></body></html>`))

func (s *Server) renderDecide(w http.ResponseWriter, r *http.Request, status int, page decidePage) {
	if strings.Contains(r.Header.Get("Accept"), "application/json") && !strings.Contains(r.Header.Get("Accept"), "text/html") {
		out := map[string]any{"message": page.Message, "status": page.Done}
		if page.View != nil {
			out["item_id"], out["action"] = page.View.ItemID, page.View.Action
		}
		writeJSON(w, status, out)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	h.Set("X-Robots-Tag", "noindex")
	w.WriteHeader(status)
	_ = decideTmpl.Execute(w, page)
}

func (s *Server) decideError(w http.ResponseWriter, r *http.Request, err error) {
	var de *app.Error
	if !errors.As(err, &de) {
		s.log.Error("decide link failed", "err", err)
		s.renderDecide(w, r, 500, decidePage{Heading: "Something went wrong", Message: "Please try again or use the dashboard."})
		return
	}
	switch de.Status {
	case 404:
		s.renderDecide(w, r, 404, decidePage{Heading: "Link not found", Message: "This decision link is not valid."})
	case 410:
		s.renderDecide(w, r, 410, decidePage{Heading: "Link no longer valid", Message: de.Message})
	default:
		s.renderDecide(w, r, de.Status, decidePage{Heading: "Could not complete", Message: de.Message})
	}
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request, tok string) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		v, err := s.svc.LookupDecisionLink(ctx, tok)
		if err != nil {
			s.decideError(w, r, err)
			return
		}
		page := decidePage{View: &v, Reject: v.Action == "reject", Form: v.Status == "pending"}
		page.Heading = "Approve this item?"
		if page.Reject {
			page.Heading = "Reject this item?"
		}
		if !page.Form {
			page.Message = "This item is already " + v.Status + "."
		}
		s.renderDecide(w, r, 200, page)
	case http.MethodPost:
		feedback := ""
		ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		switch ct {
		case "application/x-www-form-urlencoded", "multipart/form-data":
			if err := r.ParseForm(); err != nil {
				s.renderDecide(w, r, 400, decidePage{Heading: "Invalid request"})
				return
			}
			feedback = r.PostForm.Get("feedback")
		case "application/json":
			var in struct {
				Feedback string `json:"feedback"`
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
				s.renderDecide(w, r, 400, decidePage{Heading: "Invalid request"})
				return
			}
			feedback = in.Feedback
		}
		v, verr := s.svc.LookupDecisionLink(ctx, tok)
		_, action, err := s.svc.UseDecisionLink(ctx, tok, feedback)
		var view *app.DecisionLinkView
		if verr == nil {
			view = &v
		}
		if err != nil {
			var de *app.Error
			if errors.As(err, &de) && de.Code == "conflict" {
				msg := "This item was already decided."
				if view != nil {
					if cur, gerr := s.svc.LookupItemStatus(ctx, tok); gerr == nil {
						msg = "This item is already " + cur + "."
					}
				}
				s.renderDecide(w, r, 409, decidePage{Heading: "Already decided", Message: msg, View: view})
				return
			}
			s.decideError(w, r, err)
			return
		}
		done := map[string]string{"approve": "approved", "reject": "rejected"}[action]
		s.renderDecide(w, r, 200, decidePage{Heading: map[string]string{"approve": "Approved ✓", "reject": "Rejected"}[action],
			Message: "Done. The item was " + done + ".", View: view, Done: done})
	default:
		w.Header().Set("Allow", "GET, POST")
		writeErr(w, &app.Error{Code: "method_not_allowed", Status: 405, Message: "method not allowed"})
	}
}
