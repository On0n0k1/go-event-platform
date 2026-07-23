package order

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	ordersCreatedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orders_created_total",
		Help: "Total orders successfully created.",
	})

	stockReservationFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "stock_reservation_failures_total",
		Help: "Total failed stock reservation attempts, labeled by reason.",
	}, []string{"reason"})
)
