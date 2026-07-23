package inventory

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestCachingStore(t *testing.T, next Store) *CachingStore {
	t.Helper()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })

	return NewCachingStore(next, client)
}

func TestCachingStoreGetItemCachesOnMiss(t *testing.T) {
	calls := 0
	underlying := &stubStore{
		getItemFunc: func(ctx context.Context, sku string) (Item, error) {
			calls++
			return Item{SKU: sku, Name: "Widget", Quantity: 10}, nil
		},
	}
	store := newTestCachingStore(t, underlying)

	for i := 0; i < 3; i++ {
		item, err := store.GetItem(context.Background(), "SKU-001")
		if err != nil {
			t.Fatalf("GetItem: %v", err)
		}
		if item.Quantity != 10 {
			t.Fatalf("quantity = %d, want 10", item.Quantity)
		}
	}

	if calls != 1 {
		t.Fatalf("underlying GetItem called %d times, want 1 (cache should serve the rest)", calls)
	}
}

func TestCachingStoreReserveStockInvalidatesCache(t *testing.T) {
	quantity := 10
	underlying := &stubStore{
		getItemFunc: func(ctx context.Context, sku string) (Item, error) {
			return Item{SKU: sku, Name: "Widget", Quantity: quantity}, nil
		},
		reserveStockFunc: func(ctx context.Context, sku string, q int) (Item, error) {
			quantity -= q
			return Item{SKU: sku, Name: "Widget", Quantity: quantity}, nil
		},
	}
	store := newTestCachingStore(t, underlying)

	item, err := store.GetItem(context.Background(), "SKU-001")
	if err != nil || item.Quantity != 10 {
		t.Fatalf("GetItem = %+v, %v", item, err)
	}

	if _, err := store.ReserveStock(context.Background(), "SKU-001", 4); err != nil {
		t.Fatalf("ReserveStock: %v", err)
	}

	item, err = store.GetItem(context.Background(), "SKU-001")
	if err != nil {
		t.Fatalf("GetItem after reserve: %v", err)
	}
	if item.Quantity != 6 {
		t.Fatalf("quantity after reserve = %d, want 6 (cache should have been invalidated)", item.Quantity)
	}
}

func TestCachingStoreGetItemErrorIsNotCached(t *testing.T) {
	underlying := &stubStore{
		getItemFunc: func(ctx context.Context, sku string) (Item, error) {
			return Item{}, ErrNotFound
		},
	}
	store := newTestCachingStore(t, underlying)

	if _, err := store.GetItem(context.Background(), "SKU-999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
