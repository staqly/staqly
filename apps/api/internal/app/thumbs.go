package app

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
	"golang.org/x/sync/singleflight"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// ThumbWidths are the widths GET /v1/media/{id}?w= accepts. A fixed set keeps
// the on-disk cache bounded (at most len(ThumbWidths) files per image).
var ThumbWidths = []int{160, 320, 640, 1280}

// maxThumbPixels guards against decompression bombs: a small PNG can declare
// 100k×100k pixels. Larger images are served as they are (the browser has its
// own limits) instead of being decoded here.
const maxThumbPixels = 50_000_000

// thumbnailer bounds and deduplicates resizing: one decode per (file, width)
// at a time (singleflight) and at most NumCPU decodes in parallel.
type thumbnailer struct {
	group singleflight.Group
	sem   chan struct{}
}

func newThumbnailer() *thumbnailer {
	return &thumbnailer{sem: make(chan struct{}, runtime.NumCPU())}
}

// Thumb is what MediaThumb serves: a cached thumbnail or, when resizing does
// not apply (already small, AVIF, too many pixels), the original file.
type Thumb struct {
	File     *os.File
	Media    db.Medium
	MimeType string
	Resized  bool
}

func (s *Service) thumbDir(storage string) string {
	return filepath.Join(s.Cfg.UploadDir, "thumbs", storage[:2])
}

func (s *Service) thumbPath(storage string, w int, ext string) string {
	return filepath.Join(s.thumbDir(storage), storage+"_"+strconv.Itoa(w)+ext)
}

// removeMediaFiles deletes an upload and its cached thumbnails. The error is
// the original's (missing files are not an error).
func (s *Service) removeMediaFiles(storage string) error {
	for _, w := range ThumbWidths {
		for _, ext := range []string{".jpg", ".png"} {
			_ = os.Remove(s.thumbPath(storage, w, ext))
		}
	}
	if err := os.Remove(s.mediaPath(storage)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// MediaThumb returns an image scaled down to width w (one of ThumbWidths),
// generating and caching it on first use. Thumbnails are JPEG for opaque
// images and PNG when there is transparency; never wider than the original.
func (s *Service) MediaThumb(ctx context.Context, p Principal, id string, w int) (Thumb, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return Thumb{}, err
	}
	if !slices.Contains(ThumbWidths, w) {
		return Thumb{}, invalid("w", "must be one of 160, 320, 640, 1280")
	}
	m, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil {
		return Thumb{}, notFound("media")
	}
	switch m.MimeType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	case "image/avif": // no pure-Go decoder: browsers display the original
		return s.originalThumb(m)
	default:
		return Thumb{}, invalid("w", "thumbnails are only available for PNG, JPEG, GIF and WebP images")
	}
	for _, ext := range []string{".jpg", ".png"} {
		if f, err := os.Open(s.thumbPath(m.StorageName, w, ext)); err == nil {
			return Thumb{File: f, Media: m, MimeType: thumbMime(ext), Resized: true}, nil
		}
	}
	// ponytail: a thumbnail being written while its media is deleted can
	// outlive it (orphan file, never served: the media row is gone).
	key := m.StorageName + "_" + strconv.Itoa(w)
	v, err, _ := s.thumbs.group.Do(key, func() (any, error) {
		// Not tied to ctx: waiters for the same key share this result, so
		// one cancelled request must not fail the others.
		s.thumbs.sem <- struct{}{}
		defer func() { <-s.thumbs.sem }()
		return s.makeThumb(m, w)
	})
	if err != nil {
		return Thumb{}, err
	}
	path := v.(string)
	if path == "" {
		return s.originalThumb(m)
	}
	f, err := os.Open(path)
	if err != nil {
		return Thumb{}, err
	}
	return Thumb{File: f, Media: m, MimeType: thumbMime(filepath.Ext(path)), Resized: true}, nil
}

func thumbMime(ext string) string {
	if ext == ".png" {
		return "image/png"
	}
	return "image/jpeg"
}

func (s *Service) originalThumb(m db.Medium) (Thumb, error) {
	f, err := os.Open(s.mediaPath(m.StorageName))
	if errors.Is(err, os.ErrNotExist) {
		return Thumb{}, notFound("media file")
	}
	if err != nil {
		return Thumb{}, err
	}
	return Thumb{File: f, Media: m, MimeType: m.MimeType}, nil
}

// makeThumb writes the thumbnail and returns its path, or "" when the
// original should be served instead (not wider than w, or too many pixels).
func (s *Service) makeThumb(m db.Medium, w int) (string, error) {
	src, err := os.Open(s.mediaPath(m.StorageName))
	if errors.Is(err, os.ErrNotExist) {
		return "", notFound("media file")
	}
	if err != nil {
		return "", err
	}
	defer src.Close()
	cfg, err := decodeConfig(bufio.NewReader(src), m.MimeType)
	if err != nil {
		return "", nil // undecodable: let the browser try the original
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxThumbPixels {
		return "", nil
	}
	orient := 1
	if m.MimeType == "image/jpeg" {
		if _, err := src.Seek(0, io.SeekStart); err != nil {
			return "", err
		}
		orient = jpegOrientation(bufio.NewReader(src))
	}
	srcW := cfg.Width
	if orient >= 5 { // rotated 90°: the displayed width is the stored height
		srcW = cfg.Height
	}
	if srcW <= w {
		return "", nil // never upscale
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	img, err := decodeImage(bufio.NewReader(src), m.MimeType)
	if err != nil {
		return "", nil
	}
	dst := resize(img, w, orient)
	ext := ".jpg"
	if !dst.Opaque() {
		ext = ".png"
	}
	path := s.thumbPath(m.StorageName, w, ext)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".thumb-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	bw := bufio.NewWriter(tmp)
	if ext == ".png" {
		err = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(bw, dst)
	} else {
		err = jpeg.Encode(bw, dst, &jpeg.Options{Quality: 80})
	}
	if err == nil {
		err = bw.Flush()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("encode thumbnail: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

func decodeConfig(r io.Reader, mime string) (image.Config, error) {
	switch mime {
	case "image/png":
		return png.DecodeConfig(r)
	case "image/jpeg":
		return jpeg.DecodeConfig(r)
	case "image/gif":
		return gif.DecodeConfig(r)
	default:
		return webp.DecodeConfig(r)
	}
}

func decodeImage(r io.Reader, mime string) (image.Image, error) {
	switch mime {
	case "image/png":
		return png.Decode(r)
	case "image/jpeg":
		return jpeg.Decode(r)
	case "image/gif":
		return gif.Decode(r) // first frame
	default:
		return webp.Decode(r)
	}
}

// resize scales img to width w (keeping the aspect ratio) and applies the
// EXIF orientation, so the thumbnail looks like the original in a browser.
// Large reductions go through a fast bilinear pass to 2×w first; the final
// pass is Catmull-Rom for sharpness.
func resize(img image.Image, w, orient int) *image.RGBA {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if orient >= 5 {
		sw, sh = sh, sw
	}
	h := max(1, (sh*w+sw/2)/sw)
	// Scale in stored orientation, then rotate the (small) result.
	tw, th := w, h
	if orient >= 5 {
		tw, th = h, w
	}
	src := img
	if b.Dx() > 4*tw {
		mid := image.NewRGBA(image.Rect(0, 0, 2*tw, max(1, 2*th)))
		draw.ApproxBiLinear.Scale(mid, mid.Bounds(), img, b, draw.Src, nil)
		src = mid
	}
	out := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.CatmullRom.Scale(out, out.Bounds(), src, src.Bounds(), draw.Src, nil)
	return orientRGBA(out, orient)
}

// orientRGBA applies EXIF orientation 2–8 (1 = as stored).
func orientRGBA(src *image.RGBA, orient int) *image.RGBA {
	if orient < 2 || orient > 8 {
		return src
	}
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := sw, sh
	if orient >= 5 {
		dw, dh = sh, sw
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range sh {
		for x := range sw {
			var dx, dy int
			switch orient {
			case 2:
				dx, dy = sw-1-x, y
			case 3:
				dx, dy = sw-1-x, sh-1-y
			case 4:
				dx, dy = x, sh-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = sh-1-y, x
			case 7:
				dx, dy = sh-1-y, sw-1-x
			case 8:
				dx, dy = y, sw-1-x
			}
			dst.SetRGBA(dx, dy, src.RGBAAt(x, y))
		}
	}
	return dst
}

// jpegOrientation reads the EXIF Orientation tag from a JPEG's APP1 segment
// (1 when absent or unreadable). r must be positioned at the start of the file.
func jpegOrientation(r io.Reader) int {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil || hdr != [2]byte{0xFF, 0xD8} {
		return 1
	}
	for range 32 { // markers before the image data
		var mk [4]byte
		if _, err := io.ReadFull(r, mk[:]); err != nil || mk[0] != 0xFF {
			return 1
		}
		n := int(binary.BigEndian.Uint16(mk[2:])) - 2
		if n < 0 || mk[1] == 0xDA { // start of scan: no EXIF before the data
			return 1
		}
		seg := make([]byte, n)
		if _, err := io.ReadFull(r, seg); err != nil {
			return 1
		}
		if mk[1] == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return exifOrientation(seg[6:])
		}
	}
	return 1
}

func exifOrientation(t []byte) int {
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(t[4:8]))
	if off < 8 || off+2 > len(t) {
		return 1
	}
	count := int(bo.Uint16(t[off:]))
	for i := range count {
		e := off + 2 + i*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			if v := int(bo.Uint16(t[e+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
