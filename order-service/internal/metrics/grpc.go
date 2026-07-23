package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

var (
	grpcClientRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "grpc_client_requests_total",
		Help: "Total outbound gRPC requests made, labeled by method and status code.",
	}, []string{"method", "code"})

	grpcClientRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "grpc_client_request_duration_seconds",
		Help:    "Outbound gRPC request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "code"})
)

// UnaryClientInterceptor records request count/duration by method and gRPC
// status code.
func UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()

		err := invoker(ctx, method, req, reply, cc, opts...)

		code := status.Code(err).String()
		grpcClientRequestsTotal.WithLabelValues(method, code).Inc()
		grpcClientRequestDuration.WithLabelValues(method, code).Observe(time.Since(start).Seconds())

		return err
	}
}
