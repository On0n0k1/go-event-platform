package order

import "time"

const StatusConfirmed = "confirmed"

type Order struct {
	ID        string    `json:"id"`
	SKU       string    `json:"sku"`
	Quantity  int       `json:"quantity"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// OutboxEvent is written to the outbox_events table in the same transaction
// as the Order that produced it -- see internal/outbox for the relay that
// later publishes it.
type OutboxEvent struct {
	Subject     string
	Payload     []byte
	TraceParent string
}
