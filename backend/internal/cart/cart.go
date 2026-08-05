// Package cart stores the basket of a signed-in customer.
//
// The browser remains the working copy while a person shops; this is the
// backup that survives a cleared browser or a switch to another device. A
// cart is only ever emptied by the customer or by placing an order.
package cart

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Load returns an empty cart rather than an error when the customer has
// never saved one, because "no cart yet" is the normal case.
func (store *Store) Load(ctx context.Context, customerID int64) (map[string]int, error) {
	var raw []byte
	err := store.pool.QueryRow(
		ctx,
		`SELECT items FROM customer_carts WHERE customer_id = $1`,
		customerID,
	).Scan(&raw)
	if err != nil {
		return map[string]int{}, nil
	}
	items := map[string]int{}
	if err := json.Unmarshal(raw, &items); err != nil {
		return map[string]int{}, nil
	}
	return items, nil
}

func (store *Store) Save(ctx context.Context, customerID int64, items map[string]int) error {
	if items == nil {
		items = map[string]int{}
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return err
	}
	_, err = store.pool.Exec(
		ctx,
		`INSERT INTO customer_carts (customer_id, items, updated_at)
		 VALUES ($1, $2, CURRENT_TIMESTAMP)
		 ON CONFLICT (customer_id)
		 DO UPDATE SET items = EXCLUDED.items, updated_at = CURRENT_TIMESTAMP`,
		customerID,
		encoded,
	)
	return err
}
