// Package metrics is a small dependency-free registry that renders Prometheus
// text exposition format. It deliberately avoids a client library so the API
// server keeps the standard-library-only dependency policy.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// durationBuckets are the histogram upper bounds in seconds.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type seriesKey struct {
	method string
	route  string
	status int
}

type series struct {
	count   uint64
	sum     float64
	buckets []uint64
}

// Registry accumulates HTTP request counters and duration histograms.
type Registry struct {
	mu       sync.Mutex
	requests map[seriesKey]*series
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{requests: make(map[seriesKey]*series)}
}

// Observe records one completed request.
func (r *Registry) Observe(method, route string, status int, duration time.Duration) {
	if r == nil {
		return
	}
	if route == "" {
		route = "unmatched"
	}

	key := seriesKey{method: method, route: route, status: status}
	seconds := duration.Seconds()

	r.mu.Lock()
	defer r.mu.Unlock()

	item, ok := r.requests[key]
	if !ok {
		item = &series{buckets: make([]uint64, len(durationBuckets))}
		r.requests[key] = item
	}
	item.count++
	item.sum += seconds
	for i, bound := range durationBuckets {
		if seconds <= bound {
			item.buckets[i]++
		}
	}
}

// WritePrometheus renders the registry in Prometheus text exposition format.
func (r *Registry) WritePrometheus(w io.Writer) error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	keys := make([]seriesKey, 0, len(r.requests))
	snapshot := make(map[seriesKey]series, len(r.requests))
	for key, item := range r.requests {
		keys = append(keys, key)
		snapshot[key] = series{
			count:   item.count,
			sum:     item.sum,
			buckets: append([]uint64(nil), item.buckets...),
		}
	}
	r.mu.Unlock()

	sort.Slice(keys, func(i, j int) bool {
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		return keys[i].status < keys[j].status
	})

	var b strings.Builder
	b.WriteString("# HELP http_requests_total Total HTTP requests handled.\n")
	b.WriteString("# TYPE http_requests_total counter\n")
	for _, key := range keys {
		fmt.Fprintf(&b, "http_requests_total{method=%s,route=%s,status=%s} %d\n",
			quoteLabel(key.method), quoteLabel(key.route), quoteLabel(fmt.Sprintf("%d", key.status)), snapshot[key].count)
	}

	b.WriteString("# HELP http_request_duration_seconds HTTP request duration.\n")
	b.WriteString("# TYPE http_request_duration_seconds histogram\n")
	for _, key := range keys {
		item := snapshot[key]
		labels := fmt.Sprintf("method=%s,route=%s,status=%s",
			quoteLabel(key.method), quoteLabel(key.route), quoteLabel(fmt.Sprintf("%d", key.status)))
		for i, bound := range durationBuckets {
			fmt.Fprintf(&b, "http_request_duration_seconds_bucket{%s,le=%s} %d\n",
				labels, quoteLabel(formatBound(bound)), item.buckets[i])
		}
		fmt.Fprintf(&b, "http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n", labels, item.count)
		fmt.Fprintf(&b, "http_request_duration_seconds_sum{%s} %g\n", labels, item.sum)
		fmt.Fprintf(&b, "http_request_duration_seconds_count{%s} %d\n", labels, item.count)
	}

	_, err := io.WriteString(w, b.String())
	return err
}

func formatBound(bound float64) string {
	return fmt.Sprintf("%g", bound)
}

func quoteLabel(value string) string {
	return `"` + escapeLabel(value) + `"`
}

func escapeLabel(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return replacer.Replace(value)
}
