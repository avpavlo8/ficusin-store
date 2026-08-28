package integration

import (
	"context"
	"fmt"

	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
	"github.com/avpavlo8/ficusin-store/backend/internal/saby"
)

// ProcurementExecutor keeps failures isolated by channel. A broken Saby
// session must not prevent WB or Ozon actions from leaving the durable queue.
type ProcurementExecutor struct {
	marketplaces *MarketplaceExecutor
	saby         *SabyClient
	sabySink     interface {
		Sync(context.Context, []saby.CatalogItem) (saby.Result, error)
	}
}

func NewProcurementExecutor(marketplaces *MarketplaceExecutor, saby *SabyClient) *ProcurementExecutor {
	return &ProcurementExecutor{marketplaces: marketplaces, saby: saby}
}

// WithSabyCatalogSync connects the read-only Saby Retail client to the local
// catalogue importer. It is explicit so tests and deployments without Saby
// credentials keep the integration disabled without a second code path.
func (executor *ProcurementExecutor) WithSabyCatalogSync(sink interface {
	Sync(context.Context, []saby.CatalogItem) (saby.Result, error)
}) *ProcurementExecutor {
	executor.sabySink = sink
	return executor
}

func (executor *ProcurementExecutor) Configured(channel string) bool {
	switch channel {
	case "wb", "ozon":
		return executor != nil && executor.marketplaces != nil && executor.marketplaces.Configured(channel)
	case "saby":
		return executor != nil && executor.saby != nil && executor.saby.Configured()
	case "saby_price", "saby_receipt":
		return executor != nil && executor.saby != nil && executor.saby.Configured()
	default:
		return false
	}
}

func (executor *ProcurementExecutor) Execute(ctx context.Context, item procurement.ActionItem) (procurement.ActionExecution, error) {
	switch item.Channel {
	case "wb", "ozon":
		return executor.marketplaces.Execute(ctx, item)
	case "saby_receipt", "saby_price":
		if executor == nil || executor.saby == nil || !executor.saby.Configured() {
			return procurement.ActionExecution{}, fmt.Errorf("канал %s не настроен", item.Channel)
		}
		return executor.saby.CreateDraft(ctx, item)
	default:
		return procurement.ActionExecution{}, fmt.Errorf("канал %s не поддерживается", item.Channel)
	}
}

// FetchCatalog отдаёт справочник карточек маркетплейса. СБИС здесь не
// участвует: его номенклатура и есть то, с чем эти карточки связывают.
func (executor *ProcurementExecutor) FetchCatalog(ctx context.Context, channel string) ([]procurement.ChannelProduct, error) {
	switch channel {
	case "wb", "ozon":
		return executor.marketplaces.FetchCatalog(ctx, channel)
	default:
		return nil, fmt.Errorf("справочник канала %s не поддерживается", channel)
	}
}

func (executor *ProcurementExecutor) Probe(ctx context.Context, channel string) error {
	switch channel {
	case "wb", "ozon":
		return executor.marketplaces.Probe(ctx, channel)
	case "saby":
		return executor.saby.Probe(ctx)
	default:
		return fmt.Errorf("канал %s не поддерживается", channel)
	}
}

func (executor *ProcurementExecutor) RefreshSabyCatalog(ctx context.Context) (procurement.ChannelLinkResult, error) {
	if executor == nil || executor.saby == nil || executor.sabySink == nil {
		return procurement.ChannelLinkResult{}, fmt.Errorf("обновление справочника СБИС не настроено")
	}
	items, err := executor.saby.FetchCatalog(ctx)
	if err != nil {
		return procurement.ChannelLinkResult{}, err
	}
	result, err := executor.sabySink.Sync(ctx, items)
	if err != nil {
		return procurement.ChannelLinkResult{}, fmt.Errorf("сохранить справочник СБИС: %w", err)
	}
	return procurement.ChannelLinkResult{
		Channel: "saby", Fetched: result.ItemsRead, Linked: result.ItemsRead,
	}, nil
}
