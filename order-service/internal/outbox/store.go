package outbox

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Event is a row from outbox_events -- see order.OutboxEvent, which is what
// gets written there in the first place.
type Event struct {
	ID          int64
	Subject     string
	Payload     []byte
	TraceParent string
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) FetchUnpublished(ctx context.Context, limit int) ([]Event, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, subject, payload, COALESCE(trace_parent, '')
		FROM outbox_events
		WHERE published_at IS NULL
		ORDER BY id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("fetch unpublished outbox events: %w", err)
	}

	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) {
		var e Event
		err := row.Scan(&e.ID, &e.Subject, &e.Payload, &e.TraceParent)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan outbox events: %w", err)
	}

	return events, nil
}

func (s *Store) MarkPublished(ctx context.Context, id int64) error {
	if _, err := s.pool.Exec(ctx, `UPDATE outbox_events SET published_at = now() WHERE id = $1`, id); err != nil {
		return fmt.Errorf("mark outbox event published: %w", err)
	}
	return nil
}
