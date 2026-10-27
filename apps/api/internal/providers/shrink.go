package providers

import (
	"bytes"
	"image"
	"image/color"
	stdjpeg "image/jpeg"
	_ "image/png" // decoders for shrinkJPEG
	"math"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// shrinkJPEG re-encodes a JPEG, PNG or WebP picture as a JPEG of at most max bytes: first at good quality, then
// smaller (each step 80 % of the sides) until it fits. Transparency goes onto white. GIFs (possibly animated) and
// pictures over 50 megapixels are left alone: ok is false and the caller refuses the file as before.
// ponytail: a few encodes per picture on publish; cache the result if a platform's pictures get published often.
func shrinkJPEG(raw []byte, max int) (out []byte, ok bool) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || format == "gif" || int64(cfg.Width)*int64(cfg.Height) > 50_000_000 {
		return nil, false
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	for step := 0; step < 12 && w >= 64 && h >= 64; step++ {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
		for _, q := range []int{88, 78} {
			var buf bytes.Buffer
			if stdjpeg.Encode(&buf, dst, &stdjpeg.Options{Quality: q}) == nil && buf.Len() <= max {
				return buf.Bytes(), true
			}
		}
		w, h = int(math.Round(float64(w)*0.8)), int(math.Round(float64(h)*0.8))
	}
	return nil, false
}
