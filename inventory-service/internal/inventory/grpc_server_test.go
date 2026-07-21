package inventory

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	inventoryv1 "github.com/On0n0k1/go-event-platform/inventory-service/internal/inventoryv1"
)

func TestGRPCServerReserveStock(t *testing.T) {
	tests := []struct {
		name         string
		req          *inventoryv1.ReserveStockRequest
		reserveStock func(ctx context.Context, sku string, quantity int) (Item, error)
		wantCode     codes.Code
	}{
		{
			name: "success",
			req:  &inventoryv1.ReserveStockRequest{Sku: "SKU-001", Quantity: 5},
			reserveStock: func(ctx context.Context, sku string, quantity int) (Item, error) {
				return Item{SKU: sku, Name: "Widget", Quantity: 95}, nil
			},
			wantCode: codes.OK,
		},
		{
			name: "insufficient stock",
			req:  &inventoryv1.ReserveStockRequest{Sku: "SKU-001", Quantity: 1000},
			reserveStock: func(ctx context.Context, sku string, quantity int) (Item, error) {
				return Item{}, ErrInsufficientStock
			},
			wantCode: codes.FailedPrecondition,
		},
		{
			name: "item not found",
			req:  &inventoryv1.ReserveStockRequest{Sku: "SKU-999", Quantity: 1},
			reserveStock: func(ctx context.Context, sku string, quantity int) (Item, error) {
				return Item{}, ErrNotFound
			},
			wantCode: codes.NotFound,
		},
		{
			name:     "invalid argument",
			req:      &inventoryv1.ReserveStockRequest{Sku: "SKU-001", Quantity: 0},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			srv := NewGRPCServer(&stubStore{reserveStockFunc: tt.reserveStock}, logger)

			resp, err := srv.ReserveStock(context.Background(), tt.req)

			if tt.wantCode == codes.OK {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if resp.GetSku() != tt.req.GetSku() {
					t.Errorf("response sku = %q, want %q", resp.GetSku(), tt.req.GetSku())
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error with code %s, got nil", tt.wantCode)
			}
			if got := status.Code(err); got != tt.wantCode {
				t.Fatalf("error code = %s, want %s", got, tt.wantCode)
			}
		})
	}
}
