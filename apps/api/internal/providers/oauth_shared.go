package providers

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif" // decoders for imageSize
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

// Helpers shared by the OAuth platforms Reddit, TikTok, Pinterest, Dribbble and Tumblr.

// refreshableToken is an OAuth 2.0 token answer; platforms that name the refresh token's lifetime use one of the
// two expiry fields.
type refreshableToken struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshExpiresIn      int64  `json:"refresh_expires_in"`       // TikTok
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"` // Pinterest
	Scope                 string `json:"scope"`
	OpenID                string `json:"open_id"` // TikTok
	Error                 string `json:"error"`
	ErrorDescription      string `json:"error_description"`
}

// into stores the token in a copy of c: a refresh token only when a new one came (the old one stays otherwise).
func (t refreshableToken) into(c Credentials, now time.Time) Credentials {
	out := Credentials{}
	for k, v := range c {
		out[k] = v
	}
	out["access_token"] = t.AccessToken
	if t.RefreshToken != "" {
		out["refresh_token"] = t.RefreshToken
	}
	if t.ExpiresIn > 0 {
		out["expires_at"] = now.Add(time.Duration(t.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	if n := max(t.RefreshExpiresIn, t.RefreshTokenExpiresIn); n > 0 {
		out["refresh_expires_at"] = now.Add(time.Duration(n) * time.Second).UTC().Format(time.RFC3339)
	}
	return out
}

// hasScope says whether a granted scope list (space- or comma-separated) holds want.
func hasScope(granted, want string) bool {
	for _, s := range strings.FieldsFunc(granted, func(r rune) bool { return r == ' ' || r == ',' }) {
		if s == want {
			return true
		}
	}
	return false
}

// formFile is one file of a sizedForm.
type formFile struct {
	Field string
	M     MediaFile
}

// sizedForm builds a multipart body that streams the files instead of holding them in memory, and says its length
// up front (S3 upload forms refuse a body without one). A file whose size is unknown is read into memory. close
// releases the opened files; call it once the request is done.
func sizedForm(ctx context.Context, fields [][2]string, files []formFile) (body io.Reader, length int64, contentType string, close func(), err error) {
	var opened []io.Closer
	close = func() {
		for _, c := range opened {
			_ = c.Close()
		}
	}
	var segs []io.Reader
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	cut := func() {
		b := append([]byte(nil), buf.Bytes()...)
		segs = append(segs, bytes.NewReader(b))
		length += int64(len(b))
		buf.Reset()
	}
	for _, f := range fields {
		if err := w.WriteField(f[0], f[1]); err != nil {
			return nil, 0, "", close, err
		}
	}
	for _, f := range files {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, quoteEscaper.Replace(f.Field), quoteEscaper.Replace(f.M.Name)))
		mt := f.M.MimeType
		if mt == "" {
			mt = "application/octet-stream"
		}
		h.Set("Content-Type", mt)
		if _, err := w.CreatePart(h); err != nil {
			return nil, 0, "", close, err
		}
		cut()
		r, err := f.M.Reader(ctx)
		if err != nil {
			return nil, 0, "", close, fmt.Errorf("cannot read %s: %w", f.M.Name, err)
		}
		opened = append(opened, r)
		if f.M.Size > 0 {
			segs = append(segs, io.LimitReader(r, f.M.Size))
			length += f.M.Size
			continue
		}
		b, err := io.ReadAll(r)
		if err != nil {
			return nil, 0, "", close, fmt.Errorf("cannot read %s: %w", f.M.Name, err)
		}
		segs = append(segs, bytes.NewReader(b))
		length += int64(len(b))
	}
	if err := w.Close(); err != nil {
		return nil, 0, "", close, err
	}
	cut()
	return io.MultiReader(segs...), length, w.FormDataContentType(), close, nil
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

// readMedia reads a whole file (pictures: a few MB), at most limit bytes when limit > 0.
// With limit 0 a file over maxMediaRead is refused; with a limit the caller checks len > limit for its own message.
func readMedia(ctx context.Context, m MediaFile, limit int64) ([]byte, error) {
	capped := limit <= 0
	if capped {
		limit = maxMediaRead
	}
	r, err := m.Reader(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", m.Name, err)
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", m.Name, err)
	}
	if capped && int64(len(b)) > limit {
		return nil, &Failure{Kind: ErrRejected, Msg: m.Name + " is too large to send"}
	}
	return b, nil
}

// maxMediaRead caps a file read whole into memory (a platform that needs its length up front, or the bytes inline).
// ponytail: whole-file reads, stream with a known size if larger files must go through.
const maxMediaRead = 256 << 20

// privateHost: a name or address on this machine or a private network, where http:// and ws:// stay off the internet.
func privateHost(h string) bool {
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	return h == "localhost" || !strings.Contains(h, ".") || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") ||
		strings.HasSuffix(h, ".internal") || strings.HasSuffix(h, ".lan")
}

// imageSize reads a picture's width and height from its header (JPEG, PNG, GIF, WebP); ok is false for others.
func imageSize(b []byte) (w, h int, ok bool) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

// platformHost: an address a platform answered with (an upload session) that its token may go to: https on the
// platform's own domain, or the API base itself (tests point it at a local server).
func platformHost(raw, base, domain string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	if b, err := url.Parse(base); err == nil && u.Scheme == b.Scheme && u.Host == b.Host {
		return true
	}
	h := u.Hostname()
	return u.Scheme == "https" && (h == domain || strings.HasSuffix(h, "."+domain))
}
