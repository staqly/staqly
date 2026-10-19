package httpapi_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

func testImage(w, h int, alpha bool) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			a := uint8(255)
			if alpha && x < w/2 {
				a = 0
			}
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 90, A: a})
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pngHeaderOnly is a PNG that declares w×h pixels but has no image data: a
// decompression-bomb stand-in that DecodeConfig accepts.
func pngHeaderOnly(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 2 // 8-bit RGB
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

// jpegWithOrientation inserts an EXIF APP1 segment with the given
// Orientation tag right after the JPEG's SOI marker.
func jpegWithOrientation(t *testing.T, img image.Image, orient uint16) []byte {
	var j bytes.Buffer
	if err := jpeg.Encode(&j, img, nil); err != nil {
		t.Fatal(err)
	}
	tiff := []byte("II*\x00\x08\x00\x00\x00") // little endian, IFD0 at offset 8
	tiff = binary.LittleEndian.AppendUint16(tiff, 1)
	tiff = binary.LittleEndian.AppendUint16(tiff, 0x0112) // Orientation
	tiff = binary.LittleEndian.AppendUint16(tiff, 3)      // SHORT
	tiff = binary.LittleEndian.AppendUint32(tiff, 1)
	tiff = binary.LittleEndian.AppendUint16(tiff, orient)
	tiff = append(tiff, 0, 0, 0, 0, 0, 0) // value padding + next IFD
	seg := append([]byte("Exif\x00\x00"), tiff...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1}
	out = binary.BigEndian.AppendUint16(out, uint16(len(seg)+2))
	out = append(out, seg...)
	return append(out, j.Bytes()[2:]...)
}

func getThumb(t *testing.T, url string, hdr ...string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, body
}

func thumbFiles(t *testing.T, svc *app.Service) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(svc.Cfg.UploadDir, "thumbs", "*", "*"))
	return files
}

func TestMediaThumbnails(t *testing.T) {
	svc, srv := newLocal(t)
	ctx := context.Background()
	p, err := svc.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	upload := func(name string, b []byte) app.Media {
		t.Helper()
		m, err := svc.UploadMedia(ctx, p, name, bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	decode := func(body []byte) image.Config {
		t.Helper()
		cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("decode thumbnail: %v", err)
		}
		return cfg
	}

	opaque := upload("big.png", encodePNG(t, testImage(1600, 800, false)))
	url := srv.URL + "/v1/media/" + opaque.ID

	t.Run("sizes and headers", func(t *testing.T) {
		for _, w := range []int{160, 320, 640, 1280} {
			resp, body := getThumb(t, url+"?w="+strconv.Itoa(w))
			if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" {
				t.Fatalf("w=%d: %d %s", w, resp.StatusCode, resp.Header.Get("Content-Type"))
			}
			if cfg := decode(body); cfg.Width != w || cfg.Height != w/2 {
				t.Fatalf("w=%d: got %dx%d", w, cfg.Width, cfg.Height)
			}
			h := resp.Header
			if h.Get("Cache-Control") != "private, max-age=31536000, immutable" || h.Get("ETag") == "" ||
				!strings.Contains(h.Get("Content-Security-Policy"), "sandbox") || h.Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("w=%d headers: %v", w, h)
			}
		}
	})

	t.Run("cache hit", func(t *testing.T) {
		before := thumbFiles(t, svc)
		r1, b1 := getThumb(t, url+"?w=320")
		_, b2 := getThumb(t, url+"?w=320")
		if !bytes.Equal(b1, b2) || len(thumbFiles(t, svc)) != len(before) {
			t.Fatal("second request must be served from the cached file")
		}
		r3, _ := getThumb(t, url+"?w=320", "If-None-Match", r1.Header.Get("ETag"))
		if r3.StatusCode != http.StatusNotModified {
			t.Fatalf("If-None-Match: %d", r3.StatusCode)
		}
	})

	t.Run("concurrent first requests resize once", func(t *testing.T) {
		m := upload("c.png", encodePNG(t, testImage(900, 900, false)))
		var wg sync.WaitGroup
		for range 16 {
			wg.Go(func() {
				resp, body := getThumb(t, srv.URL+"/v1/media/"+m.ID+"?w=640")
				if resp.StatusCode != 200 || len(body) == 0 {
					t.Errorf("status %d", resp.StatusCode)
				}
			})
		}
		wg.Wait()
	})

	t.Run("alpha stays PNG", func(t *testing.T) {
		m := upload("a.png", encodePNG(t, testImage(800, 400, true)))
		resp, body := getThumb(t, srv.URL+"/v1/media/"+m.ID+"?w=160")
		if resp.Header.Get("Content-Type") != "image/png" || decode(body).Width != 160 {
			t.Fatalf("alpha thumbnail: %s", resp.Header.Get("Content-Type"))
		}
	})

	t.Run("exif orientation", func(t *testing.T) {
		m := upload("o.jpg", jpegWithOrientation(t, testImage(400, 200, false), 6)) // displayed 200×400
		_, body := getThumb(t, srv.URL+"/v1/media/"+m.ID+"?w=160")
		if cfg := decode(body); cfg.Width != 160 || cfg.Height != 320 {
			t.Fatalf("rotated thumbnail: %dx%d", cfg.Width, cfg.Height)
		}
	})

	t.Run("never upscales", func(t *testing.T) {
		orig := encodePNG(t, testImage(100, 50, false))
		m := upload("small.png", orig)
		resp, body := getThumb(t, srv.URL+"/v1/media/"+m.ID+"?w=640")
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(body, orig) {
			t.Fatalf("small image must be served as is: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	})

	t.Run("bad width and non-images", func(t *testing.T) {
		for _, q := range []string{"?w=300", "?w=0", "?w=abc", "?w=", "?w=99999"} {
			if resp, _ := getThumb(t, url+q); resp.StatusCode != 422 {
				t.Fatalf("%s: %d", q, resp.StatusCode)
			}
		}
		voice := upload("v.ogg", append([]byte("OggS\x00\x02"), make([]byte, 64)...))
		if resp, _ := getThumb(t, srv.URL+"/v1/media/"+voice.ID+"?w=320"); resp.StatusCode != 422 {
			t.Fatalf("audio thumbnail: %d", resp.StatusCode)
		}
		if resp, _ := getThumb(t, srv.URL+"/v1/media/med_missing?w=320"); resp.StatusCode != 404 {
			t.Fatalf("missing media: %d", resp.StatusCode)
		}
	})

	t.Run("decompression bomb is not decoded", func(t *testing.T) {
		bomb := pngHeaderOnly(20000, 20000) // 400 MP
		m := upload("bomb.png", bomb)
		before := len(thumbFiles(t, svc))
		resp, body := getThumb(t, srv.URL+"/v1/media/"+m.ID+"?w=320")
		if resp.StatusCode != 200 || !bytes.Equal(body, bomb) || len(thumbFiles(t, svc)) != before {
			t.Fatalf("bomb: %d, %d bytes", resp.StatusCode, len(body))
		}
	})

	t.Run("deleting media removes its thumbnails", func(t *testing.T) {
		m := upload("d.png", encodePNG(t, testImage(700, 300, false)))
		getThumb(t, srv.URL+"/v1/media/"+m.ID+"?w=160")
		getThumb(t, srv.URL+"/v1/media/"+m.ID+"?w=640")
		stor := storageOf(t, svc, m.ID)
		count := func() int {
			n := 0
			for _, f := range thumbFiles(t, svc) {
				if strings.Contains(f, stor) {
					n++
				}
			}
			return n
		}
		if count() != 2 {
			t.Fatalf("want 2 cached thumbnails, got %d", count())
		}
		if err := svc.DeleteMedia(ctx, p, m.ID); err != nil {
			t.Fatal(err)
		}
		if count() != 0 {
			t.Fatal("thumbnails left behind")
		}
	})
}

func storageOf(t *testing.T, svc *app.Service, id string) string {
	t.Helper()
	var s string
	if err := svc.DB.QueryRow("SELECT storage_name FROM media WHERE id = ?", id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}
