package providers

import (
	"bytes"
	"image"
	"image/png"
	"math/rand/v2"
	"testing"
)

// A noisy 2000x2000 PNG (several MB, nothing compresses it) comes out as a JPEG under 1 MB; a GIF is left alone.
func TestShrinkJPEG(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2000, 2000))
	r := rand.New(rand.NewPCG(1, 2))
	for i := range img.Pix {
		img.Pix[i] = uint8(r.IntN(256))
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if buf.Len() <= 1_000_000 {
		t.Fatalf("the test picture is only %d bytes", buf.Len())
	}
	out, ok := shrinkJPEG(buf.Bytes(), 1_000_000)
	if !ok || len(out) > 1_000_000 {
		t.Fatalf("ok %v, %d bytes", ok, len(out))
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(out)); err != nil || format != "jpeg" {
		t.Fatalf("came out as %q: %v", format, err)
	}
	gif := []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;")
	if _, ok := shrinkJPEG(gif, 10); ok {
		t.Fatal("a GIF was re-encoded")
	}
}
