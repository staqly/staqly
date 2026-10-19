package app

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// The one look of every email the server sends (sign-in codes, invitations, address changes, email alerts,
// personal notifications): a centred card with the logo, a title, the text, an optional quote, rows (one per
// channel), a button and a footer with the preferences and unsubscribe links. Tables and inline styles, so
// Gmail, Outlook and Apple Mail draw it alike; a light card that also reads in dark mode (no images of text).
// Every message has a plain-text twin.

type emailDoc struct {
	Preheader  string      // the line inboxes show after the subject
	Title      string      // the big heading
	Paragraphs []string    // plain text, one <p> each
	Code       string      // a sign-in code, shown large
	Quote      string      // the post's first line
	Rows       []emailRow  // one per channel (or alert entry)
	Button     *emailLink  // the main action
	Footer     []string    // why they get it
	Prefs      *emailLink  // change notification preferences
	Unsub      *emailLink  // one-click unsubscribe from this kind of email
	Links      []emailLink // secondary links under the button
}

type emailRow struct {
	Label  string // "X · @handle" or an alert's title
	Detail string // "Published 10:02" or the error
	Link   *emailLink
	Failed bool
}

type emailLink struct{ Text, URL string }

const (
	emailBrand  = "#16a34a"
	emailInk    = "#111827"
	emailMuted  = "#6b7280"
	emailBorder = "#e5e7eb"
)

// renderEmail returns the HTML and plain-text bodies. logoURL may be "" (the wordmark alone).
func renderEmail(d emailDoc, logoURL string) (string, string) {
	esc := html.EscapeString
	var h strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&h, format, a...) }

	w(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">`)
	w(`<meta name="color-scheme" content="light dark"><meta name="supported-color-schemes" content="light dark"><title>%s</title></head>`, esc(d.Title))
	w(`<body style="margin:0;padding:0;background:#f3f4f6;-webkit-text-size-adjust:100%%">`)
	if d.Preheader != "" {
		w(`<div style="display:none;max-height:0;overflow:hidden;opacity:0">%s</div>`, esc(d.Preheader))
	}
	w(`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:#f3f4f6"><tr><td align="center" style="padding:32px 12px">`)
	w(`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="max-width:560px">`)
	// Logo
	w(`<tr><td style="padding:0 4px 16px;font-family:system-ui,-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif">`)
	if logoURL != "" {
		w(`<img src="%s" width="28" height="28" alt="" style="vertical-align:middle;border:0;border-radius:6px"> `, esc(logoURL))
	}
	w(`<span style="vertical-align:middle;font-size:18px;font-weight:700;color:%s;letter-spacing:-0.02em">staqly</span></td></tr>`, emailInk)
	// Card
	w(`<tr><td style="background:#ffffff;border:1px solid %s;border-radius:16px;padding:32px 28px;font-family:system-ui,-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:%s">`, emailBorder, emailInk)
	w(`<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;font-weight:700;color:%s">%s</h1>`, emailInk, esc(d.Title))
	for _, p := range d.Paragraphs {
		w(`<p style="margin:0 0 14px;font-size:15px;line-height:1.6;color:#374151">%s</p>`, esc(p))
	}
	if d.Code != "" {
		w(`<p style="margin:8px 0 20px"><span style="display:inline-block;padding:12px 20px;border-radius:12px;background:#f0fdf4;border:1px solid #bbf7d0;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:28px;font-weight:700;letter-spacing:0.2em;color:%s">%s</span></p>`, emailInk, esc(d.Code))
	}
	if d.Quote != "" {
		w(`<p style="margin:0 0 20px;padding:12px 16px;border-left:3px solid %s;background:#f9fafb;border-radius:0 8px 8px 0;font-size:15px;line-height:1.55;color:%s">%s</p>`, emailBrand, emailInk, esc(d.Quote))
	}
	if len(d.Rows) > 0 {
		w(`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="margin:0 0 20px;border:1px solid %s;border-radius:12px;border-collapse:separate">`, emailBorder)
		for i, r := range d.Rows {
			top := ""
			if i > 0 {
				top = "border-top:1px solid " + emailBorder + ";"
			}
			dot := emailBrand
			if r.Failed {
				dot = "#dc2626"
			}
			w(`<tr><td style="%spadding:12px 16px;font-size:14px;line-height:1.5">`, top)
			w(`<span style="display:inline-block;width:8px;height:8px;border-radius:4px;background:%s;margin-right:8px;vertical-align:middle"></span>`, dot)
			w(`<strong style="color:%s">%s</strong>`, emailInk, esc(r.Label))
			if r.Detail != "" {
				color := emailMuted
				if r.Failed {
					color = "#b91c1c"
				}
				w(`<br><span style="color:%s;padding-left:16px">%s</span>`, color, esc(r.Detail))
			}
			if r.Link != nil {
				w(`<br><a href="%s" style="color:%s;padding-left:16px;text-decoration:underline">%s</a>`, esc(r.Link.URL), emailBrand, esc(r.Link.Text))
			}
			w(`</td></tr>`)
		}
		w(`</table>`)
	}
	if d.Button != nil {
		w(`<table role="presentation" cellpadding="0" cellspacing="0" style="margin:4px 0 8px"><tr><td style="border-radius:10px;background:%s">`, emailBrand)
		w(`<a href="%s" style="display:inline-block;padding:12px 22px;font-size:15px;font-weight:600;color:#ffffff;text-decoration:none;border-radius:10px">%s</a></td></tr></table>`, esc(d.Button.URL), esc(d.Button.Text))
	}
	for _, l := range d.Links {
		w(`<p style="margin:8px 0 0;font-size:13px"><a href="%s" style="color:%s">%s</a></p>`, esc(l.URL), emailBrand, esc(l.Text))
	}
	w(`</td></tr>`)
	// Footer
	w(`<tr><td style="padding:20px 8px 0;font-family:system-ui,-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;line-height:1.6;color:%s">`, emailMuted)
	for _, f := range d.Footer {
		w(`%s<br>`, esc(f))
	}
	var links []string
	if d.Prefs != nil {
		links = append(links, fmt.Sprintf(`<a href="%s" style="color:%s">%s</a>`, esc(d.Prefs.URL), emailMuted, esc(d.Prefs.Text)))
	}
	if d.Unsub != nil {
		links = append(links, fmt.Sprintf(`<a href="%s" style="color:%s">%s</a>`, esc(d.Unsub.URL), emailMuted, esc(d.Unsub.Text)))
	}
	if len(links) > 0 {
		w(`%s`, strings.Join(links, " &middot; "))
	}
	w(`</td></tr></table></td></tr></table></body></html>`)

	// Plain text
	var t strings.Builder
	t.WriteString(d.Title + "\n\n")
	for _, p := range d.Paragraphs {
		t.WriteString(p + "\n\n")
	}
	if d.Code != "" {
		t.WriteString("    " + d.Code + "\n\n")
	}
	if d.Quote != "" {
		t.WriteString("> " + d.Quote + "\n\n")
	}
	for _, r := range d.Rows {
		t.WriteString("- " + r.Label)
		if r.Detail != "" {
			t.WriteString(": " + r.Detail)
		}
		t.WriteString("\n")
		if r.Link != nil {
			t.WriteString("  " + r.Link.URL + "\n")
		}
	}
	if len(d.Rows) > 0 {
		t.WriteString("\n")
	}
	if d.Button != nil {
		t.WriteString(d.Button.Text + ": " + d.Button.URL + "\n\n")
	}
	for _, l := range d.Links {
		t.WriteString(l.Text + ": " + l.URL + "\n")
	}
	if len(d.Links) > 0 {
		t.WriteString("\n")
	}
	if len(d.Footer) > 0 || d.Prefs != nil || d.Unsub != nil {
		t.WriteString("--\n")
	}
	for _, f := range d.Footer {
		t.WriteString(f + "\n")
	}
	if d.Prefs != nil {
		t.WriteString(d.Prefs.Text + ": " + d.Prefs.URL + "\n")
	}
	if d.Unsub != nil {
		t.WriteString(d.Unsub.Text + ": " + d.Unsub.URL + "\n")
	}
	return h.String(), t.String()
}

var (
	emailCodeRe = regexp.MustCompile(`\b\d{6}\b`)
	urlLineRe   = regexp.MustCompile(`^https?://\S+$`)
)

// textEmailDoc turns the plain-text system emails (codes, invitations, address changes) into the shared
// look: paragraphs stay paragraphs, a line that is only a link becomes the button, a 6-digit code is shown
// large.
func textEmailDoc(subject, text string) emailDoc {
	d := emailDoc{Title: subject, Preheader: firstLine(text)}
	for _, para := range strings.Split(strings.TrimSpace(text), "\n\n") {
		var keep []string
		for _, line := range strings.Split(para, "\n") {
			line = strings.TrimSpace(line)
			if urlLineRe.MatchString(line) && d.Button == nil {
				d.Button = &emailLink{Text: buttonText(subject), URL: line}
				continue
			}
			if line != "" {
				keep = append(keep, line)
			}
		}
		if len(keep) > 0 {
			d.Paragraphs = append(d.Paragraphs, strings.Join(keep, " "))
		}
	}
	if d.Code == "" && len(d.Paragraphs) > 0 {
		if code := emailCodeRe.FindString(d.Paragraphs[0]); code != "" {
			d.Code = code
		}
	}
	return d
}

func buttonText(subject string) string {
	if strings.Contains(strings.ToLower(subject), "invited") {
		return "Accept the invitation"
	}
	return "Open Staqly"
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return truncateRunes(s, 140)
}

// emailLogoURL is the logo the dashboard serves (no tracker, no third party); "" when the public address is unknown.
func (s *Service) emailLogoURL() string {
	base := strings.TrimRight(s.Cfg.PublicDashboardURL, "/")
	if base == "" {
		return ""
	}
	return base + "/brand/icon-192.png"
}
