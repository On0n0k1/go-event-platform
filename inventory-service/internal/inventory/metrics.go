package inventory

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var itemsRestockedTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "items_restocked_total",
	Help: "Total successful restock operations.",
})
