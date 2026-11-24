package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestApp() *app {
	return newApp(slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
}

func do(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestHello(t *testing.T) {
	rec := do(newTestApp().routes(), http.MethodGet, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	want := `{"message":"hello world","version":"test"}` + "\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestUnknownPathReturns404(t *testing.T) {
	rec := do(newTestApp().routes(), http.MethodGet, "/nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestWrongMethodReturns405(t *testing.T) {
	rec := do(newTestApp().routes(), http.MethodPost, "/")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHealthz(t *testing.T) {
	rec := do(newTestApp().routes(), http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestReadyzFailsWhileShuttingDown(t *testing.T) {
	a := newTestApp()
	h := a.routes()

	if rec := do(h, http.MethodGet, "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	a.ready.Store(false)
	if rec := do(h, http.MethodGet, "/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestMetricsCountRequests(t *testing.T) {
	h := newTestApp().routes()
	do(h, http.MethodGet, "/")
	do(h, http.MethodGet, "/")
	do(h, http.MethodGet, "/nope")

	rec := do(h, http.MethodGet, "/metrics")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()

	for _, want := range []string{
		`http_requests_total{code="200",method="GET",route="/"} 2`,
		`http_requests_total{code="404",method="GET",route="other"} 1`,
		`http_request_duration_seconds_count{route="/"} 2`,
		`http_request_duration_seconds_bucket{route="/",le="+Inf"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output is missing %s\n%s", want, body)
		}
	}
}
