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

	"github.com/On0n0k1/go-event-platform/analytics-service/internal/analytics"
	"github.com/On0n0k1/go-event-platform/analytics-service/internal/config"
	"github.com/On0n0k1/go-event-platform/analytics-service/internal/events"
	"github.com/On0n0k1/go-event-platform/analytics-service/internal/httpx"
)

const durableConsumerName = "analytics-service"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	subscriber, err := events.Connect(cfg.NatsURL)
	if err != nil {
		logger.Error("failed to connect to nats", "error", err)
		os.Exit(1)
	}
	defer subscriber.Close()

	stats := &analytics.Stats{}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httpx.HealthHandler(subscriber.Conn()))
	mux.HandleFunc("GET /stats", analytics.StatsHandler(stats))

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: httpx.LoggingMiddleware(logger)(mux),
	}

	go func() {
		logger.Info("starting analytics-service", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			stop()
		}
	}()

	go func() {
		logger.Info("subscribing to order events", "subject", events.SubjectCreated, "durable", durableConsumerName)
		if err := subscriber.Consume(ctx, durableConsumerName, logger, handleOrderCreated(logger, stats)); err != nil {
			logger.Error("event consumer stopped with error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down analytics-service")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}

func handleOrderCreated(logger *slog.Logger, stats *analytics.Stats) func(context.Context, events.OrderCreated) error {
	return func(_ context.Context, evt events.OrderCreated) error {
		stats.Record(evt.Quantity)
		logger.Info("order recorded", "order_id", evt.OrderID, "sku", evt.SKU, "quantity", evt.Quantity)
		return nil
	}
}
