package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var NotificationsSentTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "notifications_sent_total",
	Help: "Total order confirmation notifications sent.",
})
