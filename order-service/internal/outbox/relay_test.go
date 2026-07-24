package outbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type stubStore struct {
	mu        sync.Mutex
	events    []Event
	published []int64
	fetchErr  error
}

func (s *stubStore) FetchUnpublished(ctx context.Context, limit int) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fetchErr != nil {
		return nil, s.fetchErr
	}
	return s.events, nil
}

func (s *stubStore) MarkPublished(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.published = append(s.published, id)
	return nil
}

type publishedCall struct {
	ctx     context.Context
	subject string
	payload []byte
	msgID   string
}

type stubPublisher struct {
	mu      sync.Mutex
	failFor map[string]bool
	calls   []publishedCall
}

func (p *stubPublisher) Publish(ctx context.Context, subject string, payload []byte, msgID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, publishedCall{ctx: ctx, subject: subject, payload: payload, msgID: msgID})
	if p.failFor[subject] {
		return fmt.Errorf("publish failed for %s", subject)
	}
	return nil
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRelayPublishesUnpublishedEvents(t *testing.T) {
	store := &stubStore{events: []Event{
		{ID: 1, Subject: "orders.created", Payload: []byte(`{"a":1}`)},
		{ID: 2, Subject: "orders.created", Payload: []byte(`{"a":2}`)},
	}}
	pub := &stubPublisher{}
	relay := NewRelay(store, pub, newTestLogger())

	relay.publishPending(context.Background())

	if len(pub.calls) != 2 {
		t.Fatalf("publish calls = %d, want 2", len(pub.calls))
	}
	if len(store.published) != 2 || store.published[0] != 1 || store.published[1] != 2 {
		t.Fatalf("marked published = %v, want [1 2]", store.published)
	}

	// A stable msgID derived from the outbox row ID is what makes retried
	// (possibly duplicate) publishes safe via JetStream's server-side dedup.
	if pub.calls[0].msgID != "outbox-1" || pub.calls[1].msgID != "outbox-2" {
		t.Fatalf("msgIDs = [%q %q], want [outbox-1 outbox-2]", pub.calls[0].msgID, pub.calls[1].msgID)
	}
}

func TestRelayContinuesAfterPublishFailure(t *testing.T) {
	store := &stubStore{events: []Event{
		{ID: 1, Subject: "will-fail", Payload: []byte(`{}`)},
		{ID: 2, Subject: "will-succeed", Payload: []byte(`{}`)},
	}}
	pub := &stubPublisher{failFor: map[string]bool{"will-fail": true}}
	relay := NewRelay(store, pub, newTestLogger())

	relay.publishPending(context.Background())

	if len(pub.calls) != 2 {
		t.Fatalf("publish calls = %d, want 2 (a failure should not stop the batch)", len(pub.calls))
	}
	if len(store.published) != 1 || store.published[0] != 2 {
		t.Fatalf("marked published = %v, want [2] (failed event must stay unpublished for retry)", store.published)
	}
}

func TestRelayLogsAndReturnsOnFetchError(t *testing.T) {
	store := &stubStore{fetchErr: errors.New("db down")}
	pub := &stubPublisher{}
	relay := NewRelay(store, pub, newTestLogger())

	relay.publishPending(context.Background())

	if len(pub.calls) != 0 {
		t.Fatalf("publish calls = %d, want 0", len(pub.calls))
	}
}

func TestRelayRestoresStoredTraceContext(t *testing.T) {
	// In production, main.go's tracing.Init sets this global propagator
	// before anything else runs; replicate that here since this test relies
	// on it to parse the stored traceparent header.
	otel.SetTextMapPropagator(propagation.TraceContext{})

	const traceParent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	store := &stubStore{events: []Event{
		{ID: 1, Subject: "orders.created", Payload: []byte(`{}`), TraceParent: traceParent},
	}}
	pub := &stubPublisher{}
	relay := NewRelay(store, pub, newTestLogger())

	relay.publishPending(context.Background())

	if len(pub.calls) != 1 {
		t.Fatalf("publish calls = %d, want 1", len(pub.calls))
	}

	sc := trace.SpanContextFromContext(pub.calls[0].ctx)
	if !sc.IsValid() {
		t.Fatal("expected a valid span context restored from the stored traceparent")
	}
	if got := sc.TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace id = %q, want 4bf92f3577b34da6a3ce929d0e0e4736", got)
	}
}

func TestRelayRunStopsOnContextCancellation(t *testing.T) {
	store := &stubStore{}
	pub := &stubPublisher{}
	relay := NewRelay(store, pub, newTestLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		relay.Run(ctx, time.Millisecond)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}
