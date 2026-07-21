package inventory

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubStore struct {
	getItemFunc      func(ctx context.Context, sku string) (Item, error)
	reserveStockFunc func(ctx context.Context, sku string, quantity int) (Item, error)
}

func (s *stubStore) GetItem(ctx context.Context, sku string) (Item, error) {
	return s.getItemFunc(ctx, sku)
}

func (s *stubStore) ReserveStock(ctx context.Context, sku string, quantity int) (Item, error) {
	return s.reserveStockFunc(ctx, sku, quantity)
}

func newTestHandler(store Store) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHandler(store, logger)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func TestGetItem(t *testing.T) {
	tests := []struct {
		name       string
		getItem    func(ctx context.Context, sku string) (Item, error)
		wantStatus int
	}{
		{
			name: "found",
			getItem: func(ctx context.Context, sku string) (Item, error) {
				return Item{SKU: sku, Name: "Widget", Quantity: 10}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found",
			getItem: func(ctx context.Context, sku string) (Item, error) {
				return Item{}, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestHandler(&stubStore{getItemFunc: tt.getItem})

			req := httptest.NewRequest(http.MethodGet, "/items/SKU-001", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestReserveStock(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		reserveStock func(ctx context.Context, sku string, quantity int) (Item, error)
		wantStatus   int
	}{
		{
			name: "success",
			body: `{"quantity":5}`,
			reserveStock: func(ctx context.Context, sku string, quantity int) (Item, error) {
				return Item{SKU: sku, Name: "Widget", Quantity: 95}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "insufficient stock",
			body: `{"quantity":1000}`,
			reserveStock: func(ctx context.Context, sku string, quantity int) (Item, error) {
				return Item{}, ErrInsufficientStock
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "item not found",
			body: `{"quantity":1}`,
			reserveStock: func(ctx context.Context, sku string, quantity int) (Item, error) {
				return Item{}, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "invalid body",
			body:       `not json`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "non-positive quantity",
			body:       `{"quantity":0}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &stubStore{reserveStockFunc: tt.reserveStock}
			handler := newTestHandler(store)

			req := httptest.NewRequest(http.MethodPost, "/items/SKU-001/reserve", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var item Item
				if err := json.NewDecoder(rec.Body).Decode(&item); err != nil {
					t.Fatalf("decode response: %v", err)
				}
			}
		})
	}
}
