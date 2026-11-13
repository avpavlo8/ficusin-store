package admin

import (
	"context"
	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
)

func (r *PostgresRepository) OrderRealizations(ctx context.Context, actor Actor, search string, offset int) (ordersale.View, error) {
	if actor.Role != RoleOwner {
		return ordersale.View{}, ErrForbidden
	}
	return ordersale.List(ctx, r.pool, search, offset)
}
func (r *PostgresRepository) PrepareOrderRealizations(ctx context.Context, actor Actor, input ordersale.PrepareInput) ([]ordersale.Event, error) {
	if actor.Role != RoleOwner {
		return nil, ErrForbidden
	}
	return ordersale.Prepare(ctx, r.pool, actor.CustomerID, input)
}
