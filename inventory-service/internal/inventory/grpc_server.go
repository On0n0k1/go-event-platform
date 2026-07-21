package inventory

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	inventoryv1 "github.com/On0n0k1/go-event-platform/inventory-service/internal/inventoryv1"
)

// GRPCServer adapts Store to the InventoryService gRPC contract. It is a
// second transport (alongside the REST handler) over the same business
// logic -- no duplicated reservation logic.
type GRPCServer struct {
	inventoryv1.UnimplementedInventoryServiceServer

	store Store
	log   *slog.Logger
}

func NewGRPCServer(store Store, log *slog.Logger) *GRPCServer {
	return &GRPCServer{store: store, log: log}
}

func (s *GRPCServer) ReserveStock(ctx context.Context, req *inventoryv1.ReserveStockRequest) (*inventoryv1.ReserveStockResponse, error) {
	if req.GetSku() == "" || req.GetQuantity() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "sku is required and quantity must be positive")
	}

	item, err := s.store.ReserveStock(ctx, req.GetSku(), int(req.GetQuantity()))
	switch {
	case errors.Is(err, ErrNotFound):
		return nil, status.Error(codes.NotFound, "item not found")
	case errors.Is(err, ErrInsufficientStock):
		return nil, status.Error(codes.FailedPrecondition, "insufficient stock")
	case err != nil:
		s.log.Error("reserve stock failed", "sku", req.GetSku(), "error", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &inventoryv1.ReserveStockResponse{
		Sku:      item.SKU,
		Name:     item.Name,
		Quantity: int32(item.Quantity),
	}, nil
}
