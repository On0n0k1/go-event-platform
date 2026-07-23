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
	grpcServerRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "grpc_server_requests_total",
		Help: "Total gRPC requests handled, labeled by method and status code.",
	}, []string{"method", "code"})

	grpcServerRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "grpc_server_request_duration_seconds",
		Help:    "gRPC server request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "code"})
)

// UnaryServerInterceptor records request count/duration by method and gRPC
// status code.
func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		code := status.Code(err).String()
		grpcServerRequestsTotal.WithLabelValues(info.FullMethod, code).Inc()
		grpcServerRequestDuration.WithLabelValues(info.FullMethod, code).Observe(time.Since(start).Seconds())

		return resp, err
	}
}
