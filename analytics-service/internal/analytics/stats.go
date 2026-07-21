package analytics

import "sync"

// Stats tracks running totals in memory. It resets on restart; a durable
// store is a reasonable future upgrade but out of scope for this MVP.
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
}

func (s *Stats) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	return Snapshot{
		OrdersCount:           s.ordersCount,
		TotalQuantityReserved: s.totalQuantity,
	}
}
