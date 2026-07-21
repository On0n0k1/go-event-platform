package proxy

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReverseProxyForwardsRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/items/SKU-001" {
			t.Errorf("upstream received path %q, want /items/SKU-001", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"sku":"SKU-001"}`))
	}))
	defer upstream.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	rp, err := NewReverseProxy(upstream.URL, logger)
	if err != nil {
		t.Fatalf("NewReverseProxy: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/items/SKU-001", nil)
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"sku":"SKU-001"}` {
		t.Errorf("body = %q, want passthrough from upstream", got)
	}
}

func TestReverseProxyReturnsBadGatewayOnUnreachableTarget(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	rp, err := NewReverseProxy("http://127.0.0.1:1", logger)
	if err != nil {
		t.Fatalf("NewReverseProxy: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/items/SKU-001", nil)
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestNewReverseProxyRejectsInvalidTarget(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := NewReverseProxy("://not-a-url", logger); err == nil {
		t.Fatal("expected error for invalid target url")
	}
}
