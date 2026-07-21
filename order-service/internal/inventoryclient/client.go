package inventoryclient

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	inventoryv1 "github.com/On0n0k1/go-event-platform/order-service/internal/inventoryv1"
)

var (
	ErrNotFound          = errors.New("item not found")
	ErrInsufficientStock = errors.New("insufficient stock")
)

type Item struct {
	SKU      string
	Name     string
	Quantity int
}

type Client struct {
	conn   *grpc.ClientConn
	client inventoryv1.InventoryServiceClient
}

func New(addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial inventory-service: %w", err)
	}

	return &Client{conn: conn, client: inventoryv1.NewInventoryServiceClient(conn)}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) Reserve(ctx context.Context, sku string, quantity int) (Item, error) {
	resp, err := c.client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
		Sku:      sku,
		Quantity: int32(quantity),
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
