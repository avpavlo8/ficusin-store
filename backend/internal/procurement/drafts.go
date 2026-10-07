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

type namedPlanDraftStore interface {
	ListNamedPlanDrafts(context.Context) ([]NamedPlanDraft, error)
	LoadNamedPlanDraft(context.Context, int64) (NamedPlanDraft, error)
	CreateNamedPlanDraft(context.Context, Actor, string, json.RawMessage) (NamedPlanDraft, error)
	UpdateNamedPlanDraft(context.Context, Actor, int64, string, json.RawMessage) (NamedPlanDraft, error)
	DeleteNamedPlanDraft(context.Context, int64) error
}

func (service *Service) ListNamedPlanDrafts(ctx context.Context, _ Actor) ([]NamedPlanDraft, error) {
	return service.store.(namedPlanDraftStore).ListNamedPlanDrafts(ctx)
}
func (service *Service) LoadNamedPlanDraft(ctx context.Context, _ Actor, id int64) (NamedPlanDraft, error) {
	return service.store.(namedPlanDraftStore).LoadNamedPlanDraft(ctx, id)
}
func (service *Service) CreateNamedPlanDraft(ctx context.Context, actor Actor, title string, payload json.RawMessage) (NamedPlanDraft, error) {
	return service.store.(namedPlanDraftStore).CreateNamedPlanDraft(ctx, actor, title, payload)
}
func (service *Service) UpdateNamedPlanDraft(ctx context.Context, actor Actor, id int64, title string, payload json.RawMessage) (NamedPlanDraft, error) {
	return service.store.(namedPlanDraftStore).UpdateNamedPlanDraft(ctx, actor, id, title, payload)
}
func (service *Service) DeleteNamedPlanDraft(ctx context.Context, _ Actor, id int64) error {
	return service.store.(namedPlanDraftStore).DeleteNamedPlanDraft(ctx, id)
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
