package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"
)

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[01m" // all codes are 5 bytes so tabwriter columns stay aligned
	dim    = "\x1b[02m"
	red    = "\x1b[31m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	blue   = "\x1b[34m"
	cyan   = "\x1b[36m"
)

// paint colors s on a color terminal. s is cleaned first (it is often
// server-provided), so painted output never carries foreign escapes.
func (c *Ctx) paint(code, s string) string {
	s = clean(s)
	if !c.Color || s == "" {
		return s
	}
	return code + s + reset
}

func (c *Ctx) status(s string) string {
	switch s {
	case "pending":
		return c.paint(yellow, s)
	case "approved", "completed", "active", "succeeded":
		return c.paint(green, s)
	case "rejected", "failed", "degraded", "expired":
		return c.paint(red, s)
	case "processing":
		return c.paint(blue, s)
	}
	return c.paint(dim, s)
}

// printRaw pretty-prints a raw JSON response (for --json).
func (c *Ctx) printRaw(raw json.RawMessage) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		// Not JSON (e.g. a proxy error page): print it as untrusted text.
		_, err = fmt.Fprintln(c.Out, clean(string(raw)))
		return err
	}
	buf.WriteByte('\n')
	_, err := c.Out.Write(safeJSON(buf.Bytes()))
	return err
}

// printJSON encodes any value (for --json output the CLI itself builds).
func (c *Ctx) printJSON(v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	_, err := c.Out.Write(safeJSON(buf.Bytes()))
	return err
}

// printJSONLine writes v as one NDJSON line.
func (c *Ctx) printJSONLine(v any) {
	b, _ := json.Marshal(v)
	_, _ = c.Out.Write(append(safeJSON(b), '\n'))
}

func printJSONError(w io.Writer, err error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]any{"error": errorBody(err)})
	_, _ = w.Write(safeJSON(buf.Bytes()))
}

// errorBody is the JSON shape of a CLI error (mirrors the API error envelope).
func errorBody(err error) map[string]any {
	body := map[string]any{"message": friendlyError(err)}
	var ae *APIError
	var ne *NetError
	switch {
	case errors.As(err, &ae):
		body["status"] = ae.Status
		body["code"] = ae.Code
		body["message"] = ae.Message
		if len(ae.Fields) > 0 {
			body["fields"] = ae.Fields
		}
		if len(ae.Details) > 0 {
			body["details"] = ae.Details
		}
		if ae.RetryAfter > 0 {
			body["retry_after_seconds"] = int(ae.RetryAfter.Seconds())
		}
	case errors.As(err, &ne):
		body["code"] = "unreachable"
	default:
		body["code"] = "error"
	}
	return body
}

// table writes aligned columns; the header is bold on a color terminal.
type table struct {
	c    *Ctx
	tw   *tabwriter.Writer
	rows int
}

func (c *Ctx) table(headers ...string) *table {
	t := &table{c: c, tw: tabwriter.NewWriter(c.Out, 0, 0, 2, ' ', 0)}
	// All color codes have the same length, so colored cells stay aligned as
	// long as a column is colored on every row (see c.status).
	fmt.Fprintln(t.tw, strings.Join(headers, "\t"))
	return t
}

// row writes one row; cells are cleaned (keeping only the CLI's own colors).
func (t *table) row(cells ...string) {
	t.rows++
	for i, cell := range cells {
		cells[i] = cleanCell(cell)
	}
	fmt.Fprintln(t.tw, strings.Join(cells, "\t"))
}

func (t *table) flush(empty string) {
	_ = t.tw.Flush()
	if t.rows == 0 && empty != "" {
		fmt.Fprintln(t.c.Stderr, t.c.paint(dim, empty))
	}
}

// kv prints "key: value" lines aligned.
func (c *Ctx) kv(pairs ...string) {
	tw := tabwriter.NewWriter(c.Out, 0, 0, 2, ' ', 0)
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			continue
		}
		fmt.Fprintf(tw, "%s:\t%s\n", pairs[i], cleanCell(pairs[i+1]))
	}
	_ = tw.Flush()
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	if n <= 3 {
		return string(r[:n])
	}
	return string(r[:n-3]) + "..."
}

// fmtTime renders server timestamps in local time; unknown formats pass through.
func fmtTime(s string) string {
	if s == "" {
		return "-"
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Local().Format("2006-01-02 15:04")
		}
	}
	return s
}

func strPtr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// multiFlag is a repeatable string flag that also splits on commas when asked.
type multiFlag struct {
	vals  []string
	comma bool
}

func (m *multiFlag) String() string { return strings.Join(m.vals, ",") }
func (m *multiFlag) Set(v string) error {
	if m.comma {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				m.vals = append(m.vals, p)
			}
		}
		return nil
	}
	m.vals = append(m.vals, v)
	return nil
}

// parseJSONObject parses a flag value that must be a JSON object.
func parseJSONObject(flagName, s string) (map[string]any, error) {
	var m map[string]any
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil || m == nil {
		return nil, usageErrf("--%s must be a JSON object, e.g. '{\"key\":\"value\"}'", flagName)
	}
	return m, nil
}

// scalarOrJSON parses v as JSON when possible (numbers, booleans, arrays),
// otherwise keeps it as a string.
func scalarOrJSON(v string) any {
	var x any
	dec := json.NewDecoder(strings.NewReader(v))
	dec.UseNumber()
	if err := dec.Decode(&x); err == nil && !dec.More() {
		return x
	}
	return v
}
