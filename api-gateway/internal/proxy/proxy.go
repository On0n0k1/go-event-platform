package proxy

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/On0n0k1/go-event-platform/api-gateway/internal/httpx"
)

func NewReverseProxy(target string, logger *slog.Logger) (*httputil.ReverseProxy, error) {
	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("parse target url %q: %w", target, err)
	}

	rp := httputil.NewSingleHostReverseProxy(targetURL)
	// otelhttp.NewTransport starts a client span (as a child of the request's
	// context) and injects fresh trace-context headers into the forwarded
	// request, so the downstream service continues the same trace rather than
	// just passing through whatever headers the original caller happened to send.
	rp.Transport = otelhttp.NewTransport(http.DefaultTransport)
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error("proxy error", "target", target, "path", r.URL.Path, "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "upstream service unavailable")
	}

	return rp, nil
}
