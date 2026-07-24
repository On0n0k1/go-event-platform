package order

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("order not found")

type Store interface {
	// CreateOrder persists o and evt atomically: both the order and its
	// outbox event either commit together or not at all, so "order exists"
	// and "event is durably queued to publish" can never diverge.
	CreateOrder(ctx context.Context, o Order, evt OutboxEvent) error
	GetOrder(ctx context.Context, id string) (Order, error)
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) CreateOrder(ctx context.Context, o Order, evt OutboxEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	if _, err := tx.Exec(ctx, `
		INSERT INTO orders (id, sku, quantity, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, o.ID, o.SKU, o.Quantity, o.Status, o.CreatedAt); err != nil {
		return fmt.Errorf("create order: %w", err)
	}

	var traceParent *string
	if evt.TraceParent != "" {
		traceParent = &evt.TraceParent
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (subject, payload, trace_parent)
		VALUES ($1, $2, $3)
	`, evt.Subject, evt.Payload, traceParent); err != nil {
		return fmt.Errorf("create outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (s *PostgresStore) GetOrder(ctx context.Context, id string) (Order, error) {
	var o Order
	err := s.pool.QueryRow(ctx,
		`SELECT id, sku, quantity, status, created_at FROM orders WHERE id = $1`, id,
	).Scan(&o.ID, &o.SKU, &o.Quantity, &o.Status, &o.CreatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("get order: %w", err)
	}

	return o, nil
}
