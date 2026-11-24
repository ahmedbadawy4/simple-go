package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// app holds the state shared by the HTTP handlers.
type app struct {
	logger  *slog.Logger
	metrics *metrics
	version string
	ready   atomic.Bool
}

func newApp(logger *slog.Logger, version string) *app {
	a := &app{logger: logger, metrics: newMetrics(), version: version}
	a.ready.Store(true)
	return a
}

// routes returns the handler for all routes, wrapped with request metrics and
// request logs.
func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.hello)
	mux.HandleFunc("GET /healthz", a.healthz)
	mux.HandleFunc("GET /readyz", a.readyz)
	mux.HandleFunc("GET /metrics", a.metrics.serve)
	return a.instrument(mux)
}

func (a *app) hello(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"message": "hello world", "version": a.version})
}

// healthz is the liveness probe: the process is running.
func (a *app) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz is the readiness probe. It fails while the service shuts down.
func (a *app) readyz(w http.ResponseWriter, r *http.Request) {
	if !a.ready.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "shutting down"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// instrument records a request counter and a latency histogram for every
// request. It logs requests that are not probes or metric scrapes.
func (a *app) instrument(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := mux.Handler(r)
		route := routeLabel(pattern)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		start := time.Now()
		mux.ServeHTTP(rec, r)
		elapsed := time.Since(start)

		a.metrics.observe(methodLabel(r.Method), route, rec.status, elapsed)
		if route == "/" || route == "other" {
			a.logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", elapsed.Milliseconds(),
			)
		}
	})
}

// statusRecorder remembers the status code written by a handler.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// routeLabel turns a ServeMux pattern such as "GET /healthz" into a metric
// label. Requests that match no route share the label "other", so clients
// cannot create new label values.
func routeLabel(pattern string) string {
	if i := strings.IndexByte(pattern, ' '); i >= 0 {
		pattern = pattern[i+1:]
	}
	switch pattern {
	case "":
		return "other"
	case "/{$}":
		return "/"
	default:
		return pattern
	}
}

// methodLabel limits the method label to a fixed set of values.
func methodLabel(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}
