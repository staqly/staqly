package metrics

import (
	"bytes"
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
