package metrics

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCounterAndHistogramText(t *testing.T) {
	c := NewCounterVec("reqs_total", "Requests.", "route", "status")
	c.Inc(`/a"b`, "200")
	c.Add(2, `/a"b`, "200")
	c.Inc("only-one-label") // wrong arity is ignored
	h := NewHistogramVec("lat_seconds", "Latency.", []float64{0.1, 1}, "route")
	h.Observe(0.05, "/x")
	h.Observe(0.5, "/x")
	h.Observe(3, "/x")
	var b bytes.Buffer
	c.Write(&b)
	h.Write(&b)
	WriteGauge(&b, "g", "Gauge.", Sample{Labels: []string{"status", "ok"}, Value: 2})
	WriteRuntime(&b, time.Unix(100, 0))
	out := b.String()
	for _, want := range []string{
		"# TYPE reqs_total counter",
		`reqs_total{route="/a\"b",status="200"} 3`,
		`lat_seconds_bucket{route="/x",le="0.1"} 1`,
		`lat_seconds_bucket{route="/x",le="1"} 2`,
		`lat_seconds_bucket{route="/x",le="+Inf"} 3`,
		`lat_seconds_sum{route="/x"} 3.55`,
		`lat_seconds_count{route="/x"} 3`,
		`g{status="ok"} 2`,
		"process_start_time_seconds 100",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "only-one-label") {
		t.Fatal("wrong-arity sample recorded")
	}
}

func TestCardinalityBound(t *testing.T) {
	c := NewCounterVec("x_total", "X.", "k")
	for i := range maxSeries + 50 {
		c.Inc(string(rune('a'+i%26)) + strings.Repeat("z", i))
	}
	if len(c.vals) != maxSeries {
		t.Fatalf("series %d, want cap %d", len(c.vals), maxSeries)
	}
}

func TestSentryDSNAndEnvelope(t *testing.T) {
	if s, err := NewSentry("", "v", ""); s != nil || err != nil {
		t.Fatal("empty DSN must disable")
	}
	for _, bad := range []string{"not a url", "ftp://k@h/1", "https://host/1", "https://k@host/"} {
		if _, err := NewSentry(bad, "v", ""); err == nil {
			t.Errorf("%q accepted", bad)
		} else if strings.Contains(err.Error(), "host") {
			t.Errorf("error leaks DSN: %v", err)
		}
	}
	s, err := NewSentry("https://pub:sec@o1.ingest.example.com/prefix/42", "1.2.3", "")
	if err != nil {
		t.Fatal(err)
	}
	if s.endpoint != "https://o1.ingest.example.com/prefix/api/42/envelope/" {
		t.Fatalf("endpoint %s", s.endpoint)
	}
	if !strings.Contains(s.auth, "sentry_key=pub") || !strings.Contains(s.auth, "sentry_secret=sec") {
		t.Fatalf("auth %s", s.auth)
	}
	pcs := make([]uintptr, 16)
	n := runtime.Callers(1, pcs)
	body, err := s.envelope(SentryEvent{Message: "panic: boom", Panic: true, Stack: pcs[:n], Tags: map[string]string{"route": "/v1/x"}})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 3 {
		t.Fatalf("envelope lines %d", len(lines))
	}
	var ev struct {
		Release     string            `json:"release"`
		Environment string            `json:"environment"`
		Tags        map[string]string `json:"tags"`
		Exception   struct {
			Values []struct {
				Value      string `json:"value"`
				Stacktrace struct {
					Frames []sentryFrame `json:"frames"`
				} `json:"stacktrace"`
			} `json:"values"`
		} `json:"exception"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &ev); err != nil {
		t.Fatal(err)
	}
	fr := ev.Exception.Values[0].Stacktrace.Frames
	if ev.Release != "1.2.3" || ev.Environment != "production" || ev.Tags["route"] != "/v1/x" ||
		ev.Exception.Values[0].Value != "panic: boom" || len(fr) == 0 ||
		!strings.Contains(fr[len(fr)-1].Function, "TestSentryDSNAndEnvelope") {
		t.Fatalf("event %+v", ev)
	}
	var nilSentry *Sentry
	nilSentry.Capture(SentryEvent{Message: "x"}) // disabled client is a no-op
}
