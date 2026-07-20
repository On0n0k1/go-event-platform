package inventory

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound          = errors.New("item not found")
	ErrInsufficientStock = errors.New("insufficient stock")
)

type Store interface {
	GetItem(ctx context.Context, sku string) (Item, error)
	ReserveStock(ctx context.Context, sku string, quantity int) (Item, error)
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) GetItem(ctx context.Context, sku string) (Item, error) {
	var item Item
	err := s.pool.QueryRow(ctx,
		`SELECT sku, name, quantity FROM items WHERE sku = $1`, sku,
	).Scan(&item.SKU, &item.Name, &item.Quantity)

	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("get item: %w", err)
	}

	return item, nil
}

func (s *PostgresStore) ReserveStock(ctx context.Context, sku string, quantity int) (Item, error) {
	var item Item
	err := s.pool.QueryRow(ctx, `
		UPDATE items
		SET quantity = quantity - $1
		WHERE sku = $2 AND quantity >= $1
		RETURNING sku, name, quantity
	`, quantity, sku).Scan(&item.SKU, &item.Name, &item.Quantity)

	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := s.GetItem(ctx, sku); errors.Is(getErr, ErrNotFound) {
			return Item{}, ErrNotFound
		}
		return Item{}, ErrInsufficientStock
	}
	if err != nil {
		return Item{}, fmt.Errorf("reserve stock: %w", err)
	}

	return item, nil
}
