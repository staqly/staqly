package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/netip"
	"net/smtp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Email: SMTP with STARTTLS (default), implicit TLS (port 465) or — only if
// explicitly configured — plaintext. Messages are multipart text + HTML with
// Approve / Reject links to the decision confirmation pages.

func (s *Service) sendEmail(ctx context.Context, ch *notifyChannel, msg *notifyMessage) error {
	c := ch.cfg
	from, err := mail.ParseAddress(c.From)
	if err != nil {
		return errors.New("invalid from address")
	}
	raw := buildEmail(from, c.To, "[staqly] "+msg.title(), emailText(msg), emailHTML(msg))

	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	policy := WebhookConfig{AllowPrivate: s.Env("NOTIFY_ALLOW_PRIVATE", "") == "true", AllowLoopback: s.Env("NOTIFY_ALLOW_LOOPBACK", "") == "true"}
	dialer := &net.Dialer{Timeout: 15 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return err
		}
		return checkNotifyIP(ip.Unmap(), policy)
	}}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp connect: %w", unwrapOpErr(err))
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(45 * time.Second))
	tlsCfg := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	if c.TLS == "tls" {
		tc := tls.Client(conn, tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("smtp tls: %w", err)
		}
		conn = tc
	}
	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	defer cl.Close()
	if err := cl.Hello("staqly"); err != nil {
		return fmt.Errorf("smtp hello: %w", err)
	}
	if c.TLS == "starttls" {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return errors.New("smtp server does not offer STARTTLS (set tls to \"none\" only for a trusted local relay)")
		}
		if err := cl.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if c.Username != "" {
		// PlainAuth refuses to send credentials over an unencrypted connection (except to localhost).
		if err := cl.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := cl.Mail(from.Address); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	for _, to := range c.To {
		if err := cl.Rcpt(to); err != nil {
			return fmt.Errorf("smtp RCPT TO: %w", err)
		}
	}
	w, err := cl.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	_ = cl.Quit()
	return nil
}

func unwrapOpErr(err error) error {
	var oe *net.OpError
	if errors.As(err, &oe) && oe.Err != nil {
		return oe.Err
	}
	return err
}

func headerSafe(v string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return ' '
		}
		return r
	}, v)
}

func buildEmail(from *mail.Address, to []string, subject, text, htmlBody string) []byte {
	var b bytes.Buffer
	boundary := "staqly-" + randomString(12)
	hdr := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	hdr("From", from.String())
	hdr("To", strings.Join(to, ", "))
	hdr("Subject", mime.QEncoding.Encode("utf-8", headerSafe(truncateRunes(subject, 180))))
	hdr("Date", time.Now().UTC().Format(time.RFC1123Z))
	domain := "staqly.local"
	if _, d, ok := strings.Cut(from.Address, "@"); ok {
		domain = d
	}
	hdr("Message-ID", "<"+randomString(16)+"@"+domain+">")
	hdr("Auto-Submitted", "auto-generated")
	hdr("MIME-Version", "1.0")
	hdr("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")
	for _, part := range []struct{ ct, body string }{{"text/plain", text}, {"text/html", htmlBody}} {
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary, part.ct)
		qp := quotedprintable.NewWriter(&b)
		_, _ = qp.Write([]byte(strings.ReplaceAll(part.body, "\n", "\r\n")))
		_ = qp.Close()
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes()
}

func emailText(msg *notifyMessage) string {
	if msg.Headline != "" {
		return msg.Headline + "\n"
	}
	var b strings.Builder
	for i, e := range msg.Entries {
		if i == 25 {
			fmt.Fprintf(&b, "...and %d more\n", len(msg.Entries)-25)
			break
		}
		fmt.Fprintf(&b, "%s: %s\n%s\n", eventLabel(e), e.Title, entryMeta(e))
		if p := truncateRunes(e.Preview, 1000); p != "" {
			b.WriteString("\n" + p + "\n")
		}
		if e.Note != "" {
			b.WriteString("\n" + e.Note + "\n")
		}
		if e.WebURL != "" {
			fmt.Fprintf(&b, "Open:    %s\n", e.WebURL)
		}
		b.WriteString("\n----\n\n")
	}
	return b.String()
}

func emailHTML(msg *notifyMessage) string {
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString(`<!doctype html><html><body style="font-family:system-ui,-apple-system,Segoe UI,sans-serif;color:#111;max-width:640px;margin:0 auto;padding:16px">`)
	if msg.Headline != "" {
		b.WriteString("<p>" + esc(msg.Headline) + "</p>")
	}
	for i, e := range msg.Entries {
		if i == 25 {
			fmt.Fprintf(&b, "<p>...and %d more</p>", len(msg.Entries)-25)
			break
		}
		b.WriteString(`<div style="border:1px solid #e5e7eb;border-radius:8px;padding:16px;margin-bottom:16px">`)
		b.WriteString(`<div style="font-size:12px;color:#6b7280">` + esc(eventLabel(e)+" · "+entryMeta(e)) + `</div>`)
		b.WriteString(`<h2 style="font-size:18px;margin:4px 0 8px">` + esc(e.Title) + `</h2>`)
		if p := truncateRunes(e.Preview, 1000); p != "" {
			b.WriteString(`<p style="white-space:pre-wrap;color:#374151">` + esc(p) + `</p>`)
		}
		if e.Note != "" {
			b.WriteString(`<p style="color:#92400e">` + esc(e.Note) + `</p>`)
		}
		b.WriteString("<p>")
		if e.WebURL != "" {
			b.WriteString(`<a href="` + esc(e.WebURL) + `">Open in staqly</a>`)
		}
		b.WriteString("</p></div>")
	}
	b.WriteString(`</body></html>`)
	return b.String()
}
