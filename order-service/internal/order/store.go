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
	CreateOrder(ctx context.Context, o Order) error
	GetOrder(ctx context.Context, id string) (Order, error)
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) CreateOrder(ctx context.Context, o Order) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO orders (id, sku, quantity, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, o.ID, o.SKU, o.Quantity, o.Status, o.CreatedAt)

	if err != nil {
		return fmt.Errorf("create order: %w", err)
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
