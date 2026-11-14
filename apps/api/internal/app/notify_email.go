package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
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
// explicitly configured — plaintext. Messages are multipart text + HTML in the shared look (email_template.go).

func (s *Service) sendEmail(ctx context.Context, ch *notifyChannel, msg *notifyMessage) error {
	c := ch.cfg
	from, err := mail.ParseAddress(c.From)
	if err != nil {
		return errors.New("invalid from address")
	}
	htmlBody, text := renderEmail(s.alertEmailDoc(ch, msg), s.emailLogoURL())
	raw := buildEmail(from, c.To, msg.title(), text, htmlBody)
	policy := WebhookConfig{AllowPrivate: s.envFlag("NOTIFY_ALLOW_PRIVATE"), AllowLoopback: s.envFlag("NOTIFY_ALLOW_LOOPBACK")}
	err = smtpDeliver(ctx, c, from, raw, policy)
	if pe := (*policyError)(nil); s.Cfg.Mode == ModeCloud && errors.As(err, &pe) {
		return errors.New("smtp connect: " + pe.public) // without the address the host resolved to
	}
	return err
}

// mailConfigured: the operator set up the server's own mail (MAIL_SMTP_HOST, MAIL_FROM) for invitations.
func (s *Service) mailConfigured() bool {
	return s.Env("MAIL_SMTP_HOST", "") != "" && s.Env("MAIL_FROM", "") != ""
}

// sendSystemEmail: mail from the server itself (sign-in codes, invitations, address changes): the text is
// laid out in the shared look (textEmailDoc), with the text as the plain-text part.
func (s *Service) sendSystemEmail(ctx context.Context, to, subject, text string) error {
	return s.sendSystemEmailDoc(ctx, to, subject, textEmailDoc(subject, text), nil)
}

// sendSystemEmailDoc sends one email from the server (MAIL_*), through the same SMTP code as email alerts.
// The operator chose the relay, so private and loopback addresses are allowed (mailpit). extra adds headers
// (List-Unsubscribe).
func (s *Service) sendSystemEmailDoc(ctx context.Context, to, subject string, d emailDoc, extra map[string]string) error {
	port, _ := strconv.Atoi(s.Env("MAIL_SMTP_PORT", "587"))
	c := AlertConfig{Host: s.Env("MAIL_SMTP_HOST", ""), Port: port, Username: s.Env("MAIL_SMTP_USERNAME", ""),
		Password: s.Env("MAIL_SMTP_PASSWORD", ""), From: s.Env("MAIL_FROM", ""), To: []string{to}, TLS: s.Env("MAIL_SMTP_TLS", "starttls")}
	from, err := mail.ParseAddress(c.From)
	if err != nil {
		return errors.New("invalid MAIL_FROM address")
	}
	d.To = to
	htmlBody, text := renderEmail(d, s.emailLogoURL())
	raw := buildEmail(from, c.To, subject, text, htmlBody, extra)
	return smtpDeliver(ctx, c, from, raw, WebhookConfig{AllowPrivate: true, AllowLoopback: true})
}

func smtpDeliver(ctx context.Context, c AlertConfig, from *mail.Address, raw []byte, policy WebhookConfig) error {
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
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

func buildEmail(from *mail.Address, to []string, subject, text, htmlBody string, extra ...map[string]string) []byte {
	var b bytes.Buffer
	boundary := "staqly-" + randomString(12)
	hdr := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	hdr("From", from.String())
	hdr("To", headerSafe(strings.Join(to, ", ")))
	// Encoded words go one per folded line: a long non-ASCII subject would exceed 998 octets on one line.
	hdr("Subject", strings.ReplaceAll(mime.QEncoding.Encode("utf-8", headerSafe(truncateRunes(subject, 180))), " =?", "\r\n =?"))
	hdr("Date", time.Now().UTC().Format(time.RFC1123Z))
	domain := "staqly.local"
	if _, d, ok := strings.Cut(from.Address, "@"); ok {
		domain = d
	}
	hdr("Message-ID", "<"+randomString(16)+"@"+domain+">")
	hdr("Auto-Submitted", "auto-generated")
	for _, m := range extra {
		for k, v := range m {
			hdr(k, headerSafe(v))
		}
	}
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

// alertEmailDoc lays out an email alert: one row per event, the post's first line as the quote when there
// is one event.
func (s *Service) alertEmailDoc(ch *notifyChannel, msg *notifyMessage) emailDoc {
	d := emailDoc{Title: msg.title(), Preheader: msg.title()}
	if msg.Headline != "" {
		d.Paragraphs = []string{msg.Headline}
	}
	for i, e := range msg.Entries {
		if i == 25 {
			d.Paragraphs = append(d.Paragraphs, fmt.Sprintf("...and %d more.", len(msg.Entries)-25))
			break
		}
		r := emailRow{Label: eventLabel(e) + ": " + e.Title, Detail: entryMeta(e), Failed: strings.Contains(e.EventType, "failed")}
		if e.Note != "" {
			r.Detail = strings.TrimSpace(r.Detail + " · " + e.Note)
		}
		if e.WebURL != "" {
			r.Link = &emailLink{Text: "Open in Staqly", URL: e.WebURL}
		}
		d.Rows = append(d.Rows, r)
	}
	if len(msg.Entries) == 1 {
		d.Quote = truncateRunes(msg.Entries[0].Preview, 280)
	}
	name := ""
	if ch != nil {
		name = ch.row.Name
	}
	d.Footer = []string{fmt.Sprintf("You get this because the alert %q is set up in this workspace. Change it under Automation, Alerts.", name)}
	return d
}
