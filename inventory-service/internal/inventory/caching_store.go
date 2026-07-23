package inventory

import (
	"context"
	"encoding/json"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"
)

const cacheTTL = 30 * time.Second

var cacheResultTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "inventory_item_cache_result_total",
	Help: "Cache-aside results for GetItem, labeled by result (hit/miss).",
}, []string{"result"})

// CachingStore wraps Store with a cache-aside layer over GetItem. Reads that
// hit the cache skip Postgres entirely; ReserveStock always invalidates the
// entry afterward rather than trusting its own write, so stock is never
// served stale. Any Redis error is treated as a cache miss -- Postgres stays
// the source of truth and the request never fails because of the cache.
type CachingStore struct {
	next  Store
	redis *redis.Client
}

func NewCachingStore(next Store, redis *redis.Client) *CachingStore {
	return &CachingStore{next: next, redis: redis}
}

func cacheKey(sku string) string {
	return "item:" + sku
}

func (s *CachingStore) GetItem(ctx context.Context, sku string) (Item, error) {
	key := cacheKey(sku)

	if cached, err := s.redis.Get(ctx, key).Result(); err == nil {
		var item Item
		if err := json.Unmarshal([]byte(cached), &item); err == nil {
			cacheResultTotal.WithLabelValues("hit").Inc()
			return item, nil
		}
	}

	cacheResultTotal.WithLabelValues("miss").Inc()

	item, err := s.next.GetItem(ctx, sku)
	if err != nil {
		return Item{}, err
	}

	s.set(ctx, sku, item)

	return item, nil
}

func (s *CachingStore) ReserveStock(ctx context.Context, sku string, quantity int) (Item, error) {
	item, err := s.next.ReserveStock(ctx, sku, quantity)
	if err != nil {
		return Item{}, err
	}

	s.redis.Del(ctx, cacheKey(sku))

	return item, nil
}

func (s *CachingStore) set(ctx context.Context, sku string, item Item) {
	payload, err := json.Marshal(item)
	if err != nil {
		return
	}
	s.redis.Set(ctx, cacheKey(sku), payload, cacheTTL)
}
