package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/On0n0k1/go-event-platform/api-gateway/internal/config"
	"github.com/On0n0k1/go-event-platform/api-gateway/internal/httpx"
	"github.com/On0n0k1/go-event-platform/api-gateway/internal/proxy"
	"github.com/On0n0k1/go-event-platform/api-gateway/internal/tracing"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Init(ctx, "api-gateway", cfg.OTLPEndpoint)
	if err != nil {
		logger.Error("failed to initialize tracing", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(shutdownCtx); err != nil {
			logger.Error("failed to shut down tracing", "error", err)
		}
	}()

	orderProxy, err := proxy.NewReverseProxy(cfg.OrderServiceURL, logger)
	if err != nil {
		logger.Error("failed to configure order-service proxy", "error", err)
		os.Exit(1)
	}

	inventoryProxy, err := proxy.NewReverseProxy(cfg.InventoryServiceURL, logger)
	if err != nil {
		logger.Error("failed to configure inventory-service proxy", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httpx.HealthHandler)
	mux.Handle("POST /orders", orderProxy)
	mux.Handle("GET /orders/{id}", orderProxy)
	mux.Handle("GET /items/{sku}", inventoryProxy)

	tracedHandler := otelhttp.NewHandler(httpx.LoggingMiddleware(logger)(mux), "api-gateway",
		otelhttp.WithFilter(func(r *http.Request) bool {
			return r.URL.Path != "/healthz"
		}),
	)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: tracedHandler,
	}

	go func() {
		logger.Info("starting api-gateway",
			"port", cfg.Port,
			"order_service_url", cfg.OrderServiceURL,
			"inventory_service_url", cfg.InventoryServiceURL,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down api-gateway")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}
