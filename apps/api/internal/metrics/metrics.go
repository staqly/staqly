// Package metrics is a tiny, dependency-free Prometheus text-format registry
// (counters, histograms, scrape-time gauges).
//
// Labels must only carry enum-like values (route patterns, methods, status
// codes, statuses). Never put IDs, URLs, keys or user input in a label.
package metrics

import (
	"fmt"
	"io"
	"math"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxSeries bounds the number of label combinations per vector so a bug that
// leaks a high-cardinality value into a label cannot exhaust memory.
const maxSeries = 2000

// Sample is one gauge value with its label pairs ("k1", "v1", "k2", "v2", ...).
type Sample struct {
	Labels []string
	Value  float64
}

func key(lv []string) string { return strings.Join(lv, "\xff") }

func escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}

func labelSet(names, values []string, extra ...string) string {
	if len(names) == 0 && len(extra) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	n := 0
	add := func(k, v string) {
		if n > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(escape(v))
		b.WriteByte('"')
		n++
	}
	for i, k := range names {
		add(k, values[i])
	}
	for i := 0; i+1 < len(extra); i += 2 {
		add(extra[i], extra[i+1])
	}
	b.WriteByte('}')
	return b.String()
}

func num(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func header(w io.Writer, name, help, typ string) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

// ---- counters ----

type CounterVec struct {
	name, help string
	labels     []string
	mu         sync.Mutex
	vals       map[string]*counterEntry
}

type counterEntry struct {
	lv []string
	v  float64
}

func NewCounterVec(name, help string, labels ...string) *CounterVec {
	return &CounterVec{name: name, help: help, labels: labels, vals: map[string]*counterEntry{}}
}

// Inc adds 1 to the series with the given label values (in label order).
func (c *CounterVec) Inc(lv ...string) { c.Add(1, lv...) }

func (c *CounterVec) Add(v float64, lv ...string) {
	if len(lv) != len(c.labels) {
		return
	}
	k := key(lv)
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.vals[k]
	if !ok {
		if len(c.vals) >= maxSeries {
			return
		}
		e = &counterEntry{lv: append([]string(nil), lv...)}
		c.vals[k] = e
	}
	e.v += v
}

// Value returns the current value of one series (for tests).
func (c *CounterVec) Value(lv ...string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.vals[key(lv)]; ok {
		return e.v
	}
	return 0
}

func (c *CounterVec) Write(w io.Writer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	header(w, c.name, c.help, "counter")
	for _, k := range sortedKeys(c.vals) {
		e := c.vals[k]
		fmt.Fprintf(w, "%s%s %s\n", c.name, labelSet(c.labels, e.lv), num(e.v))
	}
}

// ---- histograms ----

// DefBuckets suits HTTP latencies in seconds.
var DefBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type HistogramVec struct {
	name, help string
	labels     []string
	buckets    []float64
	mu         sync.Mutex
	vals       map[string]*histEntry
}

type histEntry struct {
	lv     []string
	counts []uint64 // per bucket, non-cumulative; last slot is +Inf
	sum    float64
	count  uint64
}

func NewHistogramVec(name, help string, buckets []float64, labels ...string) *HistogramVec {
	b := append([]float64(nil), buckets...)
	sort.Float64s(b)
	return &HistogramVec{name: name, help: help, labels: labels, buckets: b, vals: map[string]*histEntry{}}
}

func (h *HistogramVec) Observe(v float64, lv ...string) {
	if len(lv) != len(h.labels) {
		return
	}
	k := key(lv)
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.vals[k]
	if !ok {
		if len(h.vals) >= maxSeries {
			return
		}
		e = &histEntry{lv: append([]string(nil), lv...), counts: make([]uint64, len(h.buckets)+1)}
		h.vals[k] = e
	}
	i := sort.SearchFloat64s(h.buckets, v) // first bucket with bound >= v
	e.counts[i]++
	e.sum += v
	e.count++
}

func (h *HistogramVec) Write(w io.Writer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	header(w, h.name, h.help, "histogram")
	for _, k := range sortedKeys(h.vals) {
		e := h.vals[k]
		var cum uint64
		for i, ub := range h.buckets {
			cum += e.counts[i]
			fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, labelSet(h.labels, e.lv, "le", num(ub)), cum)
		}
		fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, labelSet(h.labels, e.lv, "le", "+Inf"), e.count)
		fmt.Fprintf(w, "%s_sum%s %s\n", h.name, labelSet(h.labels, e.lv), num(e.sum))
		fmt.Fprintf(w, "%s_count%s %d\n", h.name, labelSet(h.labels, e.lv), e.count)
	}
}

// ---- scrape-time values ----

// WriteGauge writes a gauge family computed at scrape time.
func WriteGauge(w io.Writer, name, help string, samples ...Sample) {
	writeFamily(w, name, help, "gauge", samples)
}

// WriteCounter writes a counter family computed at scrape time (e.g. GC cycles).
func WriteCounter(w io.Writer, name, help string, samples ...Sample) {
	writeFamily(w, name, help, "counter", samples)
}

func writeFamily(w io.Writer, name, help, typ string, samples []Sample) {
	header(w, name, help, typ)
	for _, s := range samples {
		fmt.Fprintf(w, "%s%s %s\n", name, labelSet(nil, nil, s.Labels...), num(s.Value))
	}
}

// WriteRuntime writes process and Go runtime basics.
func WriteRuntime(w io.Writer, start time.Time) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	WriteGauge(w, "process_start_time_seconds", "Start time of the process since unix epoch in seconds.",
		Sample{Value: float64(start.UnixNano()) / 1e9})
	WriteGauge(w, "go_info", "Information about the Go environment.",
		Sample{Labels: []string{"version", runtime.Version()}, Value: 1})
	WriteGauge(w, "go_goroutines", "Number of goroutines that currently exist.", Sample{Value: float64(runtime.NumGoroutine())})
	WriteGauge(w, "go_memstats_alloc_bytes", "Bytes of allocated heap objects.", Sample{Value: float64(ms.HeapAlloc)})
	WriteGauge(w, "go_memstats_heap_inuse_bytes", "Bytes in in-use heap spans.", Sample{Value: float64(ms.HeapInuse)})
	WriteGauge(w, "go_memstats_sys_bytes", "Bytes of memory obtained from the OS.", Sample{Value: float64(ms.Sys)})
	WriteCounter(w, "go_memstats_mallocs_total", "Cumulative count of heap objects allocated.", Sample{Value: float64(ms.Mallocs)})
	WriteCounter(w, "go_gc_cycles_total", "Number of completed GC cycles.", Sample{Value: float64(ms.NumGC)})
	WriteGauge(w, "go_gc_pause_last_seconds", "Duration of the most recent GC stop-the-world pause.",
		Sample{Value: float64(ms.PauseNs[(ms.NumGC+255)%256]) / 1e9})
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
