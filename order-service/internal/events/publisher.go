package events

import (
	"context"
	"encoding/json"
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

func (p *Publisher) PublishOrderCreated(ctx context.Context, evt OrderCreated) error {
	ctx, span := tracer.Start(ctx, "publish "+SubjectCreated, trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	payload, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}

	msg := &nats.Msg{Subject: SubjectCreated, Data: payload, Header: nats.Header{}}
	otel.GetTextMapPropagator().Inject(ctx, natsHeaderCarrier(msg.Header))

	if _, err := p.js.PublishMsg(ctx, msg); err != nil {
		return fmt.Errorf("publish event: %w", err)
	}

	return nil
}
