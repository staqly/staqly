package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	stdjpeg "image/jpeg"
	"image/png"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A channel's picture is kept as a data: URL of at most AvatarMax: a small JPEG as it is, a big picture shrunk to a
// JPEG; anything that isn't a picture, or doesn't download, leaves the link as it was.
func TestOwnAvatar(t *testing.T) {
	var small bytes.Buffer
	stdjpeg.Encode(&small, image.NewRGBA(image.Rect(0, 0, 40, 40)), nil)
	noise := image.NewRGBA(image.Rect(0, 0, 600, 600)) // noise: a PNG far over 64 KB
	r := rand.New(rand.NewPCG(1, 2))
	for i := range noise.Pix {
		noise.Pix[i] = uint8(r.IntN(256))
	}
	var big bytes.Buffer
	png.Encode(&big, noise)
	if big.Len() <= AvatarMax {
		t.Fatalf("the big picture is %d bytes", big.Len())
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/small.jpg":
			w.Write(small.Bytes())
		case "/big.png":
			w.Write(big.Bytes())
		case "/pic.svg":
			w.Write([]byte("<svg xmlns='http://www.w3.org/2000/svg'></svg>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()

	if got := OwnAvatar(ctx, srv.URL+"/small.jpg"); got != "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(small.Bytes()) {
		t.Fatalf("small: %.60s", got)
	}
	got := OwnAvatar(ctx, srv.URL+"/big.png?oe=6A1B2C3D")
	b64, ok := strings.CutPrefix(got, "data:image/jpeg;base64,")
	raw, _ := base64.StdEncoding.DecodeString(b64)
	if !ok || len(raw) == 0 || len(raw) > AvatarMax {
		t.Fatalf("big: %d bytes, %.60s", len(raw), got)
	}
	for _, link := range []string{srv.URL + "/pic.svg", srv.URL + "/gone", "", "data:image/png;base64,AAAA", "ftp://x/a.jpg"} {
		if got := OwnAvatar(ctx, link); got != link {
			t.Fatalf("%q became %.60s", link, got)
		}
	}
}
