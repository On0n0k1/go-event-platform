package events

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("order-service/events")

const (
	StreamName     = "ORDERS"
	SubjectCreated = "orders.created"
)

// OrderCreated is the wire format published to SubjectCreated. It is a
// contract between order-service (publisher) and any consumers, not shared
// Go code, so consumers keep their own copy of this shape.
type OrderCreated struct {
	OrderID   string    `json:"order_id"`
	SKU       string    `json:"sku"`
	Quantity  int       `json:"quantity"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type Publisher struct {
	nc *nats.Conn
	js jetstream.JetStream
}

func NewPublisher(ctx context.Context, natsURL string) (*Publisher, error) {
	nc, err := nats.Connect(natsURL)
	if err != nil {
		return nil, fmt.Errorf("connect to nats: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("create jetstream context: %w", err)
	}

	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     StreamName,
		Subjects: []string{SubjectCreated},
	}); err != nil {
		nc.Close()
		return nil, fmt.Errorf("ensure stream: %w", err)
	}

	return &Publisher{nc: nc, js: js}, nil
}

func (p *Publisher) Close() {
	p.nc.Close()
}

// Publish sends payload to subject, injecting the current trace context into
// NATS message headers. msgID is set as the Nats-Msg-Id header, which
// JetStream uses for server-side deduplication (default 2-minute window):
// since the outbox relay retries on ambiguous failures (e.g. a timeout where
// the server may have actually stored the message before the ack was lost),
// a stable msgID per logical event is what makes those retries safe instead
// of risking duplicate delivery. Used directly by the relay (which restores
// a stored trace context before calling this) rather than by request
// handlers -- see internal/outbox.
func (p *Publisher) Publish(ctx context.Context, subject string, payload []byte, msgID string) error {
	ctx, span := tracer.Start(ctx, "publish "+subject, trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	msg := &nats.Msg{Subject: subject, Data: payload, Header: nats.Header{}}
	msg.Header.Set("Nats-Msg-Id", msgID)
	otel.GetTextMapPropagator().Inject(ctx, natsHeaderCarrier(msg.Header))

	if _, err := p.js.PublishMsg(ctx, msg); err != nil {
		return fmt.Errorf("publish event: %w", err)
	}

	return nil
}
