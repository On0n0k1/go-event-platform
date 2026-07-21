package order

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/On0n0k1/go-event-platform/order-service/internal/inventoryclient"
)

type stubStore struct {
	createOrderFunc func(ctx context.Context, o Order) error
	getOrderFunc    func(ctx context.Context, id string) (Order, error)
}

func (s *stubStore) CreateOrder(ctx context.Context, o Order) error {
	return s.createOrderFunc(ctx, o)
}

func (s *stubStore) GetOrder(ctx context.Context, id string) (Order, error) {
	return s.getOrderFunc(ctx, id)
}

type stubInventory struct {
	reserveFunc func(ctx context.Context, sku string, quantity int) (inventoryclient.Item, error)
}

func (s *stubInventory) Reserve(ctx context.Context, sku string, quantity int) (inventoryclient.Item, error) {
	return s.reserveFunc(ctx, sku, quantity)
}

func newTestHandler(store Store, inventory InventoryReserver) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHandler(store, inventory, logger)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func TestCreateOrder(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		reserve    func(ctx context.Context, sku string, quantity int) (inventoryclient.Item, error)
		createErr  error
		wantStatus int
	}{
		{
			name: "success",
			body: `{"sku":"SKU-001","quantity":5}`,
			reserve: func(ctx context.Context, sku string, quantity int) (inventoryclient.Item, error) {
				return inventoryclient.Item{SKU: sku, Name: "Widget", Quantity: 95}, nil
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "insufficient stock",
			body: `{"sku":"SKU-001","quantity":9999}`,
			reserve: func(ctx context.Context, sku string, quantity int) (inventoryclient.Item, error) {
				return inventoryclient.Item{}, inventoryclient.ErrInsufficientStock
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "item not found",
			body: `{"sku":"SKU-999","quantity":1}`,
			reserve: func(ctx context.Context, sku string, quantity int) (inventoryclient.Item, error) {
				return inventoryclient.Item{}, inventoryclient.ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "inventory-service unreachable",
			body: `{"sku":"SKU-001","quantity":1}`,
			reserve: func(ctx context.Context, sku string, quantity int) (inventoryclient.Item, error) {
				return inventoryclient.Item{}, errors.New("connection refused")
			},
			wantStatus: http.StatusBadGateway,
		},
		{
			name: "store failure after successful reservation",
			body: `{"sku":"SKU-001","quantity":1}`,
			reserve: func(ctx context.Context, sku string, quantity int) (inventoryclient.Item, error) {
				return inventoryclient.Item{SKU: sku, Quantity: 99}, nil
			},
			createErr:  errors.New("db error"),
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "invalid body",
			body:       `not json`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing sku",
			body:       `{"quantity":1}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "non-positive quantity",
			body:       `{"sku":"SKU-001","quantity":0}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &stubStore{
				createOrderFunc: func(ctx context.Context, o Order) error {
					return tt.createErr
				},
			}
			inventory := &stubInventory{reserveFunc: tt.reserve}
			handler := newTestHandler(store, inventory)

			req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestGetOrder(t *testing.T) {
	tests := []struct {
		name       string
		getOrder   func(ctx context.Context, id string) (Order, error)
		wantStatus int
	}{
		{
			name: "found",
			getOrder: func(ctx context.Context, id string) (Order, error) {
				return Order{ID: id, SKU: "SKU-001", Quantity: 5, Status: StatusConfirmed}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found",
			getOrder: func(ctx context.Context, id string) (Order, error) {
				return Order{}, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &stubStore{getOrderFunc: tt.getOrder}
			handler := newTestHandler(store, &stubInventory{})

			req := httptest.NewRequest(http.MethodGet, "/orders/some-id", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}
