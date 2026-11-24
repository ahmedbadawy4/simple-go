package main

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// durationBuckets are the upper bounds, in seconds, of the latency histogram.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

type requestKey struct {
	method string
	route  string
	code   int
}

// histogram keeps one count per bucket. The counts are not cumulative here;
// serve adds them up when it writes the output.
type histogram struct {
	buckets []uint64
	count   uint64
	sum     float64
}

// metrics collects request counts and latencies and writes them in the
// Prometheus text format (version 0.0.4). It avoids a client library so the
// service has no dependencies.
type metrics struct {
	mu        sync.Mutex
	requests  map[requestKey]uint64
	durations map[string]*histogram
}

func newMetrics() *metrics {
	return &metrics{
		requests:  map[requestKey]uint64{},
		durations: map[string]*histogram{},
	}
}

func (m *metrics) observe(method, route string, code int, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests[requestKey{method, route, code}]++

	h, ok := m.durations[route]
	if !ok {
		h = &histogram{buckets: make([]uint64, len(durationBuckets))}
		m.durations[route] = h
	}
	secs := d.Seconds()
	for i, upper := range durationBuckets {
		if secs <= upper {
			h.buckets[i]++
			break
		}
	}
	h.count++
	h.sum += secs
}

// serve handles GET /metrics.
func (m *metrics) serve(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	fmt.Fprintln(w, "# HELP http_requests_total Total HTTP requests by method, route and status code.")
	fmt.Fprintln(w, "# TYPE http_requests_total counter")
	keys := make([]requestKey, 0, len(m.requests))
	for k := range m.requests {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		return keys[i].code < keys[j].code
	})
	for _, k := range keys {
		fmt.Fprintf(w, "http_requests_total{code=\"%d\",method=%q,route=%q} %d\n",
			k.code, k.method, k.route, m.requests[k])
	}

	fmt.Fprintln(w, "# HELP http_request_duration_seconds HTTP request latency in seconds by route.")
	fmt.Fprintln(w, "# TYPE http_request_duration_seconds histogram")
	routes := make([]string, 0, len(m.durations))
	for route := range m.durations {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	for _, route := range routes {
		h := m.durations[route]
		var cumulative uint64
		for i, upper := range durationBuckets {
			cumulative += h.buckets[i]
			fmt.Fprintf(w, "http_request_duration_seconds_bucket{route=%q,le=%q} %d\n",
				route, strconv.FormatFloat(upper, 'g', -1, 64), cumulative)
		}
		fmt.Fprintf(w, "http_request_duration_seconds_bucket{route=%q,le=\"+Inf\"} %d\n", route, h.count)
		fmt.Fprintf(w, "http_request_duration_seconds_sum{route=%q} %s\n",
			route, strconv.FormatFloat(h.sum, 'g', -1, 64))
		fmt.Fprintf(w, "http_request_duration_seconds_count{route=%q} %d\n", route, h.count)
	}
}
