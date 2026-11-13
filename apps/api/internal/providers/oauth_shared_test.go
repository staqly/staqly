package providers

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"
	"time"
)

// A token answer goes into a copy of the credentials: the refresh token only when a new one came, each expiry only
// when the platform named it (TikTok and Pinterest name the refresh token's under different fields).
func TestRefreshableTokenInto(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("CET", 3600))
	old := Credentials{"access_token": "a0", "refresh_token": "r0", "open_id": "u", "refresh_expires_at": "keep"}
	for _, c := range []struct {
		name string
		t    refreshableToken
		want Credentials
	}{
		{"rotated, both expiries (TikTok)", refreshableToken{AccessToken: "a1", RefreshToken: "r1", ExpiresIn: 86400, RefreshExpiresIn: 31536000},
			Credentials{"access_token": "a1", "refresh_token": "r1", "open_id": "u", "expires_at": "2026-01-03T02:04:05Z", "refresh_expires_at": "2027-01-02T02:04:05Z"}},
		{"Pinterest's field", refreshableToken{AccessToken: "a1", RefreshTokenExpiresIn: 60},
			Credentials{"access_token": "a1", "refresh_token": "r0", "open_id": "u", "refresh_expires_at": "2026-01-02T02:05:05Z"}},
		{"the larger of the two", refreshableToken{AccessToken: "a1", RefreshExpiresIn: 60, RefreshTokenExpiresIn: 120},
			Credentials{"access_token": "a1", "refresh_token": "r0", "open_id": "u", "refresh_expires_at": "2026-01-02T02:06:05Z"}},
		{"no refresh token, no expiry: old ones stay", refreshableToken{AccessToken: "a1"},
			Credentials{"access_token": "a1", "refresh_token": "r0", "open_id": "u", "refresh_expires_at": "keep"}},
	} {
		got := c.t.into(old, now)
		if len(got) != len(c.want) {
			t.Errorf("%s: %v", c.name, got)
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("%s: %s = %q, want %q", c.name, k, got[k], v)
			}
		}
	}
	if old["access_token"] != "a0" || len(old) != 4 {
		t.Fatalf("the original changed: %v", old)
	}
}

func TestHasScope(t *testing.T) {
	for _, c := range []struct {
		granted, want string
		ok            bool
	}{
		{"user.info.basic,video.publish", "video.publish", true},
		{"identity submit read", "submit", true},
		{"identity, submit", "submit", true},
		{"video.publish.extra", "video.publish", false},
		{"pins:read", "pins:write", false},
		{"", "submit", false},
		{"submit", "", false},
	} {
		if hasScope(c.granted, c.want) != c.ok {
			t.Errorf("hasScope(%q, %q) != %v", c.granted, c.want, c.ok)
		}
	}
}

func memFile(name, mimeType, body string, size int64) MediaFile {
	return MediaFile{Name: name, MimeType: mimeType, Size: size, Open: func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(body)), nil
	}}
}

// sizedForm's body is a valid multipart form whose length is known up front, with or without the files' sizes.
func TestSizedForm(t *testing.T) {
	ctx := context.Background()
	closed := 0
	tracked := memFile(`we"ird\.png`, "", "PNGDATA", 7)
	inner := tracked.Open
	tracked.Open = func(ctx context.Context) (io.ReadCloser, error) {
		rc, _ := inner(ctx)
		return closeCounter{rc, &closed}, nil
	}
	body, length, ct, done, err := sizedForm(ctx, [][2]string{{"key", "uploads/x"}, {"policy", "p"}},
		[]formFile{{"file", tracked}, {"thumb", memFile("t.jpg", "image/jpeg", "unknown-size", 0)}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(body)
	done()
	if int64(len(raw)) != length {
		t.Fatalf("said %d bytes, sent %d", length, len(raw))
	}
	if closed != 1 {
		t.Fatalf("files closed %d times", closed)
	}
	_, params, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatal(err)
	}
	mr := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	var got []string
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		b, _ := io.ReadAll(p)
		got = append(got, p.FormName()+"|"+p.FileName()+"|"+p.Header.Get("Content-Type")+"|"+string(b))
	}
	want := []string{"key|||uploads/x", "policy|||p", `file|we"ird\.png|application/octet-stream|PNGDATA`, "thumb|t.jpg|image/jpeg|unknown-size"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("parts:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// A file longer than its size is cut to it, so the length stays true.
	body, length, _, _, _ = sizedForm(ctx, nil, []formFile{{"file", memFile("a", "x/y", "0123456789", 4)}})
	raw, _ = io.ReadAll(body)
	if int64(len(raw)) != length || !bytes.Contains(raw, []byte("0123\r\n")) || bytes.Contains(raw, []byte("01234")) {
		t.Fatalf("cut: %q", raw)
	}

	broken := MediaFile{Name: "gone.png", Open: func(context.Context) (io.ReadCloser, error) { return nil, errors.New("no such key") }}
	if _, _, _, done, err := sizedForm(ctx, nil, []formFile{{"file", broken}}); err == nil || !strings.Contains(err.Error(), "gone.png") {
		t.Fatalf("broken file: %v", err)
	} else {
		done()
	}
}

type closeCounter struct {
	io.Reader
	n *int
}

func (c closeCounter) Close() error {
	*c.n++
	return nil
}

func TestReadMedia(t *testing.T) {
	ctx := context.Background()
	b, err := readMedia(ctx, memFile("a", "", "hello", 0), 0)
	if err != nil || string(b) != "hello" {
		t.Fatalf("read %q %v", b, err)
	}
	// With a limit, one byte over is read so the caller can tell; exactly at the limit is fine.
	if b, _ := readMedia(ctx, memFile("a", "", "hello", 0), 5); string(b) != "hello" {
		t.Fatalf("at the limit: %q", b)
	}
	if b, err := readMedia(ctx, memFile("a", "", "hello!!", 0), 5); err != nil || string(b) != "hello!" {
		t.Fatalf("over the limit: %q %v", b, err)
	}
	broken := MediaFile{Name: "gone.png", Open: func(context.Context) (io.ReadCloser, error) { return nil, errors.New("no such key") }}
	if _, err := readMedia(ctx, broken, 0); err == nil || !strings.Contains(err.Error(), "gone.png") {
		t.Fatalf("broken: %v", err)
	}
	// Without a limit, a file over maxMediaRead is refused (a reader that never ends stands in for it).
	big := MediaFile{Name: "huge.mp4", Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(zeroes{}), nil }}
	if _, err := readMedia(ctx, big, 0); errorClass(err) != "rejected" || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("huge: %v", err)
	}
}

type zeroes struct{}

func (zeroes) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestPrivateHost(t *testing.T) {
	for h, want := range map[string]bool{
		"localhost": true, "127.0.0.1": true, "::1": true, "10.1.2.3": true, "192.168.0.5": true, "172.16.0.1": true,
		"169.254.1.1": true, "fe80::1": true, "fd00::1": true, "nas": true, "app.localhost": true, "pi.local": true,
		"db.internal": true, "router.lan": true,
		"8.8.8.8": false, "172.32.0.1": false, "2001:4860::1": false, "mastodon.social": false, "local.example.com": false,
		"evil-localhost.com": false, "lan.example": false,
	} {
		if privateHost(h) != want {
			t.Errorf("privateHost(%q) != %v", h, want)
		}
	}
}

func TestImageSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.White)
	var p, g bytes.Buffer
	_ = png.Encode(&p, img)
	_ = gif.Encode(&g, img, nil)
	for name, b := range map[string][]byte{"png": p.Bytes(), "gif": g.Bytes()} {
		if w, h, ok := imageSize(b); !ok || w != 3 || h != 2 {
			t.Errorf("%s: %d x %d %v", name, w, h, ok)
		}
	}
	// A minimal lossless WebP (VP8L) header: 2 x 2.
	webp := []byte("RIFF\x1a\x00\x00\x00WEBPVP8L\x0d\x00\x00\x00\x2f\x01\x40\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	if w, h, ok := imageSize(webp); !ok || w != 2 || h != 2 {
		t.Errorf("webp: %d x %d %v", w, h, ok)
	}
	if _, _, ok := imageSize([]byte("not a picture")); ok {
		t.Error("text read as a picture")
	}
}

func TestPlatformHostFromAnyBase(t *testing.T) {
	for _, c := range []struct {
		raw, base, domain string
		ok                bool
	}{
		{"https://open.tiktokapis.com/upload/1", "https://open.tiktokapis.com", "tiktokapis.com", true},
		{"https://upload.tiktokapis.com/x", "https://open.tiktokapis.com", "tiktokapis.com", true},
		{"http://upload.tiktokapis.com/x", "https://open.tiktokapis.com", "tiktokapis.com", false},
		{"https://tiktokapis.com.evil.example/x", "https://open.tiktokapis.com", "tiktokapis.com", false},
		{"https://eviltiktokapis.com/x", "https://open.tiktokapis.com", "tiktokapis.com", false},
		{"http://127.0.0.1:5555/upload", "http://127.0.0.1:5555", "tiktokapis.com", true},
		{"http://127.0.0.1:5556/upload", "http://127.0.0.1:5555", "tiktokapis.com", false},
		{"/relative", "https://open.tiktokapis.com", "tiktokapis.com", false},
		{"https://[::1", "https://open.tiktokapis.com", "tiktokapis.com", false},
	} {
		if platformHost(c.raw, c.base, c.domain) != c.ok {
			t.Errorf("platformHost(%q, %q) != %v", c.raw, c.base, c.ok)
		}
	}
}
