package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

const (
	StreamName     = "ORDERS"
	SubjectCreated = "orders.created"
)

var tracer = otel.Tracer("notification-service/events")

// OrderCreated mirrors the wire format published by order-service on
// SubjectCreated. It is a contract, not shared Go code, so each consumer
// keeps its own copy of this shape.
type OrderCreated struct {
	OrderID   string    `json:"order_id"`
	SKU       string    `json:"sku"`
	Quantity  int       `json:"quantity"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type Subscriber struct {
	nc *nats.Conn
}

func Connect(natsURL string) (*Subscriber, error) {
	nc, err := nats.Connect(natsURL)
	if err != nil {
		return nil, fmt.Errorf("connect to nats: %w", err)
	}
	return &Subscriber{nc: nc}, nil
}

func (s *Subscriber) Conn() *nats.Conn {
	return s.nc
}

func (s *Subscriber) Close() {
	s.nc.Close()
}

// Consume ensures the ORDERS stream exists (consumers don't depend on the
// publisher having started first), binds a durable consumer under
// durableName, and invokes handle for every OrderCreated event until ctx is
// cancelled.
func (s *Subscriber) Consume(ctx context.Context, durableName string, logger *slog.Logger, handle func(context.Context, OrderCreated) error) error {
	js, err := jetstream.New(s.nc)
	if err != nil {
		return fmt.Errorf("create jetstream context: %w", err)
	}

	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     StreamName,
		Subjects: []string{SubjectCreated},
	})
	if err != nil {
		return fmt.Errorf("ensure stream: %w", err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       durableName,
		FilterSubject: SubjectCreated,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}

	consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		msgCtx := otel.GetTextMapPropagator().Extract(ctx, natsHeaderCarrier(msg.Headers()))
		msgCtx, span := tracer.Start(msgCtx, "process "+SubjectCreated, trace.WithSpanKind(trace.SpanKindConsumer))
		defer span.End()

		var evt OrderCreated
		if err := json.Unmarshal(msg.Data(), &evt); err != nil {
			logger.Error("failed to decode order created event", "error", err)
			_ = msg.Nak()
			return
		}

		if err := handle(msgCtx, evt); err != nil {
			logger.Error("failed to handle order created event", "order_id", evt.OrderID, "error", err)
			_ = msg.Nak()
			return
		}

		_ = msg.Ack()
	})
	if err != nil {
		return fmt.Errorf("start consuming: %w", err)
	}
	defer consumeCtx.Stop()

	<-ctx.Done()
	return nil
}
