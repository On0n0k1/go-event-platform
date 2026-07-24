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

	"github.com/On0n0k1/go-event-platform/order-service/internal/config"
	"github.com/On0n0k1/go-event-platform/order-service/internal/db"
	"github.com/On0n0k1/go-event-platform/order-service/internal/events"
	"github.com/On0n0k1/go-event-platform/order-service/internal/httpx"
	"github.com/On0n0k1/go-event-platform/order-service/internal/inventoryclient"
	"github.com/On0n0k1/go-event-platform/order-service/internal/metrics"
	"github.com/On0n0k1/go-event-platform/order-service/internal/order"
	"github.com/On0n0k1/go-event-platform/order-service/internal/outbox"
	"github.com/On0n0k1/go-event-platform/order-service/internal/tracing"
)

const outboxPollInterval = 500 * time.Millisecond

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Init(ctx, "order-service", cfg.OTLPEndpoint)
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

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.ApplySchema(ctx, pool); err != nil {
		logger.Error("failed to apply schema", "error", err)
		os.Exit(1)
	}

	publisher, err := events.NewPublisher(ctx, cfg.NatsURL)
	if err != nil {
		logger.Error("failed to connect to nats", "error", err)
		os.Exit(1)
	}
	defer publisher.Close()

	inventory, err := inventoryclient.New(cfg.InventoryServiceGRPCAddr, logger)
	if err != nil {
		logger.Error("failed to create inventory-service client", "error", err)
		os.Exit(1)
	}
	defer inventory.Close()

	outboxStore := outbox.NewStore(pool)
	relay := outbox.NewRelay(outboxStore, publisher, logger)
	relayDone := make(chan struct{})
	go func() {
		defer close(relayDone)
		relay.Run(ctx, outboxPollInterval)
	}()

	store := order.NewPostgresStore(pool)
	handler := order.NewHandler(store, inventory, logger)

	mux := http.NewServeMux()
	handler.Register(mux)
	mux.HandleFunc("GET /healthz", httpx.HealthHandler(pool))
	mux.Handle("GET /metrics", metrics.Handler())

	tracedHandler := otelhttp.NewHandler(httpx.LoggingMiddleware(logger)(metrics.HTTPMiddleware(mux)), "order-service",
		otelhttp.WithFilter(func(r *http.Request) bool {
			return r.URL.Path != "/healthz" && r.URL.Path != "/metrics"
		}),
	)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: tracedHandler,
	}

	go func() {
		logger.Info("starting order-service", "port", cfg.Port, "inventory_service_grpc_addr", cfg.InventoryServiceGRPCAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down order-service")

	// Let the relay finish its current poll before the deferred
	// publisher/pool.Close() calls run underneath it.
	<-relayDone

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}
