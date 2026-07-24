package inventoryclient

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	inventoryv1 "github.com/On0n0k1/go-event-platform/order-service/internal/inventoryv1"
)

type fakeInventoryServer struct {
	inventoryv1.UnimplementedInventoryServiceServer
	reserveFunc func(context.Context, *inventoryv1.ReserveStockRequest) (*inventoryv1.ReserveStockResponse, error)
}

func (f *fakeInventoryServer) ReserveStock(ctx context.Context, req *inventoryv1.ReserveStockRequest) (*inventoryv1.ReserveStockResponse, error) {
	return f.reserveFunc(ctx, req)
}

func newTestClient(t *testing.T, srv inventoryv1.InventoryServiceServer) *Client {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	inventoryv1.RegisterInventoryServiceServer(grpcServer, srv)
	go func() {
		_ = grpcServer.Serve(lis)
	}()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Client{conn: conn, client: inventoryv1.NewInventoryServiceClient(conn), log: log}
}

// useFastRetryConfig shrinks reserveRetryConfig's delays for the duration of
// a test, so retry-triggering test cases don't sit through the production
// backoff (up to ~1s per attempt).
func useFastRetryConfig(t *testing.T) {
	t.Helper()

	orig := reserveRetryConfig
	reserveRetryConfig.BaseDelay = 1 * time.Millisecond
	reserveRetryConfig.MaxDelay = 5 * time.Millisecond
	t.Cleanup(func() { reserveRetryConfig = orig })
}

func TestClientReserve(t *testing.T) {
	tests := []struct {
		name        string
		reserveFunc func(context.Context, *inventoryv1.ReserveStockRequest) (*inventoryv1.ReserveStockResponse, error)
		wantErr     error
		wantItem    Item
	}{
		{
			name: "success",
			reserveFunc: func(_ context.Context, req *inventoryv1.ReserveStockRequest) (*inventoryv1.ReserveStockResponse, error) {
				return &inventoryv1.ReserveStockResponse{Sku: req.GetSku(), Name: "Widget", Quantity: 95}, nil
			},
			wantItem: Item{SKU: "SKU-001", Name: "Widget", Quantity: 95},
		},
		{
			name: "not found",
			reserveFunc: func(context.Context, *inventoryv1.ReserveStockRequest) (*inventoryv1.ReserveStockResponse, error) {
				return nil, status.Error(codes.NotFound, "item not found")
			},
			wantErr: ErrNotFound,
		},
		{
			name: "insufficient stock",
			reserveFunc: func(context.Context, *inventoryv1.ReserveStockRequest) (*inventoryv1.ReserveStockResponse, error) {
				return nil, status.Error(codes.FailedPrecondition, "insufficient stock")
			},
			wantErr: ErrInsufficientStock,
		},
		{
			name: "unexpected error",
			reserveFunc: func(context.Context, *inventoryv1.ReserveStockRequest) (*inventoryv1.ReserveStockResponse, error) {
				return nil, status.Error(codes.Internal, "boom")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, &fakeInventoryServer{reserveFunc: tt.reserveFunc})

			item, err := client.Reserve(context.Background(), "SKU-001", 5)

			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
			case tt.name == "unexpected error":
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if item != tt.wantItem {
					t.Fatalf("item = %+v, want %+v", item, tt.wantItem)
				}
			}
		})
	}
}

func TestClientReserveRetriesOnUnavailableThenSucceeds(t *testing.T) {
	useFastRetryConfig(t)

	var calls atomic.Int32
	srv := &fakeInventoryServer{
		reserveFunc: func(_ context.Context, req *inventoryv1.ReserveStockRequest) (*inventoryv1.ReserveStockResponse, error) {
			n := calls.Add(1)
			if n < 3 {
				return nil, status.Error(codes.Unavailable, "connection refused")
			}
			return &inventoryv1.ReserveStockResponse{Sku: req.GetSku(), Name: "Widget", Quantity: 90}, nil
		},
	}
	client := newTestClient(t, srv)

	item, err := client.Reserve(context.Background(), "SKU-001", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.Quantity != 90 {
		t.Fatalf("quantity = %d, want 90", item.Quantity)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3 (2 failures + 1 success)", got)
	}
}

func TestClientReserveExhaustsRetriesOnPersistentUnavailable(t *testing.T) {
	useFastRetryConfig(t)

	var calls atomic.Int32
	srv := &fakeInventoryServer{
		reserveFunc: func(context.Context, *inventoryv1.ReserveStockRequest) (*inventoryv1.ReserveStockResponse, error) {
			calls.Add(1)
			return nil, status.Error(codes.Unavailable, "connection refused")
		},
	}
	client := newTestClient(t, srv)

	_, err := client.Reserve(context.Background(), "SKU-001", 10)
	if err == nil {
		t.Fatal("expected an error after exhausting retries, got nil")
	}
	if got := calls.Load(); int(got) != reserveRetryConfig.MaxAttempts {
		t.Fatalf("calls = %d, want %d (MaxAttempts)", got, reserveRetryConfig.MaxAttempts)
	}
}

func TestClientReserveDoesNotRetryNonUnavailableErrors(t *testing.T) {
	useFastRetryConfig(t)

	var calls atomic.Int32
	srv := &fakeInventoryServer{
		reserveFunc: func(context.Context, *inventoryv1.ReserveStockRequest) (*inventoryv1.ReserveStockResponse, error) {
			calls.Add(1)
			return nil, status.Error(codes.FailedPrecondition, "insufficient stock")
		},
	}
	client := newTestClient(t, srv)

	_, err := client.Reserve(context.Background(), "SKU-001", 10)
	if !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("error = %v, want ErrInsufficientStock", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 (business errors must not be retried -- ReserveStock isn't idempotent)", got)
	}
}
