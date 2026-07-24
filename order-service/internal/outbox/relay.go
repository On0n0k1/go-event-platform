package outbox

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const batchSize = 20

type eventStore interface {
	FetchUnpublished(ctx context.Context, limit int) ([]Event, error)
	MarkPublished(ctx context.Context, id int64) error
}

type publisher interface {
	// msgID is used for server-side deduplication -- see events.Publisher.Publish.
	Publish(ctx context.Context, subject string, payload []byte, msgID string) error
}

type Relay struct {
	store     eventStore
	publisher publisher
	log       *slog.Logger
}

func NewRelay(store eventStore, publisher publisher, log *slog.Logger) *Relay {
	return &Relay{store: store, publisher: publisher, log: log}
}

// Run polls for unpublished outbox events every pollInterval and publishes
// them, until ctx is cancelled.
func (r *Relay) Run(ctx context.Context, pollInterval time.Duration) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		r.publishPending(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Relay) publishPending(ctx context.Context) {
	events, err := r.store.FetchUnpublished(ctx, batchSize)
	if err != nil {
		r.log.Error("failed to fetch outbox events", "error", err)
		return
	}

	for _, evt := range events {
		// A failed publish leaves the row unpublished for the next poll to
		// retry -- don't let one bad event block the rest of the batch.
		if err := r.publishOne(ctx, evt); err != nil {
			r.log.Error("failed to publish outbox event", "outbox_id", evt.ID, "subject", evt.Subject, "error", err)
			continue
		}

		if err := r.store.MarkPublished(ctx, evt.ID); err != nil {
			r.log.Error("failed to mark outbox event published", "outbox_id", evt.ID, "error", err)
		}
	}
}

func (r *Relay) publishOne(ctx context.Context, evt Event) error {
	// Restore the trace captured when the event was written (same DB
	// transaction as the order), so the event continues that request's
	// trace even though it's actually published well after the request
	// returned, rather than starting a disconnected one.
	if evt.TraceParent != "" {
		carrier := propagation.MapCarrier{"traceparent": evt.TraceParent}
		ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)
	}

	msgID := "outbox-" + strconv.FormatInt(evt.ID, 10)
	return r.publisher.Publish(ctx, evt.Subject, evt.Payload, msgID)
}
