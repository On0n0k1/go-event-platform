package inventoryclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	inventoryv1 "github.com/On0n0k1/go-event-platform/order-service/internal/inventoryv1"
	"github.com/On0n0k1/go-event-platform/order-service/internal/metrics"
	"github.com/On0n0k1/go-event-platform/order-service/internal/retry"
)

var (
	ErrNotFound          = errors.New("item not found")
	ErrInsufficientStock = errors.New("insufficient stock")
)

const reserveStockMethod = "ReserveStock"

// reserveRetryConfig is sized to plausibly bridge a brief restart of
// inventory-service (a redeploy, a crash-restart) rather than just a
// sub-second network blip: with equal jitter, the wait before the final
// attempt ranges roughly 3-6s. That's a deliberate latency-for-resilience
// trade-off on a synchronous, user-facing request -- bounded, so it never
// hangs indefinitely, but long enough to actually matter.
var reserveRetryConfig = retry.Config{
	MaxAttempts: 6,
	BaseDelay:   300 * time.Millisecond,
	MaxDelay:    2 * time.Second,
}

type Item struct {
	SKU      string
	Name     string
	Quantity int
}

type Client struct {
	conn   *grpc.ClientConn
	client inventoryv1.InventoryServiceClient
	log    *slog.Logger
}

func New(addr string, log *slog.Logger, tlsConfig *tls.Config) (*Client, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithUnaryInterceptor(metrics.UnaryClientInterceptor()),
		// Bounds a single dial attempt so an unreachable/hanging peer surfaces
		// as codes.Unavailable (and gets retried) within a known ceiling,
		// instead of waiting on the OS's own TCP connect timeout -- which is
		// what turned a down inventory-service into a 25s hang before this
		// was added.
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff:           backoff.DefaultConfig,
			MinConnectTimeout: 2 * time.Second,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("dial inventory-service: %w", err)
	}

	return &Client{conn: conn, client: inventoryv1.NewInventoryServiceClient(conn), log: log}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

// isRetryableStatus only retries codes.Unavailable -- ReserveStock mutates
// state (it isn't idempotent), so retrying is only safe when we're confident
// the request never reached the server. Unavailable fires on connection-level
// failures (refused/reset, server not listening); anything else (including
// DeadlineExceeded, where the server may have already processed the request)
// is left alone.
func isRetryableStatus(err error) bool {
	return status.Code(err) == codes.Unavailable
}

func (c *Client) Reserve(ctx context.Context, sku string, quantity int) (Item, error) {
	cfg := reserveRetryConfig
	cfg.OnRetry = func(attempt int, delay time.Duration, retryErr error) {
		c.log.Warn("retrying inventory-service call",
			"method", reserveStockMethod,
			"attempt", attempt,
			"delay_ms", delay.Milliseconds(),
			"error", retryErr,
		)
		metrics.RecordGRPCClientRetry(reserveStockMethod)
		trace.SpanFromContext(ctx).AddEvent("retry", trace.WithAttributes(
			attribute.String("grpc.method", reserveStockMethod),
			attribute.Int("attempt", attempt),
		))
	}

	var resp *inventoryv1.ReserveStockResponse
	err := retry.Do(ctx, cfg, isRetryableStatus, func() error {
		var rpcErr error
		resp, rpcErr = c.client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
			Sku:      sku,
			Quantity: int32(quantity),
		})
		return rpcErr
	})

	if err != nil {
		switch status.Code(err) {
		case codes.NotFound:
			return Item{}, ErrNotFound
		case codes.FailedPrecondition:
			return Item{}, ErrInsufficientStock
		default:
			return Item{}, fmt.Errorf("call inventory-service: %w", err)
		}
	}

	return Item{
		SKU:      resp.GetSku(),
		Name:     resp.GetName(),
		Quantity: int(resp.GetQuantity()),
	}, nil
}
