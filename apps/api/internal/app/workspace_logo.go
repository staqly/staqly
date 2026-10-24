package app

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/jpeg" // registers the JPEG decoder for image.DecodeConfig
	_ "image/png"  // registers the PNG decoder
	"strings"
)

// maxLogoBytes caps a decoded logo or picture: the dashboard sends 128 px, a few KB; this leaves room for a hand-made one.
const (
	maxLogoBytes = 64 << 10
	maxLogoSide  = 512
)

func checkLogo(v string) error { return checkImage("logo", v) }

// isDataImage: a picture uploaded to staqly (a data: URL), not a sign-in provider's (an https URL).
func isDataImage(v string) bool { return strings.HasPrefix(v, "data:image/") }

// checkImage accepts '' (none) or a PNG or JPEG data: URL, no larger than maxLogoBytes and maxLogoSide: a
// workspace logo or a person's picture. Never SVG: the dashboard shows it in an <img>, but a stored SVG is a script
// waiting for a context that runs it.
func checkImage(field, v string) error {
	if v == "" {
		return nil
	}
	var format, data string
	for _, f := range []string{"png", "jpeg"} {
		if rest, ok := strings.CutPrefix(v, "data:image/"+f+";base64,"); ok {
			format, data = f, rest
		}
	}
	if format == "" {
		return invalid(field, "send a PNG or JPEG image as a data: URL")
	}
	if base64.StdEncoding.DecodedLen(len(data)) > maxLogoBytes+3 {
		return invalid(field, "the image may be 64 KB at most")
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil || len(raw) > maxLogoBytes {
		return invalid(field, "the image may be 64 KB at most, base64-encoded")
	}
	cfg, got, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || got != format {
		return invalid(field, "the image is not a readable "+strings.ToUpper(format)+" file")
	}
	if cfg.Width > maxLogoSide || cfg.Height > maxLogoSide {
		return invalid(field, "the image may be 512 px wide and high at most")
	}
	return nil
}
