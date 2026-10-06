package procurement

import (
	"context"
	"encoding/json"
)

type planDraftStore interface {
	LoadPlanDraft(context.Context, int64) (*PlanDraft, error)
	SavePlanDraft(context.Context, int64, json.RawMessage) (PlanDraft, error)
	DeletePlanDraft(context.Context, int64) error
}

func (service *Service) LoadPlanDraft(ctx context.Context, actor Actor) (*PlanDraft, error) {
	return service.store.(planDraftStore).LoadPlanDraft(ctx, actor.CustomerID)
}

func (service *Service) SavePlanDraft(ctx context.Context, actor Actor, payload json.RawMessage) (PlanDraft, error) {
	return service.store.(planDraftStore).SavePlanDraft(ctx, actor.CustomerID, payload)
}

func (service *Service) DeletePlanDraft(ctx context.Context, actor Actor) error {
	return service.store.(planDraftStore).DeletePlanDraft(ctx, actor.CustomerID)
}
