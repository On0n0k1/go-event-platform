package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/On0n0k1/go-event-platform/inventory-service/internal/cache"
	"github.com/On0n0k1/go-event-platform/inventory-service/internal/config"
	"github.com/On0n0k1/go-event-platform/inventory-service/internal/db"
	"github.com/On0n0k1/go-event-platform/inventory-service/internal/httpx"
	"github.com/On0n0k1/go-event-platform/inventory-service/internal/inventory"
	inventoryv1 "github.com/On0n0k1/go-event-platform/inventory-service/internal/inventoryv1"
	"github.com/On0n0k1/go-event-platform/inventory-service/internal/metrics"
	"github.com/On0n0k1/go-event-platform/inventory-service/internal/tlsconfig"
	"github.com/On0n0k1/go-event-platform/inventory-service/internal/tracing"
)

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

	shutdownTracing, err := tracing.Init(ctx, "inventory-service", cfg.OTLPEndpoint)
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

	redisClient, err := cache.NewClient(ctx, cfg.RedisAddr)
	if err != nil {
		logger.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	store := inventory.NewCachingStore(inventory.NewPostgresStore(pool), redisClient)

	handler := inventory.NewHandler(store, logger)
	mux := http.NewServeMux()
	handler.Register(mux)
	mux.HandleFunc("GET /healthz", httpx.HealthHandler(pool))
	mux.Handle("GET /metrics", metrics.Handler())

	tracedHandler := otelhttp.NewHandler(httpx.LoggingMiddleware(logger)(metrics.HTTPMiddleware(mux)), "inventory-service",
		otelhttp.WithFilter(func(r *http.Request) bool {
			return r.URL.Path != "/healthz" && r.URL.Path != "/metrics"
		}),
	)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: tracedHandler,
	}

	serverTLSConfig, err := tlsconfig.Server(cfg.TLSCertFile, cfg.TLSKeyFile, cfg.TLSCAFile)
	if err != nil {
		logger.Error("failed to load TLS config", "error", err)
		os.Exit(1)
	}

	grpcServer := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(serverTLSConfig)),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.UnaryInterceptor(metrics.UnaryServerInterceptor()),
	)
	inventoryv1.RegisterInventoryServiceServer(grpcServer, inventory.NewGRPCServer(store, logger))

	grpcListener, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		logger.Error("failed to listen for grpc", "error", err)
		os.Exit(1)
	}

	go func() {
		logger.Info("starting inventory-service", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			stop()
		}
	}()

	go func() {
		logger.Info("starting inventory-service grpc server", "port", cfg.GRPCPort)
		if err := grpcServer.Serve(grpcListener); err != nil {
			logger.Error("grpc server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down inventory-service")

	grpcServer.GracefulStop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}
