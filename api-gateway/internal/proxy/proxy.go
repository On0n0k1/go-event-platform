package proxy

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/On0n0k1/go-event-platform/api-gateway/internal/httpx"
)

func NewReverseProxy(target string, logger *slog.Logger) (*httputil.ReverseProxy, error) {
	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("parse target url %q: %w", target, err)
	}

	rp := httputil.NewSingleHostReverseProxy(targetURL)
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error("proxy error", "target", target, "path", r.URL.Path, "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "upstream service unavailable")
	}

	return rp, nil
}
