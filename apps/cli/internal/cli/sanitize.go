package cli

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"
)

// Everything the server sends (titles, content, metadata, error messages,
// event data ...) is untrusted: a hostile item could carry terminal escape
// sequences that clear the screen, rewrite earlier output, set the clipboard
// (OSC 52) or reorder text with bidi overrides. Human output therefore goes
// through clean(); --json output is escaped by safeJSON().

// clean strips C0/C1 control characters (keeping \t and \n), ANSI/OSC/DCS
// escape sequences, Unicode bidi overrides and invalid UTF-8.
func clean(s string) string { return sanitize(s, true) }

// cleanLine is clean for single-line contexts (table cells, key/value rows):
// tabs and newlines become spaces.
func cleanLine(s string) string { return sanitize(s, false) }

func needsSanitize(s string, keepNL bool) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b >= 0x80 || b == 0x7f || (b < 0x20 && (!keepNL || (b != '\t' && b != '\n'))) {
			return true
		}
	}
	return false
}

func isBidiControl(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}

func sanitize(s string, keepNL bool) string {
	if !needsSanitize(s, keepNL) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && n <= 1:
			b.WriteRune(utf8.RuneError) // invalid byte (e.g. a raw 8-bit C1 control)
		case r == 0x1b:
			i = skipEscape(s, i+1)
			continue
		case r == 0x9b: // C1 CSI
			i = skipCSI(s, i+n)
			continue
		case r == 0x90 || r == 0x98 || r == 0x9d || r == 0x9e || r == 0x9f: // C1 DCS, SOS, OSC, PM, APC
			i = skipControlString(s, i+n)
			continue
		case r == '\t' || r == '\n':
			if keepNL {
				b.WriteRune(r)
			} else {
				b.WriteByte(' ')
			}
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || isBidiControl(r):
			// dropped
		default:
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}

// skipEscape returns the index after the escape sequence whose ESC precedes i.
func skipEscape(s string, i int) int {
	if i >= len(s) {
		return i
	}
	switch c := s[i]; {
	case c == '[':
		return skipCSI(s, i+1)
	case c == ']' || c == 'P' || c == 'X' || c == '^' || c == '_':
		return skipControlString(s, i+1)
	case c >= 0x20 && c <= 0x7e:
		// ESC [intermediates] final, e.g. "ESC c" (reset) or "ESC ( B".
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
			i++
		}
		if i < len(s) && s[i] >= 0x30 && s[i] <= 0x7e {
			i++
		}
		return i
	}
	return i // lone ESC: drop it, keep what follows
}

// skipCSI skips parameter/intermediate bytes and the final byte of a CSI.
func skipCSI(s string, i int) int {
	for i < len(s) {
		c := s[i]
		switch {
		case c >= 0x20 && c <= 0x3f:
			i++
		case c >= 0x40 && c <= 0x7e:
			return i + 1
		default:
			return i // malformed: stop here, the rest is sanitized normally
		}
	}
	return i
}

// skipControlString skips an OSC/DCS/APC/PM/SOS payload up to its terminator
// (BEL, ESC \ or C1 ST). An unterminated string swallows the rest of s.
func skipControlString(s string, i int) int {
	for i < len(s) {
		switch {
		case s[i] == 0x07:
			return i + 1
		case s[i] == 0x1b:
			if i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
			return i // another sequence starts: let the caller handle it
		case strings.HasPrefix(s[i:], "\u009c"):
			return i + len("\u009c")
		}
		i++
	}
	return i
}

// ownColors are the only escape sequences the CLI itself writes into table
// cells and key/value rows (see output.go).
var ownColors = []string{reset, bold, dim, red, green, yellow, blue, cyan}

// cleanCell is cleanLine that keeps the CLI's own color codes, for cells that
// may already have been painted (c.status, c.paint). Untrusted text inside
// painted cells was cleaned by paint itself.
func cleanCell(s string) string {
	if !needsSanitize(s, false) {
		return s
	}
	var b strings.Builder
	for s != "" {
		i := strings.IndexByte(s, 0x1b)
		if i < 0 {
			b.WriteString(cleanLine(s))
			break
		}
		b.WriteString(cleanLine(s[:i]))
		s = s[i:]
		own := ""
		for _, code := range ownColors {
			if strings.HasPrefix(s, code) {
				own = code
				break
			}
		}
		if own != "" {
			b.WriteString(own)
			s = s[len(own):]
			continue
		}
		s = s[skipEscape(s, 1):]
	}
	return b.String()
}

// scrub cleans every string reachable from v (a pointer to a decoded API
// response) in place: struct fields, slices, maps and interface values.
func scrub(v any) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() {
		e := rv.Elem()
		if e.CanSet() {
			e.Set(scrubValue(e))
		}
	}
}

var rawJSONType = reflect.TypeOf([]byte(nil))

func scrubValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.String:
		s := v.String()
		if c := clean(s); c != s {
			nv := reflect.New(v.Type()).Elem()
			nv.SetString(c)
			return nv
		}
	case reflect.Pointer:
		if !v.IsNil() && v.Elem().CanSet() {
			v.Elem().Set(scrubValue(v.Elem()))
		}
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		inner := scrubValue(v.Elem())
		nv := reflect.New(v.Type()).Elem()
		nv.Set(inner)
		return nv
	case reflect.Struct:
		nv := v
		if !v.CanSet() {
			nv = reflect.New(v.Type()).Elem()
			nv.Set(v)
		}
		for i := 0; i < nv.NumField(); i++ {
			if f := nv.Field(i); f.CanSet() {
				f.Set(scrubValue(f))
			}
		}
		return nv
	case reflect.Slice:
		if v.Type().ConvertibleTo(rawJSONType) {
			return v // json.RawMessage / []byte: cleaned where rendered
		}
		for i := 0; i < v.Len(); i++ {
			el := v.Index(i)
			el.Set(scrubValue(el))
		}
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		for _, k := range v.MapKeys() {
			nk, nval := scrubValue(k), scrubValue(v.MapIndex(k))
			if nk.Interface() != k.Interface() {
				v.SetMapIndex(k, reflect.Value{})
			}
			v.SetMapIndex(nk, nval)
		}
	}
	return v
}

// safeJSON escapes characters that are valid inside JSON strings but act on a
// terminal (C1 controls, bidi overrides, invalid UTF-8) as \uXXXX, so --json
// output stays lossless and valid JSON yet cannot drive the terminal. Raw
// C0 controls cannot appear in valid JSON; callers only pass valid JSON.
func safeJSON(b []byte) []byte {
	unsafe := false
	for _, c := range b {
		if c >= 0x80 || c == 0x7f {
			unsafe = true
			break
		}
	}
	if !unsafe {
		return b
	}
	var out bytes.Buffer
	out.Grow(len(b))
	for i := 0; i < len(b); {
		r, n := utf8.DecodeRune(b[i:])
		switch {
		case r == utf8.RuneError && n <= 1:
			out.WriteString(`�`)
		case r == 0x7f || (r >= 0x80 && r <= 0x9f) || isBidiControl(r):
			fmt.Fprintf(&out, `\u%04x`, r)
		default:
			out.Write(b[i : i+n])
		}
		i += n
	}
	return out.Bytes()
}
