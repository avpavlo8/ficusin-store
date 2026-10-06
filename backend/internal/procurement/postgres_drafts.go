package procurement

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type PlanDraft struct {
	Payload   json.RawMessage `json:"payload"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

func (store *PostgresStore) LoadPlanDraft(ctx context.Context, customerID int64) (*PlanDraft, error) {
	var draft PlanDraft
	err := store.pool.QueryRow(ctx, `SELECT payload, updated_at FROM procurement_plan_drafts WHERE customer_id=$1`, customerID).Scan(&draft.Payload, &draft.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &draft, nil
}

func (store *PostgresStore) SavePlanDraft(ctx context.Context, customerID int64, payload json.RawMessage) (PlanDraft, error) {
	var draft PlanDraft
	err := store.pool.QueryRow(ctx, `INSERT INTO procurement_plan_drafts(customer_id,payload) VALUES($1,$2)
		ON CONFLICT(customer_id) DO UPDATE SET payload=EXCLUDED.payload,updated_at=now()
		RETURNING payload,updated_at`, customerID, payload).Scan(&draft.Payload, &draft.UpdatedAt)
	return draft, err
}

func (store *PostgresStore) DeletePlanDraft(ctx context.Context, customerID int64) error {
	_, err := store.pool.Exec(ctx, `DELETE FROM procurement_plan_drafts WHERE customer_id=$1`, customerID)
	return err
}
