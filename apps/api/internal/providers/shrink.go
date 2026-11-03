package providers

import (
	"bytes"
	"image"
	"image/color"
	stdjpeg "image/jpeg"
	"image/png"
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

// fitPicture draws a JPEG, PNG or WebP picture whole in the middle of a w x h one, white around it, and encodes
// it as a JPEG (a JPEG came in) or a PNG. GIFs and pictures over 50 megapixels are left alone: ok is false.
func fitPicture(raw []byte, w, h int) (out []byte, ok bool) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || format == "gif" || int64(cfg.Width)*int64(cfg.Height) > 50_000_000 {
		return nil, false
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}
	b := src.Bounds()
	scale := math.Min(float64(w)/float64(b.Dx()), float64(h)/float64(b.Dy()))
	sw, sh := max(1, int(math.Round(float64(b.Dx())*scale))), max(1, int(math.Round(float64(b.Dy())*scale)))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	at := image.Rect((w-sw)/2, (h-sh)/2, (w-sw)/2+sw, (h-sh)/2+sh)
	draw.CatmullRom.Scale(dst, at, src, b, draw.Over, nil)
	var buf bytes.Buffer
	if format == "jpeg" {
		err = stdjpeg.Encode(&buf, dst, &stdjpeg.Options{Quality: 90})
	} else {
		err = png.Encode(&buf, dst)
	}
	return buf.Bytes(), err == nil
}
