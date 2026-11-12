package admin

import (
	"context"
	"github.com/avpavlo8/ficusin-store/backend/internal/orderworkflow"
)

func (r *PostgresRepository) hydrateOrderWorkflows(ctx context.Context, orders []*Order) error {
	ids := make([]int64, len(orders))
	for i, o := range orders {
		ids[i] = o.ID
	}
	if err := orderworkflow.Refresh(ctx, r.pool, ids); err != nil {
		return err
	}
	states, err := orderworkflow.Read(ctx, r.pool, ids)
	if err != nil {
		return err
	}
	for _, o := range orders {
		if state, ok := states[o.ID]; ok {
			o.Workflow = state
			o.Status = state.Status
			o.HasPreorder = !state.StockConfirmed
		}
	}
	return nil
}
