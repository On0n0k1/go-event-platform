package analytics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	ordersRecordedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "analytics_orders_recorded_total",
		Help: "Total orders recorded from the orders.created event stream.",
	})

	quantityReservedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "analytics_quantity_reserved_total",
		Help: "Total quantity reserved across all recorded orders.",
	})
)

// Stats tracks running totals in memory. It resets on restart; a durable
// store is a reasonable future upgrade but out of scope for this MVP. The
// same totals are also exposed as Prometheus counters (which persist across
// scrapes independent of this struct) so /stats and /metrics agree.
type Stats struct {
	mu            sync.Mutex
	ordersCount   int
	totalQuantity int
}

type Snapshot struct {
	OrdersCount           int `json:"orders_count"`
	TotalQuantityReserved int `json:"total_quantity_reserved"`
}

func (s *Stats) Record(quantity int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ordersCount++
	s.totalQuantity += quantity

	ordersRecordedTotal.Inc()
	quantityReservedTotal.Add(float64(quantity))
}

func (s *Stats) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	return Snapshot{
		OrdersCount:           s.ordersCount,
		TotalQuantityReserved: s.totalQuantity,
	}
}
